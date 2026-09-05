package gitlabv176

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

func TestDecoderPipelineProjectHook(t *testing.T) {
	payload := map[string]any{
		"object_kind": "pipeline",
		"user": map[string]any{
			"id": 7, "username": "pipeline-user", "name": "Pipeline User", "email": "pipeline@example.com",
		},
		"project": map[string]any{
			"id": 76, "path_with_namespace": "group/project", "web_url": "https://gitlab.example/group/project",
		},
		"object_attributes": map[string]any{
			"id": 31, "iid": 3, "name": "Pipeline for branch: main", "ref": "main", "sha": "abc", "source": "push", "status": "failed", "url": "https://gitlab.example/group/project/-/pipelines/31", "created_at": "2026-09-04T00:00:00Z", "finished_at": "2026-09-04T00:01:00Z",
		},
		"commit": map[string]any{
			"message": "fix pipeline", "author": map[string]any{"name": "Carol", "email": "carol@example.com"},
		},
		"builds": []any{
			map[string]any{"id": 1, "name": "test", "status": "failed", "allow_failure": false},
			map[string]any{"id": 2, "name": "optional", "status": "failed", "allow_failure": true},
		},
	}
	body := marshalPayload(t, payload)
	decoder := NewDecoder()
	got, err := decoder.Decode(context.Background(), http.Header{"X-Gitlab-Event": []string{"Pipeline Hook"}, "Idempotency-Key": []string{"pipeline-1"}}, body)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Kind != domain.EventKindPipeline || got.Action != "failed" {
		t.Fatalf("unexpected event identity: kind=%q action=%q", got.Kind, got.Action)
	}
	if got.EventKey != "gitlab:header:pipeline-1" {
		t.Fatalf("EventKey = %q", got.EventKey)
	}
	if got.Author == nil || got.Author.Email != "carol@example.com" {
		t.Fatalf("Author = %#v", got.Author)
	}
	if got.Pipeline == nil || len(got.Pipeline.FailedJobs) != 1 || got.Pipeline.FailedJobs[0].Name != "test" {
		t.Fatalf("Pipeline = %#v", got.Pipeline)
	}
	if got.OccurredAt == nil || !got.OccurredAt.Equal(time.Date(2026, 9, 4, 0, 1, 0, 0, time.UTC)) {
		t.Fatalf("OccurredAt = %v", got.OccurredAt)
	}
}

func TestDecoderSystemHookMergeRequest(t *testing.T) {
	payload := map[string]any{
		"object_kind": "merge_request",
		"event_type":  "merge_request",
		"user":        map[string]any{"id": 7, "username": "reviewer", "email": "reviewer@example.com"},
		"project":     map[string]any{"id": 76, "path_with_namespace": "group/project", "web_url": "https://gitlab.example/group/project"},
		"object_attributes": map[string]any{
			"id": 9001, "iid": 12, "action": "update", "state": "opened", "title": "Improve API", "description": "Please review @carol and ignore `@code`.", "url": "https://gitlab.example/group/project/-/merge_requests/12", "author_id": 42,
		},
		"reviewers": []any{
			map[string]any{"id": 99, "username": "carol", "name": "Carol"},
		},
		"changes": map[string]any{
			"reviewers": map[string]any{
				"previous": []any{},
				"current":  []any{map[string]any{"id": 99, "username": "carol", "name": "Carol"}},
			},
			"title": map[string]any{"previous": "Old API", "current": "Improve API"},
		},
	}
	got, err := NewDecoder().Decode(context.Background(), http.Header{"X-Gitlab-Event": []string{"System Hook"}}, marshalPayload(t, payload))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Kind != domain.EventKindMergeRequest || got.MergeRequest == nil {
		t.Fatalf("MergeRequest = %#v", got.MergeRequest)
	}
	if got.MergeRequest.Author.ProviderID != "42" || got.MergeRequest.Title != "Improve API" {
		t.Fatalf("MergeRequest = %#v", got.MergeRequest)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].Username != "carol" {
		t.Fatalf("Mentions = %#v", got.Mentions)
	}
	if len(got.Changes) != 2 || len(got.Changes[0].Added) != 1 {
		t.Fatalf("Changes = %#v", got.Changes)
	}
}

