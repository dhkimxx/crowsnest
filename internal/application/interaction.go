package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type InteractionService struct {
	store       ports.InteractionStore
	preferences ports.PreferenceStore
	logger      *slog.Logger
	clock       func() time.Time
}

func NewInteractionService(store ports.InteractionStore, preferences ports.PreferenceStore, logger *slog.Logger) *InteractionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &InteractionService{store: store, preferences: preferences, logger: logger, clock: time.Now}
}

func (s *InteractionService) Handle(ctx context.Context, interaction domain.Interaction) (domain.InteractionResult, error) {
	if s == nil || s.store == nil || s.preferences == nil {
		return domain.InteractionResult{}, errors.New("interaction service is not configured")
	}
	if strings.TrimSpace(interaction.EventID) == "" {
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This action could not be verified."}, nil
	}
	duplicate, err := s.store.BeginInteraction(ctx, ports.InteractionRecord{
		EventID:   interaction.EventID,
		MessageID: interaction.MessageID,
		ActorID:   interaction.ActorID,
		Action:    interaction.Action,
	})
	if err != nil {
		return domain.InteractionResult{}, err
	}
	if duplicate {
		return domain.InteractionResult{Status: domain.InteractionDuplicate}, nil
	}
	result := s.apply(ctx, interaction)
	if err := s.store.FinishInteraction(ctx, interaction.EventID, result.Status); err != nil {
		s.logger.Warn("could not record interaction result", "error", err)
	}
	return result, nil
}

func (s *InteractionService) apply(ctx context.Context, interaction domain.Interaction) domain.InteractionResult {
	switch interaction.Action {
	case domain.ActionOpenSettings:
		return s.openSettings(ctx, interaction)
	case domain.ActionCloseSettings:
		return s.closeSettings(ctx, interaction)
	case domain.ActionMuteAll, domain.ActionUnmuteAll:
		return s.setMute(ctx, interaction)
	default:
		s.logger.Warn("unsupported interaction action", "action", interaction.Action)
		return domain.InteractionResult{Status: domain.InteractionIgnored}
	}
}

func (s *InteractionService) openSettings(ctx context.Context, interaction domain.Interaction) domain.InteractionResult {
	record, rejected, ok := s.delivery(ctx, interaction)
	if !ok {
		return rejected
	}
	state, err := s.preferences.MuteState(ctx, record.Notification.Recipient)
	if err != nil {
		s.logger.Error("could not read notification preference", "error", err)
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "Settings could not be loaded."}
	}
	s.logger.Info("interaction applied", "action", interaction.Action, "delivery_key", record.Key, "actor", interaction.ActorID)
	return domain.InteractionResult{Status: domain.InteractionApplied, Settings: &state}
}

func (s *InteractionService) closeSettings(ctx context.Context, interaction domain.Interaction) domain.InteractionResult {
	record, rejected, ok := s.delivery(ctx, interaction)
	if !ok {
		return rejected
	}
	s.logger.Info("interaction applied", "action", interaction.Action, "delivery_key", record.Key, "actor", interaction.ActorID)
	return domain.InteractionResult{Status: domain.InteractionApplied, Notification: &record.Notification}
}

func (s *InteractionService) setMute(ctx context.Context, interaction domain.Interaction) domain.InteractionResult {
	record, rejected, ok := s.delivery(ctx, interaction)
	if !ok {
		return rejected
	}
	muted := interaction.Action == domain.ActionMuteAll
	var until *time.Time
	if muted {
		deadline := s.clock().Add(muteDuration)
		until = &deadline
	}
	if err := s.preferences.SetMute(ctx, record.Notification.Recipient, until); err != nil {
		s.logger.Error("could not update notification preference", "error", err)
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This action could not be applied."}
	}
	state := domain.PreferenceState{Muted: muted, MutedUntil: until}
	toast := "Unmuted."
	if muted {
		toast = "Muted for 30 days."
	}
	s.logger.Info("interaction applied", "action", interaction.Action, "delivery_key", record.Key, "actor", interaction.ActorID)
	return domain.InteractionResult{Status: domain.InteractionApplied, Settings: &state, Toast: toast}
}

func (s *InteractionService) delivery(ctx context.Context, interaction domain.Interaction) (*ports.RecordedDelivery, domain.InteractionResult, bool) {
	record, err := s.store.DeliveryByMessageID(ctx, interaction.MessageID)
	if err != nil {
		s.logger.Error("interaction delivery lookup failed", "error", err)
		return nil, domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This alert could not be loaded."}, false
	}
	if record == nil {
		return nil, domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This alert can no longer be updated."}, false
	}
	return record, domain.InteractionResult{}, true
}
