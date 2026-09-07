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
	for _, expected := range []string{"Merge Request updated", "Content: Please review this change.", "Project: group/project", "Branch: feature/api → main", "Open Merge Request"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("card does not contain %q: %s", expected, content)
		}
	}
	if strings.Contains(content, "Reason:") || strings.Contains(content, "Related:") {
		t.Fatalf("single reason should be represented by the card title: %s", content)
	}
}
