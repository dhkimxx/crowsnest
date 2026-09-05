package gitlabv176

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

const version = "17.6"

var (
	mentionPattern = regexp.MustCompile(`(^|[^A-Za-z0-9._%+\-])@([A-Za-z0-9][A-Za-z0-9_.\-]{0,63})\b`)
	fencedCode     = regexp.MustCompile("(?s)```.*?```")
	inlineCode     = regexp.MustCompile("`[^`]*`")
)

type Decoder struct {
	now func() time.Time
}

func NewDecoder() Decoder {
	return Decoder{now: time.Now}
}

func (Decoder) Provider() domain.Provider {
	return domain.ProviderGitLab
}

func (Decoder) Version() string {
	return version
}

func (d Decoder) Decode(ctx context.Context, headers http.Header, body []byte) (domain.CanonicalEvent, error) {
	select {
	case <-ctx.Done():
		return domain.CanonicalEvent{}, ctx.Err()
	default:
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return domain.CanonicalEvent{}, fmt.Errorf("decode GitLab webhook JSON: %w", err)
	}

	sourceEvent := strings.TrimSpace(headers.Get("X-Gitlab-Event"))
	if sourceEvent == "" {
		return domain.CanonicalEvent{}, errors.New("missing X-Gitlab-Event header")
	}
	kind, ignored := eventKind(sourceEvent, payload)
	if ignored {
		return domain.CanonicalEvent{}, ports.ErrIgnoredEvent
	}

	now := time.Now
	if d.now != nil {
		now = d.now
	}
	event := domain.CanonicalEvent{
		EventKey:      eventKey(headers, body),
		Source:        domain.ProviderGitLab,
		SourceVersion: version,
		SourceEvent:   sourceEvent,
		Kind:          kind,
		Action:        actionFor(kind, payload),
		ReceivedAt:    now(),
		Project:       projectRef(mapValue(payload, "project")),
		Actor:         identityFromMap(mapValue(payload, "user")),
		Object:        domain.ResourceRef{Kind: kind},
		Changes:       parseChanges(mapValue(payload, "changes")),
		Metadata:      map[string]string{},
	}
	event.OccurredAt = occurredAt(payload, kind)

	switch kind {
	case domain.EventKindPipeline:
		decodePipeline(&event, payload)
	case domain.EventKindMergeRequest:
		decodeMergeRequest(&event, payload)
	case domain.EventKindNote:
		decodeNote(&event, payload)
	case domain.EventKindIssue:
		decodeIssue(&event, payload)
	default:
		return domain.CanonicalEvent{}, ports.ErrIgnoredEvent
	}

	if len(event.Metadata) == 0 {
		event.Metadata = nil
	}
	return event, nil
}

func eventKind(sourceEvent string, payload map[string]any) (domain.EventKind, bool) {
	switch strings.ToLower(strings.TrimSpace(sourceEvent)) {
	case "pipeline hook":
		return domain.EventKindPipeline, false
	case "merge request hook":
		return domain.EventKindMergeRequest, false
	case "note hook":
		return domain.EventKindNote, false
	case "issue hook":
		return domain.EventKindIssue, false
	case "system hook":
		return kindFromPayload(payload)
	}
	return kindFromPayload(payload)
}

func kindFromPayload(payload map[string]any) (domain.EventKind, bool) {
	value := strings.ToLower(stringValue(payload["object_kind"]))
	if value == "" {
		value = strings.ToLower(stringValue(payload["event_name"]))
	}
	switch value {
	case "pipeline":
		return domain.EventKindPipeline, false
	case "merge_request":
		return domain.EventKindMergeRequest, false
	case "note":
		return domain.EventKindNote, false
	case "issue", "work_item":
		return domain.EventKindIssue, false
	default:
		return "", true
	}
}

func actionFor(kind domain.EventKind, payload map[string]any) string {
	attributes := mapValue(payload, "object_attributes")
	if action := stringValue(attributes["action"]); action != "" {
		return action
	}
	if kind == domain.EventKindPipeline {
		return stringValue(attributes["status"])
	}
	return stringValue(payload["event_name"])
}

