package application

import (
	"testing"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestRecipientPolicyAllowsAllWhenAllowlistIsEmpty(t *testing.T) {
	policy := NewRecipientPolicy(nil)
	if !policy.Allows(domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "anyone@example.com"}) {
		t.Fatal("empty allowlist should allow all recipients")
	}
}

func TestRecipientPolicyMatchesEmailCaseInsensitively(t *testing.T) {
	policy := NewRecipientPolicy([]string{" Carol.Kim@example.com "})
	if !policy.Allows(domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "carol@example.com"}) {
		t.Fatal("allowlisted recipient was blocked")
	}
	if policy.Allows(domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "other@example.com"}) {
		t.Fatal("recipient outside allowlist was allowed")
	}
}
