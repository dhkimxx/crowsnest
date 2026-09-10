package feishu

import (
	"strings"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestRenderCardUsesReadableLabelsAndContextualButton(t *testing.T) {
	card, err := RenderCard(domain.Notification{
		Kind:       domain.EventKindMergeRequest,
		Title:      "Merge Request updated",
		Summary:    "group/project · !12 · Updated",
		SourceText: "Please review this change.",
		URL:        "https://gitlab.example/group/project/-/merge_requests/12",
		Facts: map[string]string{
			"Project": "group/project",
			"Branch":  "feature/api → main",
			"Status":  "Updated",
		},
		Reasons: []domain.NotificationReason{{Code: "mr_updated", Text: "Merge Request가 변경되었습니다."}},
	})
	if err != nil {
		t.Fatalf("RenderCard() error = %v", err)
	}
	content := string(card)
	for _, expected := range []string{"Merge Request updated", "Description: Please review this change.", "Project: group/project", "Branch: feature/api → main", "Open Merge Request"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("card does not contain %q: %s", expected, content)
		}
	}
	if strings.Contains(content, "Reason:") || strings.Contains(content, "Related:") {
		t.Fatalf("single reason should be represented by the card title: %s", content)
	}
}

func TestRenderCardMakesFailedJobsAndRelatedResourcesClickable(t *testing.T) {
	card, err := RenderCard(domain.Notification{
		Kind:    domain.EventKindPipeline,
		Title:   "Pipeline failed",
		Summary: "group/project · Pipeline #3 · Failed",
		URL:     "https://gitlab.example/group/project/-/pipelines/31",
		FailedJobs: []string{
			"build-aws-dev",
		},
		FailedJobLinks: []domain.NotificationLink{{
			Label: "build-aws-dev",
			URL:   "https://gitlab.example/group/project/-/jobs/380",
		}},
		RelatedLinks: []domain.NotificationLink{{
			Label: "Open Merge Request",
			URL:   "https://gitlab.example/group/project/-/merge_requests/12",
		}},
	})
	if err != nil {
		t.Fatalf("RenderCard() error = %v", err)
	}
	content := string(card)
	for _, expected := range []string{
		"Failed Jobs: [build-aws-dev](https://gitlab.example/group/project/-/jobs/380)",
		"Links: [Open Merge Request](https://gitlab.example/group/project/-/merge_requests/12)",
		"Open Pipeline",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("card does not contain %q: %s", expected, content)
		}
	}
}

func TestRenderCardKeepsFailedJobNamesWhenOnlySomeLinksAreAvailable(t *testing.T) {
	card, err := RenderCard(domain.Notification{
		Kind:       domain.EventKindPipeline,
		Title:      "Pipeline failed",
		Summary:    "group/project · Pipeline #3 · Failed",
		FailedJobs: []string{"build-aws-dev", "lint"},
		FailedJobLinks: []domain.NotificationLink{{
			Label: "build-aws-dev",
			URL:   "https://gitlab.example/group/project/-/jobs/380",
		}},
	})
	if err != nil {
		t.Fatalf("RenderCard() error = %v", err)
	}
	content := string(card)
	for _, expected := range []string{
		"[build-aws-dev](https://gitlab.example/group/project/-/jobs/380)",
		"lint",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("card does not contain %q: %s", expected, content)
		}
	}
}

func TestRenderCardAcceptsCaseInsensitiveHTTPURL(t *testing.T) {
	card, err := RenderCard(domain.Notification{
		Kind: domain.EventKindPipeline,
		URL:  "HTTPS://gitlab.example/group/project/-/pipelines/31",
	})
	if err != nil {
		t.Fatalf("RenderCard() error = %v", err)
	}
	if !strings.Contains(string(card), "Open Pipeline") {
		t.Fatalf("card = %s", card)
	}
}

func TestRenderCardUsesCommentButton(t *testing.T) {
	card, err := RenderCard(domain.Notification{
		Kind: domain.EventKindNote,
		URL:  "https://gitlab.example/group/project/-/merge_requests/12#note_123",
	})
	if err != nil {
		t.Fatalf("RenderCard() error = %v", err)
	}
	if !strings.Contains(string(card), "Open Comment") {
		t.Fatalf("card = %s", card)
	}
}
