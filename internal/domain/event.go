package domain

import "time"

type Provider string

const (
	ProviderGitLab  Provider = "gitlab"
	ProviderGitHub  Provider = "github"
	ProviderForgejo Provider = "forgejo"
)

type EventKind string

const (
	EventKindPipeline     EventKind = "pipeline"
	EventKindMergeRequest EventKind = "merge_request"
	EventKindNote         EventKind = "note"
	EventKindIssue        EventKind = "issue"
)

type Identity struct {
	Provider   Provider `json:"provider,omitempty"`
	ProviderID string   `json:"provider_id,omitempty"`
	Username   string   `json:"username,omitempty"`
	Email      string   `json:"email,omitempty"`
	Name       string   `json:"name,omitempty"`
}

type ProjectRef struct {
	ID            string `json:"id,omitempty"`
	Path          string `json:"path,omitempty"`
	URL           string `json:"url,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type ResourceRef struct {
	Kind  EventKind `json:"kind,omitempty"`
	ID    string    `json:"id,omitempty"`
	IID   string    `json:"iid,omitempty"`
	Title string    `json:"title,omitempty"`
	URL   string    `json:"url,omitempty"`
}

type Change struct {
	Field   string     `json:"field"`
	Before  string     `json:"before,omitempty"`
	After   string     `json:"after,omitempty"`
	Added   []Identity `json:"added,omitempty"`
	Removed []Identity `json:"removed,omitempty"`
}

type PipelineJob struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name,omitempty"`
	Status       string `json:"status,omitempty"`
	AllowFailure bool   `json:"allow_failure,omitempty"`
}

type PipelineDetails struct {
	ID           string        `json:"id,omitempty"`
	IID          string        `json:"iid,omitempty"`
	Name         string        `json:"name,omitempty"`
	Ref          string        `json:"ref,omitempty"`
	SHA          string        `json:"sha,omitempty"`
	Source       string        `json:"source,omitempty"`
	Status       string        `json:"status,omitempty"`
	URL          string        `json:"url,omitempty"`
	CommitAuthor Identity      `json:"commit_author,omitempty"`
	MergeRequest *ResourceRef  `json:"merge_request,omitempty"`
	FailedJobs   []PipelineJob `json:"failed_jobs,omitempty"`
}

type MergeRequestDetails struct {
	ID           string     `json:"id,omitempty"`
	IID          string     `json:"iid,omitempty"`
	Title        string     `json:"title,omitempty"`
	Description  string     `json:"description,omitempty"`
	URL          string     `json:"url,omitempty"`
	SourceBranch string     `json:"source_branch,omitempty"`
	TargetBranch string     `json:"target_branch,omitempty"`
	State        string     `json:"state,omitempty"`
	Author       Identity   `json:"author,omitempty"`
	Reviewers    []Identity `json:"reviewers,omitempty"`
	Assignees    []Identity `json:"assignees,omitempty"`
}

type IssueDetails struct {
	ID          string     `json:"id,omitempty"`
	IID         string     `json:"iid,omitempty"`
	Title       string     `json:"title,omitempty"`
	Description string     `json:"description,omitempty"`
	URL         string     `json:"url,omitempty"`
	State       string     `json:"state,omitempty"`
	Author      Identity   `json:"author,omitempty"`
	Assignees   []Identity `json:"assignees,omitempty"`
}

type NoteDetails struct {
	ID           string               `json:"id,omitempty"`
	Body         string               `json:"body,omitempty"`
	Action       string               `json:"action,omitempty"`
	NoteableType string               `json:"noteable_type,omitempty"`
	URL          string               `json:"url,omitempty"`
	System       bool                 `json:"system,omitempty"`
	MergeRequest *MergeRequestDetails `json:"merge_request,omitempty"`
	Issue        *IssueDetails        `json:"issue,omitempty"`
}

type CanonicalEvent struct {
	EventKey      string               `json:"event_key"`
	Source        Provider             `json:"source"`
	SourceVersion string               `json:"source_version"`
	SourceEvent   string               `json:"source_event"`
	Kind          EventKind            `json:"kind"`
	Action        string               `json:"action"`
	ReceivedAt    time.Time            `json:"received_at"`
	OccurredAt    *time.Time           `json:"occurred_at,omitempty"`
	Project       ProjectRef           `json:"project"`
	Actor         Identity             `json:"actor"`
	Author        *Identity            `json:"author,omitempty"`
	Reviewers     []Identity           `json:"reviewers,omitempty"`
	Assignees     []Identity           `json:"assignees,omitempty"`
	Mentions      []Identity           `json:"mentions,omitempty"`
	Object        ResourceRef          `json:"object"`
	Changes       []Change             `json:"changes,omitempty"`
	Pipeline      *PipelineDetails     `json:"pipeline,omitempty"`
	MergeRequest  *MergeRequestDetails `json:"merge_request,omitempty"`
	Note          *NoteDetails         `json:"note,omitempty"`
	Issue         *IssueDetails        `json:"issue,omitempty"`
	SourceText    string               `json:"source_text,omitempty"`
	RawStatus     string               `json:"raw_status,omitempty"`
	Metadata      map[string]string    `json:"metadata,omitempty"`
}

type PipelineState struct {
	CorrelationKey string             `json:"correlation_key"`
	Status         string             `json:"status"`
	Recipients     []RecipientAddress `json:"recipients"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

type HookReconcileReport struct {
	DryRun           bool   `json:"dry_run"`
	SystemHookAction string `json:"system_hook_action"`
	ProjectsScanned  int    `json:"projects_scanned"`
	HooksCreated     int    `json:"hooks_created"`
	HooksUpdated     int    `json:"hooks_updated"`
	HooksUnchanged   int    `json:"hooks_unchanged"`
	HooksFailed      int    `json:"hooks_failed"`
}
