package application

import (
	"net/mail"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

type RecipientPolicy struct {
	allowAll bool
	allowed  map[string]struct{}
}

func NewRecipientPolicy(emails []string) *RecipientPolicy {
	policy := &RecipientPolicy{allowAll: len(emails) == 0, allowed: make(map[string]struct{}, len(emails))}
	for _, email := range emails {
		email = normalizeRecipientEmail(email)
		if email != "" {
			policy.allowed[email] = struct{}{}
		}
	}
	return policy
}

func (p *RecipientPolicy) Allows(address domain.RecipientAddress) bool {
	if p == nil || p.allowAll {
		return true
	}
	_, ok := p.allowed[normalizeRecipientEmail(address.Value)]
	return ok
}

func normalizeRecipientEmail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || !strings.Contains(value, "@") {
		return ""
	}
	return value
}
