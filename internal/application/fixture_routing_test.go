package application

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/adapter/git/gitlab/v17_6"
	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestFixtureRouting(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test source")
	}
	fixtureRoot := filepath.Join(filepath.Dir(sourceFile), "../../testdata/gitlab/v17_6")
	cases := []struct {
		name          string
		file          string
		header        string
		wantRecipient string
		wantReason    string
	}{
		{"pipeline failure", "pipeline_failed.json", "Pipeline Hook", "user@example.com", ReasonCIFailed},
		{"new reviewer", "merge_request_open.json", "Merge Request Hook", "reviewer@example.com", ReasonMRReviewRequested},
		{"reviewer update", "merge_request_update_reviewer.json", "System Hook", "reviewer@example.com", ReasonMRReviewRequested},
		{"MR note", "note_merge_request.json", "Note Hook", "author@example.com", ReasonMRComment},
		{"issue assignee", "issue_update_assignee.json", "Issue Hook", "reviewer@example.com", ReasonIssueAssigned},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(fixtureRoot, test.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			event, err := gitlabv176.NewDecoder().Decode(context.Background(), http.Header{"X-Gitlab-Event": []string{test.header}}, body)
			if err != nil {
				t.Fatalf("decode event: %v", err)
			}
			router := NewRouter(&fixtureIdentityStore{}, nil, nil, nil)
			result, err := router.Route(context.Background(), event)
			if err != nil {
				t.Fatalf("route event: %v", err)
			}
			if !hasDelivery(result.Deliveries, test.wantRecipient, test.wantReason) {
				t.Fatalf("deliveries = %#v", result.Deliveries)
			}
		})
	}
}

func hasDelivery(deliveries []domain.Delivery, email, reason string) bool {
	for _, delivery := range deliveries {
		if delivery.Notification.Recipient.Value != email {
			continue
		}
		for _, candidateReason := range delivery.Notification.Reasons {
			if candidateReason.Code == reason {
				return true
			}
		}
	}
	return false
}

type fixtureIdentityStore struct{}

func (s *fixtureIdentityStore) Resolve(_ context.Context, identities []domain.Identity) ([]domain.Identity, error) {
	result := make([]domain.Identity, 0, len(identities))
	for _, identity := range identities {
		switch identity.ProviderID {
		case "7":
			identity.Email = "author@example.com"
		case "8":
			identity.Email = "commenter@example.com"
		case "99":
			identity.Email = "reviewer@example.com"
		}
		result = append(result, identity)
	}
	return result, nil
}

func (s *fixtureIdentityStore) Upsert(context.Context, domain.Provider, domain.Identity, bool) error {
	return nil
}
