package feishu

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func RenderCard(notification domain.Notification) ([]byte, error) {
	title := notification.Title
	if title == "" {
		title = "GitLab 알림"
	}
	template := "blue"
	if notification.Kind == domain.EventKindPipeline && notification.Action == "failed" {
		template = "red"
	} else if notification.Kind == domain.EventKindPipeline && notification.Action == "success" {
		template = "green"
	}

	elements := []any{
		map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": notification.Summary,
			},
		},
	}
	if notification.SourceText != "" {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": "Content: " + truncate(notification.SourceText, 1000),
			},
		})
	}
	if len(notification.Facts) > 0 {
		elements = append(elements, map[string]any{"tag": "hr"})
		keys := orderedFactKeys(notification.Facts)
		for _, key := range keys {
			elements = append(elements, map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": key + ": " + notification.Facts[key],
				},
			})
		}
	}
	if len(notification.FailedJobs) > 0 {
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": "Failed Jobs: " + joinLimited(notification.FailedJobs, 8),
			},
		})
	}
	if len(notification.Reasons) > 1 {
		elements = append(elements, map[string]any{"tag": "hr"})
		reasons := make([]string, 0, len(notification.Reasons))
		for _, reason := range notification.Reasons {
			reasons = append(reasons, reason.Text)
		}
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": "Related: " + strings.Join(reasons, " · "),
			},
		})
	}
	if validURL(notification.URL) {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{
				map[string]any{
					"tag":  "button",
					"type": "primary",
					"text": map[string]any{"tag": "plain_text", "content": openButtonLabel(notification)},
					"url":  notification.URL,
				},
			},
		})
	}
	card := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": template,
			"title":    map[string]any{"tag": "plain_text", "content": title},
		},
		"elements": elements,
	}
	return json.Marshal(card)
}

var factOrder = []string{"Project", "Title", "Branch", "Source", "State", "Status", "Triggered by", "Changed by", "Commented by"}

func orderedFactKeys(facts map[string]string) []string {
	keys := make([]string, 0, len(facts))
	seen := make(map[string]struct{}, len(facts))
	for _, key := range factOrder {
		if _, ok := facts[key]; ok {
			keys = append(keys, key)
			seen[key] = struct{}{}
		}
	}
	remaining := make([]string, 0, len(facts)-len(keys))
	for key := range facts {
		if _, ok := seen[key]; !ok {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	return append(keys, remaining...)
}

func joinLimited(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, ", ")
	}
	return strings.Join(values[:limit], ", ") + fmt.Sprintf(" +%d more", len(values)-limit)
}

func openButtonLabel(notification domain.Notification) string {
	switch notification.Kind {
	case domain.EventKindPipeline:
		return "Open Pipeline"
	case domain.EventKindMergeRequest:
		return "Open Merge Request"
	case domain.EventKindIssue:
		return "Open Issue"
	default:
		return "Open in GitLab"
	}
}

func validURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return fmt.Sprintf("%s…", string(runes[:limit]))
}
