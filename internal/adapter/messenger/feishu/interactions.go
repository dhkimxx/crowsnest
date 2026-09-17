package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

type InteractionWorkerConfig struct {
	AppID     string
	AppSecret string
	Handler   ports.InteractionHandler
	Logger    *slog.Logger
}

type InteractionWorker struct {
	client  *larkws.Client
	handler ports.InteractionHandler
	logger  *slog.Logger
}

func NewInteractionWorker(config InteractionWorkerConfig) (*InteractionWorker, error) {
	if strings.TrimSpace(config.AppID) == "" || strings.TrimSpace(config.AppSecret) == "" {
		return nil, errors.New("feishu app credentials are required for card interactions")
	}
	if config.Handler == nil {
		return nil, errors.New("an interaction handler is required")
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	worker := &InteractionWorker{handler: config.Handler, logger: logger}
	eventDispatcher := dispatcher.NewEventDispatcher("", "")
	eventDispatcher.OnP2CardActionTrigger(worker.handleCardAction)
	worker.client = larkws.NewClient(config.AppID, config.AppSecret,
		larkws.WithEventHandler(eventDispatcher),
		larkws.WithLogLevel(larkcore.LogLevelError),
		larkws.WithOnReady(func() { logger.Info("feishu long connection ready") }),
		larkws.WithOnError(func(err error) { logger.Warn("feishu long connection error", "error", err) }),
		larkws.WithOnReconnected(func() { logger.Info("feishu long connection reconnected") }),
		larkws.WithOnDisconnected(func() { logger.Warn("feishu long connection disconnected") }),
	)
	return worker, nil
}

func (w *InteractionWorker) Run(ctx context.Context) error {
	if w == nil || w.client == nil {
		return errors.New("interaction worker is not configured")
	}
	return w.client.Start(ctx)
}

func (w *InteractionWorker) handleCardAction(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	interaction, ok := interactionFromCardAction(event)
	if !ok {
		return toastResponse("error", "This action is not supported."), nil
	}
	result, err := w.handler.Handle(ctx, interaction)
	if err != nil {
		w.logger.Error("could not handle card interaction", "error", err)
		return toastResponse("error", "This action could not be applied."), nil
	}
	if result.Notification == nil {
		text := result.Toast
		if text == "" {
			text = "This action was already handled."
		}
		return toastResponse(interactionToastType(result.Status), text), nil
	}
	cardJSON, err := RenderCard(*result.Notification)
	if err != nil {
		w.logger.Error("could not render updated card", "error", err)
		return toastResponse("error", "This action could not be displayed."), nil
	}
	var card map[string]any
	if err := json.Unmarshal(cardJSON, &card); err != nil {
		w.logger.Error("could not decode updated card", "error", err)
		return toastResponse("error", "This action could not be displayed."), nil
	}
	response := toastResponse("success", result.Toast)
	response.Card = &callback.Card{Type: "raw", Data: card}
	return response, nil
}

func interactionFromCardAction(event *callback.CardActionTriggerEvent) (domain.Interaction, bool) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return domain.Interaction{}, false
	}
	interaction := domain.Interaction{}
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		interaction.EventID = event.EventV2Base.Header.EventID
	}
	if event.Event.Context != nil {
		interaction.MessageID = event.Event.Context.OpenMessageID
	}
	if event.Event.Operator != nil {
		interaction.ActorID = event.Event.Operator.OpenID
	}
	value := make(map[string]string, len(event.Event.Action.Value))
	for key, item := range event.Event.Action.Value {
		if text, ok := item.(string); ok {
			value[key] = text
		}
	}
	interaction.Action = value["action"]
	delete(value, "action")
	interaction.Value = value
	if interaction.MessageID == "" || interaction.Action == "" {
		return domain.Interaction{}, false
	}
	return interaction, true
}

func toastResponse(kind, content string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: content}}
}

func interactionToastType(status string) string {
	switch status {
	case domain.InteractionApplied:
		return "success"
	case domain.InteractionRejected:
		return "error"
	default:
		return "info"
	}
}
