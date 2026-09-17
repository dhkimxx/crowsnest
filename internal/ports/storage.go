package ports

import (
	"context"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

type EventStore interface {
	Enqueue(context.Context, domain.CanonicalEvent, []domain.Delivery) error
}

type IgnoredEventStore interface {
	RecordIgnored(context.Context, string, string, string) error
}

type TransactionalEventStore interface {
	EventStore
	EnqueueWithPipelineState(context.Context, domain.CanonicalEvent, []domain.Delivery, *domain.PipelineState) error
}

type OutboxStore interface {
	Claim(context.Context, string, int) ([]domain.Delivery, error)
	MarkDelivered(context.Context, string, domain.DeliveryReceipt) error
	MarkFailed(context.Context, string, domain.DeliveryFailure) error
}

type IdentityStore interface {
	Resolve(context.Context, []domain.Identity) ([]domain.Identity, error)
	Upsert(context.Context, domain.Provider, domain.Identity, bool) error
}

type IdentitySyncStore interface {
	IdentityStore
	DisableExcept(context.Context, domain.Provider, []string) (int, error)
}

type UnresolvedStore interface {
	Record(context.Context, string, []domain.Identity) error
}

type PreferenceStore interface {
	Enabled(context.Context, domain.RecipientAddress, domain.EventKind, string, string) (bool, error)
	SetReasonEnabled(context.Context, domain.RecipientAddress, string, bool) error
}

type PipelineStateStore interface {
	Get(context.Context, string) (*domain.PipelineState, error)
	Put(context.Context, domain.PipelineState) error
}

type InteractionRecord struct {
	EventID   string
	Provider  domain.Provider
	MessageID string
	ActorID   string
	Action    string
	Result    string
}

type RecordedDelivery struct {
	Key          string
	Notification domain.Notification
}

type InteractionStore interface {
	BeginInteraction(context.Context, InteractionRecord) (bool, error)
	FinishInteraction(context.Context, string, string) error
	DeliveryByMessageID(context.Context, string) (*RecordedDelivery, error)
}

type InteractionHandler interface {
	Handle(context.Context, domain.Interaction) (domain.InteractionResult, error)
}
