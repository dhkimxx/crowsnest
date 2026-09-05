package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type DecoderRegistry struct {
	decoders []ports.WebhookDecoder
}

func NewDecoderRegistry(decoders ...ports.WebhookDecoder) *DecoderRegistry {
	return &DecoderRegistry{decoders: decoders}
}

func (r *DecoderRegistry) Decode(ctx context.Context, headers http.Header, body []byte) (domain.CanonicalEvent, error) {
	if r == nil || len(r.decoders) == 0 {
		return domain.CanonicalEvent{}, errors.New("no webhook decoders configured")
	}
	var ignored bool
	var lastErr error
	for _, decoder := range r.decoders {
		event, err := decoder.Decode(ctx, headers, body)
		if err == nil {
			return event, nil
		}
		if errors.Is(err, ports.ErrIgnoredEvent) {
			ignored = true
			continue
		}
		lastErr = err
	}
	if ignored {
		return domain.CanonicalEvent{}, ports.ErrIgnoredEvent
	}
	if lastErr != nil {
		return domain.CanonicalEvent{}, lastErr
	}
	return domain.CanonicalEvent{}, errors.New("no configured decoder accepted webhook")
}

type IngestResult struct {
	Status          string
	EventKey        string
	DeliveryCount   int
	UnresolvedCount int
}

type IngestService struct {
	decoders       *DecoderRegistry
	router         *Router
	events         ports.EventStore
	pipelineStates ports.PipelineStateStore
	logger         *slog.Logger
}

func NewIngestService(decoders *DecoderRegistry, router *Router, events ports.EventStore, pipelineStates ports.PipelineStateStore, logger *slog.Logger) *IngestService {
	if logger == nil {
		logger = slog.Default()
	}
	return &IngestService{
		decoders:       decoders,
		router:         router,
		events:         events,
		pipelineStates: pipelineStates,
		logger:         logger,
	}
}

func (s *IngestService) Handle(ctx context.Context, headers http.Header, body []byte) (IngestResult, error) {
	if s == nil || s.decoders == nil || s.router == nil || s.events == nil {
		return IngestResult{}, errors.New("ingest service is not configured")
	}
	event, err := s.decoders.Decode(ctx, headers, body)
	if err != nil {
		if errors.Is(err, ports.ErrIgnoredEvent) {
			if ignoredStore, ok := s.events.(ports.IgnoredEventStore); ok {
				digest := sha256.Sum256(body)
				if recordErr := ignoredStore.RecordIgnored(ctx, "gitlab:ignored:"+hex.EncodeToString(digest[:]), "17.6", headers.Get("X-Gitlab-Event")); recordErr != nil {
					s.logger.Error("could not record ignored GitLab event", "error", recordErr)
				}
			}
			s.logger.Info("ignored GitLab event", "source_event", headers.Get("X-Gitlab-Event"))
			return IngestResult{Status: "ignored"}, nil
		}
		return IngestResult{}, fmt.Errorf("decode webhook: %w", err)
	}
	route, err := s.router.Route(ctx, event)
	if err != nil {
		return IngestResult{}, fmt.Errorf("route webhook event: %w", err)
	}
	if transactional, ok := s.events.(ports.TransactionalEventStore); ok {
		if err := transactional.EnqueueWithPipelineState(ctx, event, route.Deliveries, route.PipelineState); err != nil {
			return IngestResult{}, fmt.Errorf("enqueue webhook event: %w", err)
		}
	} else {
		if err := s.events.Enqueue(ctx, event, route.Deliveries); err != nil {
			return IngestResult{}, fmt.Errorf("enqueue webhook event: %w", err)
		}
		if route.PipelineState != nil && s.pipelineStates != nil {
			if err := s.pipelineStates.Put(ctx, *route.PipelineState); err != nil {
				return IngestResult{}, fmt.Errorf("store pipeline state: %w", err)
			}
		}
	}
	if len(route.UnresolvedUsers) > 0 {
		if unresolvedStore, ok := s.events.(ports.UnresolvedStore); ok {
			if err := unresolvedStore.Record(ctx, event.EventKey, route.UnresolvedUsers); err != nil {
				s.logger.Error("could not record unresolved GitLab users", "event_key", event.EventKey, "error", err)
			}
		}
	}
	if len(route.UnresolvedUsers) > 0 {
		s.logger.Warn("GitLab users could not be resolved", "count", len(route.UnresolvedUsers), "event_key", event.EventKey)
	}
	return IngestResult{
		Status:          "accepted",
		EventKey:        event.EventKey,
		DeliveryCount:   len(route.Deliveries),
		UnresolvedCount: len(route.UnresolvedUsers),
	}, nil
}
