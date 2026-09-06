package application

import (
	"context"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/adapter/database/sqlite"
	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

func TestIdentitySyncEnablesOnlyActiveFeishuUsers(t *testing.T) {
	store, err := sqlite.Open(":memory:", sqlite.DefaultConfig())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	service := NewIdentitySyncService(
		fakeUserDirectory{users: []ports.UserRecord{
			{Identity: domain.Identity{ProviderID: "1", Username: "alice", Name: "Alice", Email: "Alice@example.com"}, Active: true},
			{Identity: domain.Identity{ProviderID: "2", Username: "bob", Name: "Bob", Email: "bob@example.com"}, Active: true},
			{Identity: domain.Identity{ProviderID: "3", Username: "carol", Name: "Carol", Email: "carol@example.com"}, Active: false},
			{Identity: domain.Identity{ProviderID: "4", Username: "vendor", Name: "Vendor", Email: "vendor@external.test"}, Active: true},
		}},
		fakeEmailDirectory{users: map[string]ports.DirectoryUser{
			"alice@example.com": {Email: "alice@example.com", ID: "ou_alice"},
		}},
		store,
		[]string{"example.com"},
	)

	report, err := service.Sync(context.Background(), false)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if report.GitLabUsers != 4 || report.EligibleUsers != 3 || report.FeishuUsersFound != 1 || report.MappingsConsidered != 3 || report.MappingsUpserted != 3 || report.MappingsEnabled != 1 || report.MappingsDisabled != 2 || report.SkippedDomain != 1 || report.SkippedInactive != 1 {
		t.Fatalf("report = %#v", report)
	}

	resolved, err := store.Resolve(context.Background(), []domain.Identity{
		{Provider: domain.ProviderGitLab, ProviderID: "1"},
		{Provider: domain.ProviderGitLab, ProviderID: "2"},
		{Provider: domain.ProviderGitLab, ProviderID: "3"},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 3 || resolved[0].Email != "alice@example.com" || resolved[1].Email != "" || resolved[2].Email != "" {
		t.Fatalf("resolved = %#v", resolved)
	}
}

func TestIdentitySyncDryRunDoesNotChangeMappings(t *testing.T) {
	store, err := sqlite.Open(":memory:", sqlite.DefaultConfig())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if err := store.Upsert(context.Background(), domain.ProviderGitLab, domain.Identity{ProviderID: "1", Username: "alice", Email: "old@example.com"}, true); err != nil {
		t.Fatalf("seed mapping: %v", err)
	}

	service := NewIdentitySyncService(
		fakeUserDirectory{users: []ports.UserRecord{{Identity: domain.Identity{ProviderID: "1", Username: "alice", Email: "alice@example.com"}, Active: true}}},
		fakeEmailDirectory{users: map[string]ports.DirectoryUser{}},
		store,
		[]string{"example.com"},
	)
	if _, err := service.Sync(context.Background(), true); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	resolved, err := store.Resolve(context.Background(), []domain.Identity{{Provider: domain.ProviderGitLab, ProviderID: "1"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 1 || resolved[0].Email != "old@example.com" {
		t.Fatalf("dry-run changed mapping: %#v", resolved)
	}
}

type fakeUserDirectory struct {
	users []ports.UserRecord
}

func (d fakeUserDirectory) ListUsers(context.Context) ([]ports.UserRecord, error) {
	return d.users, nil
}

type fakeEmailDirectory struct {
	users map[string]ports.DirectoryUser
}

func (d fakeEmailDirectory) LookupEmails(_ context.Context, emails []string) (map[string]ports.DirectoryUser, error) {
	result := make(map[string]ports.DirectoryUser)
	for _, email := range emails {
		if user, ok := d.users[email]; ok {
			result[email] = user
		}
	}
	return result, nil
}
