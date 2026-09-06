package application

import (
	"context"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestRouterRoutesPipelineFailureToCommitAuthor(t *testing.T) {
	router := NewRouter(nil, nil, &fakePipelineStateStore{}, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "pipeline-1",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindPipeline,
		Action:   "failed",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7", Email: "runner@example.com"},
		Object:   domain.ResourceRef{Kind: domain.EventKindPipeline, ID: "31", URL: "https://gitlab.example/pipeline/31"},
		Pipeline: &domain.PipelineDetails{
			Status:       "failed",
			Ref:          "main",
			CommitAuthor: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Email: "carol@example.com"},
		},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 1 {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
	delivery := result.Deliveries[0]
	if delivery.Notification.Recipient.Value != "carol@example.com" || delivery.Notification.Reasons[0].Code != ReasonCIFailed {
		t.Fatalf("delivery = %#v", delivery)
	}
	if result.PipelineState == nil || result.PipelineState.Status != "failed" || len(result.PipelineState.Recipients) != 1 {
		t.Fatalf("pipeline state = %#v", result.PipelineState)
	}
}

func TestRouterOnlyCreatesAllowlistedDeliveries(t *testing.T) {
	router := NewRouterWithRecipientPolicy(nil, nil, nil, []string{"example.com"}, NewRecipientPolicy([]string{"carol@example.com"}))
	event := domain.CanonicalEvent{
		EventKey: "allowlist-1",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindNote,
		Action:   "create",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7", Email: "bob@example.com"},
		Object:   domain.ResourceRef{Kind: domain.EventKindMergeRequest, IID: "12"},
		Note: &domain.NoteDetails{
			ID: "102",
			MergeRequest: &domain.MergeRequestDetails{
				IID:    "12",
				Author: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Email: "alice@example.com"},
			},
		},
		Mentions: []domain.Identity{
			{Provider: domain.ProviderGitLab, ProviderID: "99", Email: "carol@example.com"},
		},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 1 || result.Deliveries[0].Notification.Recipient.Value != "carol@example.com" {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
}

func TestRouterFallsBackToPipelineActorWhenCommitAuthorCannotResolve(t *testing.T) {
	router := NewRouter(nil, nil, &fakePipelineStateStore{}, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "pipeline-fallback",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindPipeline,
		Action:   "failed",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7", Email: "runner@example.com"},
		Pipeline: &domain.PipelineDetails{
			Status:       "failed",
			CommitAuthor: domain.Identity{Name: "Private Commit Author"},
		},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 1 || result.Deliveries[0].Notification.Recipient.Value != "runner@example.com" {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
}

func TestRouterRoutesPipelineRecoveryOnlyAfterFailure(t *testing.T) {
	stateStore := &fakePipelineStateStore{state: &domain.PipelineState{
		CorrelationKey: "gitlab:76:ref:main",
		Status:         "failed",
		Recipients: []domain.RecipientAddress{{
			Kind:  domain.AddressKindEmail,
			Value: "carol@example.com",
		}},
	}}
	router := NewRouter(nil, nil, stateStore, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "pipeline-2",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindPipeline,
		Action:   "success",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Object:   domain.ResourceRef{Kind: domain.EventKindPipeline, ID: "32"},
		Pipeline: &domain.PipelineDetails{Status: "success", Ref: "main"},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 1 || result.Deliveries[0].Notification.Reasons[0].Code != ReasonCIRecovered {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
	if result.PipelineState == nil || result.PipelineState.Status != "success" {
		t.Fatalf("pipeline state = %#v", result.PipelineState)
	}
}

func TestRouterCombinesCommentAndMentionWithoutSelfNotification(t *testing.T) {
	router := NewRouter(nil, nil, nil, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "note-1",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindNote,
		Action:   "create",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7", Username: "bob", Email: "bob@example.com"},
		Object:   domain.ResourceRef{Kind: domain.EventKindMergeRequest, IID: "12", URL: "https://gitlab.example/mr/12"},
		Note: &domain.NoteDetails{
			ID: "100",
			MergeRequest: &domain.MergeRequestDetails{
				IID:    "12",
				Title:  "Improve API",
				Author: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "alice", Email: "alice@example.com"},
			},
		},
		Mentions: []domain.Identity{
			{Provider: domain.ProviderGitLab, ProviderID: "99", Username: "carol", Email: "carol@example.com"},
			{Provider: domain.ProviderGitLab, ProviderID: "7", Username: "bob", Email: "bob@example.com"},
		},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 2 {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
	byEmail := map[string]domain.Notification{}
	for _, delivery := range result.Deliveries {
		byEmail[delivery.Notification.Recipient.Value] = delivery.Notification
	}
	if len(byEmail["alice@example.com"].Reasons) != 1 || byEmail["alice@example.com"].Reasons[0].Code != ReasonMRComment {
		t.Fatalf("alice notification = %#v", byEmail["alice@example.com"])
	}
	if len(byEmail["carol@example.com"].Reasons) != 1 || byEmail["carol@example.com"].Reasons[0].Code != ReasonMention {
		t.Fatalf("carol notification = %#v", byEmail["carol@example.com"])
	}
	if _, ok := byEmail["bob@example.com"]; ok {
		t.Fatal("actor should not receive a self mention notification")
	}
}

func TestRouterCombinesMultipleReasonsForOneRecipient(t *testing.T) {
	router := NewRouter(nil, nil, nil, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "note-2",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindNote,
		Action:   "create",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7", Username: "bob", Email: "bob@example.com"},
		Object:   domain.ResourceRef{Kind: domain.EventKindMergeRequest, IID: "12"},
		Note: &domain.NoteDetails{
			ID: "101",
			MergeRequest: &domain.MergeRequestDetails{
				IID:    "12",
				Author: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "alice", Email: "alice@example.com"},
			},
		},
		Mentions: []domain.Identity{{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "alice", Email: "alice@example.com"}},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 1 || len(result.Deliveries[0].Notification.Reasons) != 2 {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
}

func TestRouterDoesNotNotifyForSystemNote(t *testing.T) {
	router := NewRouter(nil, nil, nil, nil)
	event := domain.CanonicalEvent{
		EventKey: "system-note-1",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindNote,
		Action:   "create",
		Project:  domain.ProjectRef{ID: "76"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7"},
		Note: &domain.NoteDetails{
			ID:     "101",
			System: true,
			MergeRequest: &domain.MergeRequestDetails{
				Author: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Email: "author@example.com"},
			},
		},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 0 {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
}

func TestRouterResolvesReviewerByIdentityStore(t *testing.T) {
	identityStore := &fakeIdentityStore{mapped: map[string]domain.Identity{
		"id:99": {Provider: domain.ProviderGitLab, ProviderID: "99", Username: "carol", Email: "carol@example.com", Name: "Carol"},
	}}
	router := NewRouter(identityStore, nil, nil, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "mr-1",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindMergeRequest,
		Action:   "update",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "alice"},
		Object:   domain.ResourceRef{Kind: domain.EventKindMergeRequest, IID: "12"},
		MergeRequest: &domain.MergeRequestDetails{
			IID:    "12",
			Title:  "Improve API",
			Author: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Username: "alice"},
		},
		Changes: []domain.Change{{
			Field: "reviewers",
			Added: []domain.Identity{{Provider: domain.ProviderGitLab, ProviderID: "99", Username: "carol"}},
		}},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 1 || result.Deliveries[0].Notification.Recipient.Value != "carol@example.com" {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
}

func TestRouterIgnoresMergeRequestUpdateWithoutMeaningfulChanges(t *testing.T) {
	router := NewRouter(nil, nil, nil, []string{"example.com"})
	event := domain.CanonicalEvent{
		EventKey: "mr-noop",
		Source:   domain.ProviderGitLab,
		Kind:     domain.EventKindMergeRequest,
		Action:   "update",
		Project:  domain.ProjectRef{ID: "76", Path: "group/project"},
		Actor:    domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "7", Email: "actor@example.com"},
		Object:   domain.ResourceRef{Kind: domain.EventKindMergeRequest, IID: "12"},
		MergeRequest: &domain.MergeRequestDetails{
			IID:    "12",
			Author: domain.Identity{Provider: domain.ProviderGitLab, ProviderID: "42", Email: "author@example.com"},
		},
	}
	result, err := router.Route(context.Background(), event)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if len(result.Deliveries) != 0 {
		t.Fatalf("deliveries = %#v", result.Deliveries)
	}
}

type fakeIdentityStore struct {
	mapped map[string]domain.Identity
}

func (s *fakeIdentityStore) Resolve(_ context.Context, identities []domain.Identity) ([]domain.Identity, error) {
	result := make([]domain.Identity, 0, len(identities))
	for _, identity := range identities {
		key := "id:" + identity.ProviderID
		if mapped, ok := s.mapped[key]; ok {
			result = append(result, mapped)
		} else {
			result = append(result, identity)
		}
	}
	return result, nil
}

func (s *fakeIdentityStore) Upsert(context.Context, domain.Provider, domain.Identity, bool) error {
	return nil
}

type fakePipelineStateStore struct {
	state *domain.PipelineState
}

func (s *fakePipelineStateStore) Get(context.Context, string) (*domain.PipelineState, error) {
	return s.state, nil
}

func (s *fakePipelineStateStore) Put(_ context.Context, state domain.PipelineState) error {
	s.state = &state
	return nil
}
