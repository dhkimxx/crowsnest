package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestStoreEnqueueClaimAndDeliver(t *testing.T) {
	store := openTestStore(t)
	event := testEvent("event-1")
	delivery := domain.Delivery{
		Key: "delivery-1",
		Notification: domain.Notification{
			EventKey:  event.EventKey,
			Kind:      domain.EventKindPipeline,
			Action:    "failed",
			Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "user@example.com"},
		},
	}
	if err := store.Enqueue(context.Background(), event, []domain.Delivery{delivery}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := store.Enqueue(context.Background(), event, []domain.Delivery{delivery}); err != nil {
		t.Fatalf("duplicate Enqueue() error = %v", err)
	}

	claimed, err := store.Claim(context.Background(), "worker-1", 10)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].Key != delivery.Key {
		t.Fatalf("claimed = %#v", claimed)
	}
	claimedAgain, err := store.Claim(context.Background(), "worker-1", 10)
	if err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("claimedAgain = %#v", claimedAgain)
	}
	if err := store.MarkDelivered(context.Background(), delivery.Key, domain.DeliveryReceipt{ProviderMessageID: "message-1"}); err != nil {
		t.Fatalf("MarkDelivered() error = %v", err)
	}
	claimedAfterDelivery, err := store.Claim(context.Background(), "worker-1", 10)
	if err != nil {
		t.Fatalf("Claim() after delivery error = %v", err)
	}
	if len(claimedAfterDelivery) != 0 {
		t.Fatalf("claimedAfterDelivery = %#v", claimedAfterDelivery)
	}
}

func TestStoreRetryableFailureUsesBackoffAndEventuallyStops(t *testing.T) {
	store := openTestStoreWithConfig(t, Config{LeaseDuration: time.Minute, MaxAttempts: 2, RetryDelay: time.Nanosecond})
	event := testEvent("event-retry")
	delivery := domain.Delivery{
		Key:          "delivery-retry",
		Notification: domain.Notification{EventKey: event.EventKey, Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "user@example.com"}},
	}
	if err := store.Enqueue(context.Background(), event, []domain.Delivery{delivery}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, err := store.Claim(context.Background(), "worker-1", 1); err != nil {
		t.Fatalf("first Claim() error = %v", err)
	}
	if err := store.MarkFailed(context.Background(), delivery.Key, domain.DeliveryFailure{Class: "server", Retryable: true, Message: "temporary"}); err != nil {
		t.Fatalf("first MarkFailed() error = %v", err)
	}
	if _, err := store.Claim(context.Background(), "worker-1", 1); err != nil {
		t.Fatalf("second Claim() error = %v", err)
	}
	if err := store.MarkFailed(context.Background(), delivery.Key, domain.DeliveryFailure{Class: "server", Retryable: true, Message: "temporary"}); err != nil {
		t.Fatalf("second MarkFailed() error = %v", err)
	}
	var status string
	if err := store.db.QueryRow(`SELECT status FROM deliveries WHERE delivery_key = ?`, delivery.Key).Scan(&status); err != nil {
		t.Fatalf("read delivery status: %v", err)
	}
	if status != "failed" {
		t.Fatalf("status = %q", status)
	}
}

func TestStoreDoesNotRetryPermanentFailure(t *testing.T) {
	store := openTestStore(t)
	event := testEvent("event-permanent")
	delivery := domain.Delivery{
		Key:          "delivery-permanent",
		Notification: domain.Notification{EventKey: event.EventKey, Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "user@example.com"}},
	}
	if err := store.Enqueue(context.Background(), event, []domain.Delivery{delivery}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, err := store.Claim(context.Background(), "worker-1", 1); err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if err := store.MarkFailed(context.Background(), delivery.Key, domain.DeliveryFailure{Class: "recipient_not_found", Retryable: false, Message: "unknown recipient"}); err != nil {
		t.Fatalf("MarkFailed() error = %v", err)
	}
	claimed, err := store.Claim(context.Background(), "worker-1", 1)
	if err != nil {
		t.Fatalf("Claim() after permanent failure error = %v", err)
	}
	if len(claimed) != 0 {
		t.Fatalf("claimed = %#v", claimed)
	}
}

