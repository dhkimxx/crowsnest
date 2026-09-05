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
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": truncate(notification.SourceText, 1000),
			},
		})
	}
	if len(notification.Facts) > 0 {
		keys := make([]string, 0, len(notification.Facts))
		for key := range notification.Facts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
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
				"content": "실패 Job: " + strings.Join(notification.FailedJobs, ", "),
			},
		})
	}
	if len(notification.Reasons) > 0 {
		reasons := make([]string, 0, len(notification.Reasons))
		for _, reason := range notification.Reasons {
			reasons = append(reasons, reason.Text)
		}
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": "수신 이유: " + strings.Join(reasons, ", "),
			},
		})
	}
	if validURL(notification.URL) {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{
				map[string]any{
					"tag":  "button",
					"type": "primary",
					"text": map[string]any{"tag": "plain_text", "content": "GitLab에서 확인"},
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
