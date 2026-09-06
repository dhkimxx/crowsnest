package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type DeliveryWorkerConfig struct {
	WorkerID        string
	BatchSize       int
	PollInterval    time.Duration
	RecipientPolicy *RecipientPolicy
}

type DeliveryWorker struct {
	outbox    ports.OutboxStore
	messenger ports.Messenger
	config    DeliveryWorkerConfig
	logger    *slog.Logger
}

func NewDeliveryWorker(outbox ports.OutboxStore, messenger ports.Messenger, config DeliveryWorkerConfig, logger *slog.Logger) *DeliveryWorker {
	if config.WorkerID == "" {
		config.WorkerID = "crowsnest-delivery"
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 2 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	if config.RecipientPolicy == nil {
		config.RecipientPolicy = NewRecipientPolicy(nil)
	}
	return &DeliveryWorker{outbox: outbox, messenger: messenger, config: config, logger: logger}
}

func (w *DeliveryWorker) Run(ctx context.Context) error {
	if w.outbox == nil || w.messenger == nil {
		return errors.New("delivery worker dependencies are missing")
	}
	if err := w.process(ctx); err != nil && !errors.Is(err, context.Canceled) {
		w.logger.Error("initial delivery worker pass failed", "error", err)
	}
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := w.process(ctx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Error("delivery worker pass failed", "error", err)
			}
		}
	}
}

func (w *DeliveryWorker) process(ctx context.Context) error {
	deliveries, err := w.outbox.Claim(ctx, w.config.WorkerID, w.config.BatchSize)
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		if !w.config.RecipientPolicy.Allows(delivery.Notification.Recipient) {
			failure := domain.DeliveryFailure{Class: "policy_blocked", Retryable: false, Message: "recipient is outside the configured allowlist"}
			if err := w.outbox.MarkFailed(ctx, delivery.Key, failure); err != nil {
				return fmt.Errorf("mark blocked delivery %s failed: %w", delivery.Key, err)
			}
			w.logger.Info("delivery blocked by recipient allowlist", "delivery_key", delivery.Key, "event_key", delivery.Notification.EventKey)
			continue
		}
		receipt, sendErr := w.messenger.Send(ctx, delivery.Notification)
		if sendErr == nil {
			if err := w.outbox.MarkDelivered(ctx, delivery.Key, receipt); err != nil {
				return fmt.Errorf("mark delivery %s delivered: %w", delivery.Key, err)
			}
			w.logger.Info("delivery completed", "delivery_key", delivery.Key, "event_key", delivery.Notification.EventKey)
			continue
		}
		failure := domain.DeliveryFailure{Class: "unknown", Retryable: true, Message: sendErr.Error()}
		var classified ports.DeliveryError
		if errors.As(sendErr, &classified) {
			failure = classified.DeliveryFailure()
		}
		if err := w.outbox.MarkFailed(ctx, delivery.Key, failure); err != nil {
			return fmt.Errorf("mark delivery %s failed: %w", delivery.Key, err)
		}
		w.logger.Warn("delivery failed", "delivery_key", delivery.Key, "class", failure.Class, "retryable", failure.Retryable)
	}
	return nil
}

type DryRunMessenger struct {
	logger *slog.Logger
}

func NewDryRunMessenger(logger *slog.Logger) *DryRunMessenger {
	if logger == nil {
		logger = slog.Default()
	}
	return &DryRunMessenger{logger: logger}
}

func (m *DryRunMessenger) Send(_ context.Context, notification domain.Notification) (domain.DeliveryReceipt, error) {
	digest := sha256.Sum256([]byte(notification.Recipient.Value))
	m.logger.Info("dry-run delivery", "event_key", notification.EventKey, "recipient_hash", hex.EncodeToString(digest[:8]), "kind", notification.Kind)
	return domain.DeliveryReceipt{ProviderMessageID: "dry-run"}, nil
}
