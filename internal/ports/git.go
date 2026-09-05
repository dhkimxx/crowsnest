package ports

import (
	"context"
	"errors"
	"net/http"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

var ErrIgnoredEvent = errors.New("ignored provider event")

type WebhookDecoder interface {
	Provider() domain.Provider
	Version() string
	Decode(context.Context, http.Header, []byte) (domain.CanonicalEvent, error)
}

type UserRecord struct {
	Identity domain.Identity
	Active   bool
}

type UserDirectory interface {
	ListUsers(context.Context) ([]UserRecord, error)
}

type HookReconcileOptions struct {
	DryRun bool
}

type HookController interface {
	Reconcile(context.Context, HookReconcileOptions) (domain.HookReconcileReport, error)
}
