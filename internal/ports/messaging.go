package ports

import (
	"context"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

type Messenger interface {
	Send(context.Context, domain.Notification) (domain.DeliveryReceipt, error)
}

type DirectoryUser struct {
	Email string
	ID    string
}

type EmailDirectory interface {
	LookupEmails(context.Context, []string) (map[string]DirectoryUser, error)
}

type DeliveryError interface {
	DeliveryFailure() domain.DeliveryFailure
}