func TestDecoderNoteMentionsAndIssueTarget(t *testing.T) {
	payload := map[string]any{
		"object_kind": "note",
		"event_type":  "note",
		"user":        map[string]any{"id": 7, "username": "bob", "email": "bob@example.com"},
		"project":     map[string]any{"id": 76, "path_with_namespace": "group/project", "web_url": "https://gitlab.example/group/project"},
		"object_attributes": map[string]any{
			"id": 123, "action": "create", "note": "@carol please check. carol@example.com and `@ignored`.", "noteable_type": "Issue", "url": "https://gitlab.example/group/project/-/issues/4#note_123",
		},
		"issue": map[string]any{
			"id": 400, "iid": 4, "title": "Build issue", "description": "Issue description", "state": "opened", "author_id": 42, "url": "https://gitlab.example/group/project/-/issues/4",
		},
	}
	got, err := NewDecoder().Decode(context.Background(), http.Header{"X-Gitlab-Event": []string{"Note Hook"}}, marshalPayload(t, payload))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Kind != domain.EventKindNote || got.Note == nil || got.Note.Issue == nil {
		t.Fatalf("Note = %#v", got.Note)
	}
	if len(got.Mentions) != 1 || got.Mentions[0].Username != "carol" {
		t.Fatalf("Mentions = %#v", got.Mentions)
	}
	if got.Object.Kind != domain.EventKindIssue || got.Object.IID != "4" {
		t.Fatalf("Object = %#v", got.Object)
	}
}

func TestDecoderIgnoresUnsupportedSystemEvent(t *testing.T) {
	payload := map[string]any{"event_name": "project_create", "project_id": 1}
	_, err := NewDecoder().Decode(context.Background(), http.Header{"X-Gitlab-Event": []string{"System Hook"}}, marshalPayload(t, payload))
	if !errors.Is(err, ports.ErrIgnoredEvent) {
		t.Fatalf("error = %v, want ErrIgnoredEvent", err)
	}
}

func TestDecoderReadsVersionedFixtures(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test source")
	}
	fixtureRoot := filepath.Join(filepath.Dir(sourceFile), "../../../../../testdata/gitlab/v17_6")
	tests := []struct {
		file       string
		header     string
		kind       domain.EventKind
		action     string
		shouldWork bool
	}{
		{"pipeline_failed.json", "Pipeline Hook", domain.EventKindPipeline, "failed", true},
		{"merge_request_open.json", "Merge Request Hook", domain.EventKindMergeRequest, "open", true},
		{"merge_request_update_reviewer.json", "System Hook", domain.EventKindMergeRequest, "update", true},
		{"note_merge_request.json", "Note Hook", domain.EventKindNote, "create", true},
		{"issue_update_assignee.json", "Issue Hook", domain.EventKindIssue, "update", true},
		{"system_hook_project_create.json", "System Hook", "", "", false},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(fixtureRoot, test.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			event, decodeErr := NewDecoder().Decode(context.Background(), http.Header{"X-Gitlab-Event": []string{test.header}}, body)
			if !test.shouldWork {
				if !errors.Is(decodeErr, ports.ErrIgnoredEvent) {
					t.Fatalf("error = %v, want ErrIgnoredEvent", decodeErr)
				}
				return
			}
			if decodeErr != nil {
				t.Fatalf("Decode() error = %v", decodeErr)
			}
			if event.Kind != test.kind || event.Action != test.action {
				t.Fatalf("event = %#v", event)
			}
		})
	}
}

func marshalPayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.TrimSpace(string(body)) == "" {
		t.Fatal("empty payload")
	}
	return body
}
