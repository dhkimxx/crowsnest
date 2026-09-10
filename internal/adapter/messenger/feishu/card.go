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
	if len(notification.FailedJobs) > 0 || len(notification.FailedJobLinks) > 0 {
		elements = append(elements, map[string]any{"tag": "hr"})
		failedJobsTag, failedJobsContent := failedJobsText(notification)
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     failedJobsTag,
				"content": failedJobsContent,
			},
		})
	}
	if notification.SourceText != "" {
		elements = append(elements, map[string]any{"tag": "hr"})
		elements = append(elements, map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "plain_text",
				"content": sourceTextLabel(notification) + ": " + truncate(notification.SourceText, 1000),
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
	if len(notification.RelatedLinks) > 0 {
		if relatedLinks := renderLinks(notification.RelatedLinks, 4); relatedLinks != "" {
			elements = append(elements, map[string]any{"tag": "hr"})
			elements = append(elements, map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "lark_md",
					"content": "Links: " + relatedLinks,
				},
			})
		}
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

var factOrder = []string{"Project", "Title", "Changed", "Branch", "Source", "State", "Status", "Commit author", "Triggered by", "Changed by", "Commented by"}

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
	case domain.EventKindNote:
		return "Open Comment"
	default:
		return "Open in GitLab"
	}
}

func sourceTextLabel(notification domain.Notification) string {
	switch notification.Kind {
	case domain.EventKindPipeline:
		return "Commit"
	case domain.EventKindNote:
		return "Comment"
	case domain.EventKindMergeRequest, domain.EventKindIssue:
		return "Description"
	default:
		return "Content"
	}
}

func failedJobsText(notification domain.Notification) (string, string) {
	if links, hasLink := renderFailedJobs(notification, 8); hasLink {
		return "lark_md", "Failed Jobs: " + links
	}
	return "plain_text", "Failed Jobs: " + joinLimited(notification.FailedJobs, 8)
}

func renderFailedJobs(notification domain.Notification, limit int) (string, bool) {
	if limit <= 0 {
		return "", false
	}
	linksByLabel := make(map[string][]domain.NotificationLink)
	for _, link := range notification.FailedJobLinks {
		if _, ok := renderLink(link); !ok {
			continue
		}
		linksByLabel[link.Label] = append(linksByLabel[link.Label], link)
	}

	parts := make([]string, 0, min(len(notification.FailedJobs), limit))
	linked := false
	omitted := 0
	for _, name := range notification.FailedJobs {
		if name == "" {
			continue
		}
		if len(parts) == limit {
			omitted++
			continue
		}
		if candidates := linksByLabel[name]; len(candidates) > 0 {
			if rendered, ok := renderLink(candidates[0]); ok {
				parts = append(parts, rendered)
				linked = true
				linksByLabel[name] = candidates[1:]
				continue
			}
		}
		parts = append(parts, escapeMarkdownText(truncate(name, 120)))
	}
	if len(notification.FailedJobs) == 0 {
		for _, link := range notification.FailedJobLinks {
			if len(parts) == limit {
				omitted++
				continue
			}
			if rendered, ok := renderLink(link); ok {
				parts = append(parts, rendered)
				linked = true
			}
		}
	}
	if omitted > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", omitted))
	}
	return strings.Join(parts, " · "), linked
}

func renderLinks(links []domain.NotificationLink, limit int) string {
	if limit <= 0 {
		return ""
	}
	values := make([]string, 0, min(len(links), limit))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		if len(values) == limit {
			break
		}
		if link.Label == "" || !validURL(link.URL) {
			continue
		}
		key := link.Label + "\x00" + link.URL
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if rendered, ok := renderLink(link); ok {
			values = append(values, rendered)
		}
	}
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, " · ")
}

func renderLink(link domain.NotificationLink) (string, bool) {
	if link.Label == "" || !validURL(link.URL) {
		return "", false
	}
	return "[" + escapeMarkdownText(truncate(link.Label, 120)) + "](" + escapeMarkdownURL(link.URL) + ")", true
}

func escapeMarkdownText(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"`", "\\`",
	)
	return replacer.Replace(value)
}

func escapeMarkdownURL(value string) string {
	return strings.NewReplacer("(", "%28", ")", "%29").Replace(value)
}

func validURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) && parsed.Host != ""
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return fmt.Sprintf("%s…", string(runes[:limit]))
}
