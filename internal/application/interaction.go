package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type InteractionService struct {
	store       ports.InteractionStore
	preferences ports.PreferenceStore
	logger      *slog.Logger
}

func NewInteractionService(store ports.InteractionStore, preferences ports.PreferenceStore, logger *slog.Logger) *InteractionService {
	if logger == nil {
		logger = slog.Default()
	}
	return &InteractionService{store: store, preferences: preferences, logger: logger}
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
	if interaction.Action != domain.ActionMuteReason && interaction.Action != domain.ActionUnmuteReason {
		s.logger.Warn("unsupported interaction action", "action", interaction.Action)
		return domain.InteractionResult{Status: domain.InteractionIgnored}
	}
	reason := strings.TrimSpace(interaction.Value["reason"])
	if !toggleableReason(reason) {
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This action is not supported."}
	}
	record, err := s.store.DeliveryByMessageID(ctx, interaction.MessageID)
	if err != nil {
		s.logger.Error("interaction delivery lookup failed", "error", err)
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This alert could not be loaded."}
	}
	if record == nil {
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This alert can no longer be updated."}
	}
	if !notificationHasReason(record.Notification, reason) {
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This action is not supported."}
	}
	enabled := interaction.Action == domain.ActionUnmuteReason
	if err := s.preferences.SetReasonEnabled(ctx, record.Notification.Recipient, reason, enabled); err != nil {
		s.logger.Error("could not update notification preference", "error", err)
		return domain.InteractionResult{Status: domain.InteractionRejected, Toast: "This action could not be applied."}
	}
	updated := record.Notification
	reasonTitle := notificationActionTitle(updated, reason)
	updated.Actions = toggleNotificationActions(updated.Actions, reason, reasonTitle, enabled)
	s.logger.Info("interaction applied",
		"action", interaction.Action,
		"reason", reason,
		"delivery_key", record.Key,
		"actor", interaction.ActorID,
	)
	return domain.InteractionResult{
		Status:       domain.InteractionApplied,
		Notification: &updated,
		Toast:        toastFor(reasonTitle, enabled),
	}
}

func toggleNotificationActions(actions []domain.NotificationAction, reasonCode, reasonTitle string, enabled bool) []domain.NotificationAction {
	updated := make([]domain.NotificationAction, 0, len(actions))
	for _, action := range actions {
		if action.Value["reason"] != reasonCode {
			updated = append(updated, action)
			continue
		}
		updated = append(updated, toggleReasonAction(reasonCode, reasonTitle, enabled))
	}
	return updated
}

func notificationHasReason(notification domain.Notification, reasonCode string) bool {
	for _, reason := range notification.Reasons {
		if reason.Code == reasonCode {
			return true
		}
	}
	return false
}

func notificationActionTitle(notification domain.Notification, reasonCode string) string {
	for _, action := range notification.Actions {
		if action.Value["reason"] == reasonCode && action.Value["title"] != "" {
			return action.Value["title"]
		}
	}
	return reasonCode
}

func toastFor(title string, enabled bool) string {
	if enabled {
		return "Alerts are on again for " + title + "."
	}
	return "Alerts are muted for " + title + "."
}
