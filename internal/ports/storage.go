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
}

type PipelineStateStore interface {
	Get(context.Context, string) (*domain.PipelineState, error)
	Put(context.Context, domain.PipelineState) error
}

type HookStateStore interface {
	SaveHookState(context.Context, domain.HookState) error
}