func TestStoreIdentityResolutionAndPreferences(t *testing.T) {
	store := openTestStore(t)
	identity := domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "carol", Name: "Carol", Email: "carol@example.com"}
	if err := store.Upsert(context.Background(), domain.ProviderGitLab, identity, true); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	resolved, err := store.Resolve(context.Background(), []domain.Identity{{Provider: domain.ProviderGitLab, ProviderID: "42"}})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(resolved) != 1 || resolved[0].Email != identity.Email {
		t.Fatalf("resolved = %#v", resolved)
	}
	enabled, err := store.Enabled(context.Background(), domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: identity.Email}, domain.EventKindIssue, "issue_updated", "group/project")
	if err != nil {
		t.Fatalf("Enabled() error = %v", err)
	}
	if enabled {
		t.Fatal("issue_updated should be disabled by default")
	}
	enabled, err = store.Enabled(context.Background(), domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: identity.Email}, domain.EventKindPipeline, "ci_failed", "group/project")
	if err != nil {
		t.Fatalf("Enabled() error = %v", err)
	}
	if !enabled {
		t.Fatal("ci_failed should be enabled by default")
	}
	if err := store.UpsertPreference(context.Background(), Preference{
		Email:             identity.Email,
		Enabled:           true,
		CIFailed:          false,
		CIRecovered:       true,
		MRReviewRequested: true,
		MRAssigned:        true,
		MRUpdated:         true,
		MRApproved:        true,
		MRStateChanged:    true,
		MRComment:         true,
		Mention:           true,
		IssueAssigned:     true,
		IssueUpdated:      false,
	}); err != nil {
		t.Fatalf("UpsertPreference() error = %v", err)
	}
	enabled, err = store.Enabled(context.Background(), domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: identity.Email}, domain.EventKindPipeline, "ci_failed", "group/project")
	if err != nil {
		t.Fatalf("Enabled() after preference error = %v", err)
	}
	if enabled {
		t.Fatal("ci_failed should be disabled by preference")
	}
}

func TestStoreRejectsIdentityMappingConflict(t *testing.T) {
	store := openTestStore(t)
	base := domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "alice", Email: "alice@example.com"}
	if err := store.Upsert(context.Background(), domain.ProviderGitLab, base, true); err != nil {
		t.Fatalf("first Upsert() error = %v", err)
	}
	conflict := domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "99", Username: "alice", Email: "other@example.com"}
	if err := store.Upsert(context.Background(), domain.ProviderGitLab, conflict, true); err == nil {
		t.Fatal("conflicting Upsert() error = nil")
	}
}

func TestStoreRecordsUnresolvedIdentity(t *testing.T) {
	store := openTestStore(t)
	identity := domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "99", Username: "unknown"}
	if err := store.Record(context.Background(), "event-unresolved", []domain.Identity{identity}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	var occurrences int
	if err := store.db.QueryRow(`SELECT occurrences FROM unresolved_identities WHERE event_key = ? AND provider_id = ?`, "event-unresolved", "99").Scan(&occurrences); err != nil {
		t.Fatalf("read unresolved identity: %v", err)
	}
	if occurrences != 1 {
		t.Fatalf("occurrences = %d", occurrences)
	}
}

func TestStorePipelineState(t *testing.T) {
	store := openTestStore(t)
	want := domain.PipelineState{
		CorrelationKey: "gitlab:76:ref:main",
		Status:         "failed",
		Recipients: []domain.RecipientAddress{{
			Kind:  domain.AddressKindEmail,
			Value: "user@example.com",
		}},
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Put(context.Background(), want); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	got, err := store.Get(context.Background(), want.CorrelationKey)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == nil || got.Status != want.Status || len(got.Recipients) != 1 || got.Recipients[0].Value != "user@example.com" {
		t.Fatalf("got = %#v", got)
	}
}

func openTestStore(t *testing.T) *Store {
	return openTestStoreWithConfig(t, DefaultConfig())
}

func openTestStoreWithConfig(t *testing.T, config Config) *Store {
	t.Helper()
	store, err := Open(t.TempDir()+"/crowsnest.sqlite3", config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testEvent(key string) domain.CanonicalEvent {
	return domain.CanonicalEvent{
		EventKey:      key,
		Source:        domain.ProviderGitLab,
		SourceVersion: "17.6",
		SourceEvent:   "Pipeline Hook",
		Kind:          domain.EventKindPipeline,
		Action:        "failed",
		ReceivedAt:    time.Now().UTC(),
		Project:       domain.ProjectRef{ID: "76", Path: "group/project"},
		Object:        domain.ResourceRef{Kind: domain.EventKindPipeline, ID: "31"},
	}
}
