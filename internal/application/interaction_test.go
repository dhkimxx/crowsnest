package application

import (
	"context"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type fakeInteractionStore struct {
	duplicate bool
	delivery  *ports.RecordedDelivery
	begun     []ports.InteractionRecord
	finished  map[string]string
}

func (f *fakeInteractionStore) BeginInteraction(_ context.Context, record ports.InteractionRecord) (bool, error) {
	f.begun = append(f.begun, record)
	return f.duplicate, nil
}

func (f *fakeInteractionStore) FinishInteraction(_ context.Context, eventID, result string) error {
	if f.finished == nil {
		f.finished = map[string]string{}
	}
	f.finished[eventID] = result
	return nil
}

func (f *fakeInteractionStore) DeliveryByMessageID(_ context.Context, _ string) (*ports.RecordedDelivery, error) {
	return f.delivery, nil
}

type fakePreferenceStore struct {
	calls   int
	address domain.RecipientAddress
	reason  string
	enabled bool
}

func (f *fakePreferenceStore) Enabled(context.Context, domain.RecipientAddress, domain.EventKind, string, string) (bool, error) {
	return true, nil
}

func (f *fakePreferenceStore) SetReasonEnabled(_ context.Context, address domain.RecipientAddress, reason string, enabled bool) error {
	f.calls++
	f.address = address
	f.reason = reason
	f.enabled = enabled
	return nil
}

func interactionDelivery() *ports.RecordedDelivery {
	return &ports.RecordedDelivery{
		Key: "gitlab:pipeline:1:carol@example.com:ci_failed",
		Notification: domain.Notification{
			EventKey:  "event-1",
			Kind:      domain.EventKindPipeline,
			Action:    "failed",
			Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "carol@example.com"},
			Reasons:   []domain.NotificationReason{{Code: ReasonCIFailed, Text: "The pipeline for your commit failed."}},
			Actions:   []domain.NotificationAction{toggleReasonAction(ReasonCIFailed, "Pipeline failed", true)},
		},
	}
}

func muteInteraction() domain.Interaction {
	return domain.Interaction{
		EventID:   "evt-1",
		MessageID: "om_123",
		ActorID:   "ou_1",
		Action:    domain.ActionMuteReason,
		Value:     map[string]string{"reason": ReasonCIFailed},
	}
}

func TestInteractionServiceMutesReason(t *testing.T) {
	store := &fakeInteractionStore{delivery: interactionDelivery()}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	result, err := service.Handle(context.Background(), muteInteraction())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionApplied {
		t.Fatalf("status = %q", result.Status)
	}
	if preferences.calls != 1 || preferences.reason != ReasonCIFailed || preferences.enabled {
		t.Fatalf("preference call = %d %q %v", preferences.calls, preferences.reason, preferences.enabled)
	}
	if preferences.address.Value != "carol@example.com" {
		t.Fatalf("preference address = %#v", preferences.address)
	}
	if result.Notification == nil || len(result.Notification.Actions) != 1 {
		t.Fatalf("notification = %#v", result.Notification)
	}
	action := result.Notification.Actions[0]
	if action.Action != domain.ActionUnmuteReason || action.Label != "Unmute \"Pipeline failed\"" {
		t.Fatalf("action = %#v", action)
	}
	if store.finished["evt-1"] != domain.InteractionApplied {
		t.Fatalf("finished = %#v", store.finished)
	}
	if result.Toast == "" {
		t.Fatal("expected a toast message")
	}
}

func TestInteractionServiceUnmutesReason(t *testing.T) {
	delivery := interactionDelivery()
	delivery.Notification.Actions = []domain.NotificationAction{toggleReasonAction(ReasonCIFailed, "Pipeline failed", false)}
	store := &fakeInteractionStore{delivery: delivery}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	interaction := muteInteraction()
	interaction.Action = domain.ActionUnmuteReason
	result, err := service.Handle(context.Background(), interaction)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionApplied || !preferences.enabled {
		t.Fatalf("status = %q enabled = %v", result.Status, preferences.enabled)
	}
	if action := result.Notification.Actions[0]; action.Action != domain.ActionMuteReason {
		t.Fatalf("action = %#v", action)
	}
}

func TestInteractionServiceSkipsDuplicate(t *testing.T) {
	store := &fakeInteractionStore{duplicate: true, delivery: interactionDelivery()}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	result, err := service.Handle(context.Background(), muteInteraction())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionDuplicate || preferences.calls != 0 {
		t.Fatalf("status = %q preference calls = %d", result.Status, preferences.calls)
	}
}

func TestInteractionServiceRejectsUnknownMessage(t *testing.T) {
	store := &fakeInteractionStore{}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	result, err := service.Handle(context.Background(), muteInteraction())
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionRejected || preferences.calls != 0 || result.Toast == "" {
		t.Fatalf("result = %#v preference calls = %d", result, preferences.calls)
	}
	if store.finished["evt-1"] != domain.InteractionRejected {
		t.Fatalf("finished = %#v", store.finished)
	}
}

func TestInteractionServiceRejectsReasonNotOnNotification(t *testing.T) {
	store := &fakeInteractionStore{delivery: interactionDelivery()}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	interaction := muteInteraction()
	interaction.Value = map[string]string{"reason": ReasonMention}
	result, err := service.Handle(context.Background(), interaction)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionRejected || preferences.calls != 0 {
		t.Fatalf("result = %#v preference calls = %d", result, preferences.calls)
	}
}

func TestInteractionServiceIgnoresUnsupportedAction(t *testing.T) {
	store := &fakeInteractionStore{delivery: interactionDelivery()}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	interaction := muteInteraction()
	interaction.Action = "trigger_pipeline_rerun"
	result, err := service.Handle(context.Background(), interaction)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionIgnored || preferences.calls != 0 {
		t.Fatalf("result = %#v preference calls = %d", result, preferences.calls)
	}
}
