package feishu

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type fakeInteractionHandler struct {
	result domain.InteractionResult
	err    error
}

func (f fakeInteractionHandler) Handle(context.Context, domain.Interaction) (domain.InteractionResult, error) {
	return f.result, f.err
}

func cardActionEvent() *callback.CardActionTriggerEvent {
	return &callback.CardActionTriggerEvent{
		EventV2Base: &larkevent.EventV2Base{
			Header: &larkevent.EventHeader{EventID: "evt-1", EventType: "card.action.trigger"},
		},
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "ou_1"},
			Context:  &callback.Context{OpenMessageID: "om_123"},
			Action: &callback.CallBackAction{Value: map[string]interface{}{
				"action": "mute_all",
			}},
		},
	}
}

func TestInteractionFromCardAction(t *testing.T) {
	interaction, ok := interactionFromCardAction(cardActionEvent())
	if !ok {
		t.Fatal("interactionFromCardAction() did not accept the callback")
	}
	if interaction.EventID != "evt-1" || interaction.MessageID != "om_123" || interaction.ActorID != "ou_1" {
		t.Fatalf("interaction = %#v", interaction)
	}
	if interaction.Action != domain.ActionMuteAll {
		t.Fatalf("action = %q", interaction.Action)
	}
	if len(interaction.Value) != 0 {
		t.Fatalf("value = %#v", interaction.Value)
	}
	if _, ok := interaction.Value["action"]; ok {
		t.Fatalf("value should not keep the action key: %#v", interaction.Value)
	}
}

func TestInteractionFromCardActionRejectsMissingFields(t *testing.T) {
	event := cardActionEvent()
	event.Event.Context = nil
	if _, ok := interactionFromCardAction(event); ok {
		t.Fatal("callback without a message id should be rejected")
	}
	event = cardActionEvent()
	event.Event.Action = nil
	if _, ok := interactionFromCardAction(event); ok {
		t.Fatal("callback without an action should be rejected")
	}
}

func TestInteractionWorkerUpdatesCard(t *testing.T) {
	notification := domain.Notification{
		EventKey:  "event-1",
		Kind:      domain.EventKindPipeline,
		Action:    "failed",
		Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "carol@example.com"},
		Title:     "[GitLab] Pipeline failed",
		Summary:   "group/project · failed",
		URL:       "https://gitlab.example/group/project/-/pipelines/1",
		Reasons:   []domain.NotificationReason{{Code: "ci_failed", Text: "The pipeline for your commit failed."}},
		Actions: []domain.NotificationAction{{
			Action: domain.ActionUnmuteAll,
			Label:  "Unmute",
			Value:  map[string]string{"reason": "ci_failed", "title": "Pipeline failed"},
		}},
	}
	worker, err := NewInteractionWorker(InteractionWorkerConfig{
		AppID:     "cli_test",
		AppSecret: "secret",
		Handler: fakeInteractionHandler{result: domain.InteractionResult{
			Status:       domain.InteractionApplied,
			Notification: &notification,
			Toast:        "Alerts are muted for Pipeline failed.",
		}},
	})
	if err != nil {
		t.Fatalf("NewInteractionWorker() error = %v", err)
	}
	response, err := worker.handleCardAction(context.Background(), cardActionEvent())
	if err != nil {
		t.Fatalf("handleCardAction() error = %v", err)
	}
	if response.Toast == nil || response.Toast.Type != "success" {
		t.Fatalf("toast = %#v", response.Toast)
	}
	if response.Card == nil || response.Card.Type != "raw" {
		t.Fatalf("card = %#v", response.Card)
	}
	payload, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	text := string(payload)
	if !strings.Contains(text, "Unmute") || !strings.Contains(text, `"action":"unmute_all"`) {
		t.Fatalf("response does not contain the toggled action: %s", text)
	}
}

func TestInteractionWorkerRejectsUnknownCallback(t *testing.T) {
	worker, err := NewInteractionWorker(InteractionWorkerConfig{
		AppID:     "cli_test",
		AppSecret: "secret",
		Handler:   fakeInteractionHandler{},
	})
	if err != nil {
		t.Fatalf("NewInteractionWorker() error = %v", err)
	}
	response, err := worker.handleCardAction(context.Background(), &callback.CardActionTriggerEvent{})
	if err != nil {
		t.Fatalf("handleCardAction() error = %v", err)
	}
	if response.Toast == nil || response.Toast.Type != "error" {
		t.Fatalf("toast = %#v", response.Toast)
	}
	if response.Card != nil {
		t.Fatalf("card = %#v", response.Card)
	}
}

var _ ports.InteractionHandler = fakeInteractionHandler{}
