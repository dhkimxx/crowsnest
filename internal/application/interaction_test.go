package application

import (
	"context"
	"testing"
	"time"

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
	calls     int
	address   domain.RecipientAddress
	until     *time.Time
	muteState domain.PreferenceState
}

func (f *fakePreferenceStore) Enabled(context.Context, domain.RecipientAddress, domain.EventKind, string, string) (bool, error) {
	return true, nil
}

func (f *fakePreferenceStore) SetMute(_ context.Context, address domain.RecipientAddress, until *time.Time) error {
	f.calls++
	f.address = address
	f.until = until
	return nil
}

func (f *fakePreferenceStore) MuteState(context.Context, domain.RecipientAddress) (domain.PreferenceState, error) {
	return f.muteState, nil
}

func interactionDelivery() *ports.RecordedDelivery {
	return &ports.RecordedDelivery{
		Key: "gitlab:header:pipeline-1:carol@example.com:ci_failed",
		Notification: domain.Notification{
			EventKey:  "event-1",
			Kind:      domain.EventKindPipeline,
			Action:    "failed",
			Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "carol@example.com"},
			Reasons:   []domain.NotificationReason{{Code: ReasonCIFailed, Text: "The pipeline for your commit failed."}},
			Actions:   []domain.NotificationAction{settingsAction()},
		},
	}
}

func cardInteraction(action string) domain.Interaction {
	return domain.Interaction{
		EventID:   "evt-1",
		MessageID: "om_123",
		ActorID:   "ou_1",
		Action:    action,
	}
}

func TestInteractionServiceOpensSettings(t *testing.T) {
	until := time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC)
	preferences := &fakePreferenceStore{muteState: domain.PreferenceState{Muted: true, MutedUntil: &until}}
	service := NewInteractionService(&fakeInteractionStore{delivery: interactionDelivery()}, preferences, nil)

	result, err := service.Handle(context.Background(), cardInteraction(domain.ActionOpenSettings))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionApplied || result.Settings == nil {
		t.Fatalf("result = %#v", result)
	}
	if !result.Settings.Muted || result.Settings.MutedUntil == nil || !result.Settings.MutedUntil.Equal(until) {
		t.Fatalf("settings = %#v", result.Settings)
	}
	if preferences.calls != 0 {
		t.Fatalf("opening settings must not write a preference, calls = %d", preferences.calls)
	}
}

func TestInteractionServiceMutesAllForThirtyDays(t *testing.T) {
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(&fakeInteractionStore{delivery: interactionDelivery()}, preferences, nil)
	fixed := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	service.clock = func() time.Time { return fixed }

	result, err := service.Handle(context.Background(), cardInteraction(domain.ActionMuteAll))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionApplied || result.Settings == nil || !result.Settings.Muted {
		t.Fatalf("result = %#v", result)
	}
	if preferences.calls != 1 || preferences.until == nil {
		t.Fatalf("preference call = %d until = %v", preferences.calls, preferences.until)
	}
	if want := fixed.Add(30 * 24 * time.Hour); !preferences.until.Equal(want) {
		t.Fatalf("muted until = %v, want %v", preferences.until, want)
	}
	if preferences.address.Value != "carol@example.com" {
		t.Fatalf("preference address = %#v", preferences.address)
	}
	if result.Toast == "" {
		t.Fatal("expected a toast message")
	}
}

func TestInteractionServiceUnmutesAll(t *testing.T) {
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(&fakeInteractionStore{delivery: interactionDelivery()}, preferences, nil)

	result, err := service.Handle(context.Background(), cardInteraction(domain.ActionUnmuteAll))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionApplied || result.Settings == nil || result.Settings.Muted {
		t.Fatalf("result = %#v", result)
	}
	if preferences.calls != 1 || preferences.until != nil {
		t.Fatalf("unmute should clear the deadline, calls = %d until = %v", preferences.calls, preferences.until)
	}
}

func TestInteractionServiceClosesSettings(t *testing.T) {
	service := NewInteractionService(&fakeInteractionStore{delivery: interactionDelivery()}, &fakePreferenceStore{}, nil)

	result, err := service.Handle(context.Background(), cardInteraction(domain.ActionCloseSettings))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionApplied || result.Notification == nil || result.Settings != nil {
		t.Fatalf("result = %#v", result)
	}
	if result.Notification.EventKey != "event-1" {
		t.Fatalf("notification = %#v", result.Notification)
	}
}

func TestInteractionServiceSkipsDuplicate(t *testing.T) {
	store := &fakeInteractionStore{duplicate: true, delivery: interactionDelivery()}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	result, err := service.Handle(context.Background(), cardInteraction(domain.ActionMuteAll))
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

	result, err := service.Handle(context.Background(), cardInteraction(domain.ActionOpenSettings))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionRejected || result.Toast == "" {
		t.Fatalf("result = %#v", result)
	}
	if store.finished["evt-1"] != domain.InteractionRejected {
		t.Fatalf("finished = %#v", store.finished)
	}
}

func TestInteractionServiceIgnoresUnsupportedAction(t *testing.T) {
	store := &fakeInteractionStore{delivery: interactionDelivery()}
	preferences := &fakePreferenceStore{}
	service := NewInteractionService(store, preferences, nil)

	result, err := service.Handle(context.Background(), cardInteraction("trigger_pipeline_rerun"))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != domain.InteractionIgnored || preferences.calls != 0 {
		t.Fatalf("result = %#v preference calls = %d", result, preferences.calls)
	}
}