func eventKey(headers http.Header, body []byte) string {
	for _, header := range []string{"Idempotency-Key", "X-Gitlab-Event-UUID", "X-Gitlab-Webhook-UUID", "X-Gitlab-Webhook-ID", "X-Gitlab-Event-ID"} {
		if value := strings.TrimSpace(headers.Get(header)); value != "" {
			return "gitlab:header:" + value
		}
	}
	digest := sha256.Sum256(body)
	return "gitlab:body:" + hex.EncodeToString(digest[:])
}

func occurredAt(payload map[string]any, kind domain.EventKind) *time.Time {
	attributes := mapValue(payload, "object_attributes")
	keys := []string{"updated_at", "finished_at", "merged_at", "created_at"}
	if kind == domain.EventKindPipeline {
		keys = []string{"finished_at", "updated_at", "created_at"}
	}
	for _, key := range keys {
		if parsed, ok := parseTime(stringValue(attributes[key])); ok {
			return &parsed
		}
		if parsed, ok := parseTime(stringValue(payload[key])); ok {
			return &parsed
		}
	}
	return nil
}

func parseTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05 MST",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02T15:04:05.000Z",
	}
	for _, format := range formats {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func decodePipeline(event *domain.CanonicalEvent, payload map[string]any) {
	attributes := mapValue(payload, "object_attributes")
	commit := mapValue(payload, "commit")
	commitAuthor := identityFromMap(mapValue(commit, "author"))
	pipeline := &domain.PipelineDetails{
		ID:           stringValue(attributes["id"]),
		IID:          stringValue(attributes["iid"]),
		Name:         stringValue(attributes["name"]),
		Ref:          stringValue(attributes["ref"]),
		SHA:          stringValue(attributes["sha"]),
		Source:       stringValue(attributes["source"]),
		Status:       stringValue(attributes["status"]),
		URL:          firstNonEmpty(stringValue(attributes["url"]), stringValue(attributes["web_url"])),
		CommitAuthor: commitAuthor,
	}
	if mergeRequest := mapValue(payload, "merge_request"); len(mergeRequest) > 0 {
		pipeline.MergeRequest = resourceRef(domain.EventKindMergeRequest, mergeRequest)
	}
	for _, raw := range arrayValue(payload["builds"]) {
		build := mapValue(raw)
		job := domain.PipelineJob{
			ID:           stringValue(build["id"]),
			Name:         stringValue(build["name"]),
			Status:       stringValue(build["status"]),
			AllowFailure: boolValue(build["allow_failure"]),
		}
		if job.Status == "failed" && !job.AllowFailure {
			pipeline.FailedJobs = append(pipeline.FailedJobs, job)
		}
	}
	event.Pipeline = pipeline
	event.Object = domain.ResourceRef{
		Kind:  domain.EventKindPipeline,
		ID:    pipeline.ID,
		IID:   pipeline.IID,
		Title: pipeline.Name,
		URL:   pipeline.URL,
	}
	if hasIdentity(commitAuthor) {
		event.Author = &commitAuthor
	}
	event.SourceText = truncate(stringValue(commit["message"]), 2000)
	event.RawStatus = pipeline.Status
	event.Metadata["ref"] = pipeline.Ref
	event.Metadata["source"] = pipeline.Source
}

func decodeMergeRequest(event *domain.CanonicalEvent, payload map[string]any) {
	attributes := mapValue(payload, "object_attributes")
	mergeRequest := mergeRequestDetails(attributes, payload, event.Project.URL)
	event.MergeRequest = &mergeRequest
	event.Object = domain.ResourceRef{
		Kind:  domain.EventKindMergeRequest,
		ID:    mergeRequest.ID,
		IID:   mergeRequest.IID,
		Title: mergeRequest.Title,
		URL:   mergeRequest.URL,
	}
	event.Reviewers = mergeRequest.Reviewers
	event.Assignees = mergeRequest.Assignees
	if hasIdentity(mergeRequest.Author) {
		event.Author = &mergeRequest.Author
	}
	event.Mentions = extractMentions(mergeRequest.Title, mergeRequest.Description)
	event.SourceText = truncate(firstNonEmpty(mergeRequest.Description, mergeRequest.Title), 2000)
	event.RawStatus = mergeRequest.State
	if oldRev := stringValue(attributes["oldrev"]); oldRev != "" {
		event.Metadata["oldrev"] = oldRev
	}
}

func decodeNote(event *domain.CanonicalEvent, payload map[string]any) {
	attributes := mapValue(payload, "object_attributes")
	note := &domain.NoteDetails{
		ID:           stringValue(attributes["id"]),
		Body:         truncate(stringValue(attributes["note"]), 4000),
		Action:       stringValue(attributes["action"]),
		NoteableType: stringValue(attributes["noteable_type"]),
		URL:          stringValue(attributes["url"]),
		System:       boolValue(attributes["system"]),
	}
	if nested := mapValue(payload, "merge_request"); len(nested) > 0 {
		parsed := mergeRequestDetails(nested, payload, event.Project.URL)
		note.MergeRequest = &parsed
		event.Author = identityPointer(parsed.Author)
		event.Reviewers = parsed.Reviewers
		event.Assignees = parsed.Assignees
		event.Object = domain.ResourceRef{Kind: domain.EventKindMergeRequest, ID: note.ID, IID: parsed.IID, Title: parsed.Title, URL: parsed.URL}
	} else if nested := mapValue(payload, "issue"); len(nested) > 0 {
		parsed := issueDetails(nested, payload, event.Project.URL)
		note.Issue = &parsed
		event.Author = identityPointer(parsed.Author)
		event.Assignees = parsed.Assignees
		event.Object = domain.ResourceRef{Kind: domain.EventKindIssue, ID: note.ID, IID: parsed.IID, Title: parsed.Title, URL: parsed.URL}
	} else {
		event.Object = domain.ResourceRef{Kind: domain.EventKindNote, ID: note.ID, URL: note.URL}
	}
	event.Note = note
	event.Mentions = extractMentions(note.Body)
	event.SourceText = note.Body
	event.Metadata["noteable_type"] = note.NoteableType
}

func decodeIssue(event *domain.CanonicalEvent, payload map[string]any) {
	attributes := mapValue(payload, "object_attributes")
	issuePayload := mapValue(payload, "issue")
	if len(issuePayload) == 0 {
		issuePayload = attributes
	}
	issue := issueDetails(issuePayload, payload, event.Project.URL)
	event.Issue = &issue
	event.Object = domain.ResourceRef{
		Kind:  domain.EventKindIssue,
		ID:    issue.ID,
		IID:   issue.IID,
		Title: issue.Title,
		URL:   issue.URL,
	}
	event.Assignees = issue.Assignees
	if hasIdentity(issue.Author) {
		event.Author = &issue.Author
	}
	event.Mentions = extractMentions(issue.Title, issue.Description)
	event.SourceText = truncate(firstNonEmpty(issue.Description, issue.Title), 2000)
	event.RawStatus = issue.State
}

func mergeRequestDetails(data, payload map[string]any, projectURL string) domain.MergeRequestDetails {
	author := identityFromMap(mapValue(data, "author"))
	if !hasIdentity(author) {
		author = identityFromMap(mapValue(payload, "author"))
	}
	if !hasIdentity(author) {
		author.ProviderID = stringValue(data["author_id"])
	}
	reviewers := identitiesFrom(data["reviewers"])
	if len(reviewers) == 0 {
		reviewers = identitiesFrom(payload["reviewers"])
	}
	assignees := identitiesFrom(data["assignees"])
	if len(assignees) == 0 {
		assignees = identitiesFrom(payload["assignees"])
	}
	if len(assignees) == 0 {
		if assignee := mapValue(payload, "assignee"); len(assignee) > 0 {
			assignees = []domain.Identity{identityFromMap(assignee)}
		}
	}
	iid := stringValue(data["iid"])
	return domain.MergeRequestDetails{
		ID:           stringValue(data["id"]),
		IID:          iid,
		Title:        stringValue(data["title"]),
		Description:  truncate(stringValue(data["description"]), 4000),
		URL:          firstNonEmpty(stringValue(data["url"]), stringValue(data["web_url"]), fallbackURL(projectURL, "merge_requests", iid)),
		SourceBranch: stringValue(data["source_branch"]),
		TargetBranch: stringValue(data["target_branch"]),
		State:        stringValue(data["state"]),
		Author:       author,
		Reviewers:    reviewers,
		Assignees:    assignees,
	}
}

func issueDetails(data, payload map[string]any, projectURL string) domain.IssueDetails {
	author := identityFromMap(mapValue(data, "author"))
	if !hasIdentity(author) {
		author = identityFromMap(mapValue(payload, "author"))
	}
	if !hasIdentity(author) {
		author.ProviderID = stringValue(data["author_id"])
	}
	assignees := identitiesFrom(data["assignees"])
	if len(assignees) == 0 {
		if assignee := mapValue(data, "assignee"); len(assignee) > 0 {
			assignees = []domain.Identity{identityFromMap(assignee)}
		}
	}
	iid := stringValue(data["iid"])
	return domain.IssueDetails{
		ID:          stringValue(data["id"]),
		IID:         iid,
		Title:       stringValue(data["title"]),
		Description: truncate(stringValue(data["description"]), 4000),
		URL:         firstNonEmpty(stringValue(data["url"]), stringValue(data["web_url"]), fallbackURL(projectURL, "issues", iid)),
		State:       stringValue(data["state"]),
		Author:      author,
		Assignees:   assignees,
	}
}

func projectRef(data map[string]any) domain.ProjectRef {
	return domain.ProjectRef{
		ID:            stringValue(data["id"]),
		Path:          firstNonEmpty(stringValue(data["path_with_namespace"]), stringValue(data["path"]), stringValue(data["name"])),
		URL:           firstNonEmpty(stringValue(data["web_url"]), stringValue(data["url"])),
		DefaultBranch: stringValue(data["default_branch"]),
	}
}

func resourceRef(kind domain.EventKind, data map[string]any) *domain.ResourceRef {
	ref := domain.ResourceRef{
		Kind:  kind,
		ID:    stringValue(data["id"]),
		IID:   stringValue(data["iid"]),
		Title: stringValue(data["title"]),
		URL:   firstNonEmpty(stringValue(data["url"]), stringValue(data["web_url"])),
	}
	return &ref
}

func parseChanges(data map[string]any) []domain.Change {
	if len(data) == 0 {
		return nil
	}
	fields := make([]string, 0, len(data))
	for field := range data {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	changes := make([]domain.Change, 0, len(fields))
	for _, field := range fields {
		value := mapValue(data, field)
		if len(value) == 0 {
			continue
		}
		normalizedField := field
		if field == "reviewer_ids" {
			normalizedField = "reviewers"
		}
		if field == "assignee_ids" {
			normalizedField = "assignees"
		}
		change := domain.Change{Field: normalizedField, Before: scalarString(value["previous"]), After: scalarString(value["current"])}
		if normalizedField == "reviewers" || normalizedField == "assignees" {
			before := identitiesFrom(value["previous"])
			after := identitiesFrom(value["current"])
			change.Added, change.Removed = identityDiff(before, after)
			change.Before = ""
			change.After = ""
		}
		if change.Before != "" || change.After != "" || len(change.Added) > 0 || len(change.Removed) > 0 {
			changes = append(changes, change)
		}
	}
	return changes
}

func identityDiff(before, after []domain.Identity) (added, removed []domain.Identity) {
	beforeByKey := make(map[string]domain.Identity, len(before))
	afterByKey := make(map[string]domain.Identity, len(after))
	for _, item := range before {
		beforeByKey[identityKey(item)] = item
	}
	for _, item := range after {
		afterByKey[identityKey(item)] = item
		if _, ok := beforeByKey[identityKey(item)]; !ok {
			added = append(added, item)
		}
	}
	for key, item := range beforeByKey {
		if _, ok := afterByKey[key]; !ok {
			removed = append(removed, item)
		}
	}
	return added, removed
}

func extractMentions(texts ...string) []domain.Identity {
	seen := map[string]struct{}{}
	var mentions []domain.Identity
	for _, text := range texts {
		cleaned := inlineCode.ReplaceAllString(fencedCode.ReplaceAllString(text, " "), " ")
		for _, match := range mentionPattern.FindAllStringSubmatch(cleaned, -1) {
			username := strings.ToLower(strings.TrimSpace(match[2]))
			if username == "" || username == "all" || username == "here" {
				continue
			}
			if _, ok := seen[username]; ok {
				continue
			}
			seen[username] = struct{}{}
			mentions = append(mentions, domain.Identity{Username: username})
		}
	}
	return mentions
}

func identitiesFrom(value any) []domain.Identity {
	items := arrayValue(value)
	identities := make([]domain.Identity, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		identity := identityFromMap(mapValue(item))
		if !hasIdentity(identity) && item != nil {
			identity = domain.Identity{Provider: domain.ProviderGitLab, ProviderID: stringValue(item)}
		}
		if !hasIdentity(identity) {
			continue
		}
		key := identityKey(identity)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		identities = append(identities, identity)
	}
	return identities
}

func identityFromMap(data map[string]any) domain.Identity {
	email := usableEmail(stringValue(data["email"]))
	return domain.Identity{
		Provider:   domain.ProviderGitLab,
		ProviderID: firstNonEmpty(stringValue(data["id"]), stringValue(data["user_id"])),
		Username:   strings.ToLower(firstNonEmpty(stringValue(data["username"]), stringValue(data["user_username"]))),
		Email:      email,
		Name:       firstNonEmpty(stringValue(data["name"]), stringValue(data["user_name"])),
	}
}

func usableEmail(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "[redacted]" || strings.Contains(value, "noreply") {
		return ""
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || !strings.Contains(value, "@") {
		return ""
	}
	return value
}

func hasIdentity(identity domain.Identity) bool {
	return identity.ProviderID != "" || identity.Username != "" || identity.Email != "" || identity.Name != ""
}

func identityPointer(identity domain.Identity) *domain.Identity {
	if !hasIdentity(identity) {
		return nil
	}
	return &identity
}

func identityKey(identity domain.Identity) string {
	if identity.ProviderID != "" {
		return "id:" + identity.ProviderID
	}
	if identity.Username != "" {
		return "username:" + strings.ToLower(identity.Username)
	}
	return "email:" + strings.ToLower(identity.Email)
}

func fallbackURL(projectURL, resource string, iid string) string {
	if projectURL == "" || iid == "" {
		return ""
	}
	return strings.TrimRight(projectURL, "/") + "/-/" + resource + "/" + iid
}

func mapValue(value any, key ...string) map[string]any {
	if len(key) == 0 {
		if data, ok := value.(map[string]any); ok {
			return data
		}
		return nil
	}
	data, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, part := range key {
		value, ok := data[part]
		if !ok {
			return nil
		}
		data, ok = value.(map[string]any)
		if !ok {
			return nil
		}
	}
	return data
}

func arrayValue(value any) []any {
	items, _ := value.([]any)
	return items
}

func boolValue(value any) bool {
	parsed, ok := value.(bool)
	if ok {
		return parsed
	}
	return strings.EqualFold(stringValue(value), "true")
}

func scalarString(value any) string {
	if value == nil {
		return ""
	}
	if data, ok := value.(map[string]any); ok {
		encoded, err := json.Marshal(data)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
	if _, ok := value.([]any); ok {
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
	return stringValue(value)
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%g", typed)
	case bool:
		return fmt.Sprintf("%t", typed)
	default:
		return fmt.Sprint(typed)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
