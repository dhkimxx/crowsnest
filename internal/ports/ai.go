package ports

import (
	"context"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

type AIEnricher interface {
	Enrich(context.Context, domain.CanonicalEvent, domain.Notification) (domain.AIAnnotation, error)
}
