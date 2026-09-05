package gitlabv176

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

const (
	defaultGitLabHTTPTimeout = 15 * time.Second
	maxGitLabResponseBytes   = 2 << 20
)

type HookConfig struct {
	BaseURL               string
	APIToken              string
	WebhookURL            string
	WebhookToken          string
	HookName              string
	EnableSSLVerification bool
	HTTPClient            *http.Client
}

type HookController struct {
	config HookConfig
	client *http.Client
}

type apiError struct {
	StatusCode int
	Message    string
	RetryAfter time.Duration
}

func (e *apiError) Error() string {
	return fmt.Sprintf("GitLab API request failed (status=%d): %s", e.StatusCode, e.Message)
}

func NewHookController(config HookConfig) (*HookController, error) {
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, errors.New("GitLab API base URL is required")
	}
	if strings.TrimSpace(config.APIToken) == "" {
		return nil, errors.New("GitLab API token is required")
	}
	if strings.TrimSpace(config.WebhookURL) == "" {
		return nil, errors.New("GitLab webhook URL is required")
	}
	if strings.TrimSpace(config.WebhookToken) == "" {
		return nil, errors.New("GitLab webhook token is required")
	}
	if config.HookName == "" {
		config.HookName = "Crowsnest"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: defaultGitLabHTTPTimeout}
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	return &HookController{config: config, client: config.HTTPClient}, nil
}

func (c *HookController) Reconcile(ctx context.Context, options ports.HookReconcileOptions) (domain.HookReconcileReport, error) {
	report := domain.HookReconcileReport{DryRun: options.DryRun}
	systemAction, err := c.ensureSystemHook(ctx, options.DryRun)
	if err != nil {
		return report, err
	}
	report.SystemHookAction = systemAction

	projects, err := c.listProjects(ctx)
	if err != nil {
		return report, err
	}
	var reconcileErrors []error
	for _, project := range projects {
		report.ProjectsScanned++
		action, reconcileErr := c.ensureProjectHook(ctx, project.ID, options.DryRun)
		if reconcileErr != nil {
			report.HooksFailed++
			reconcileErrors = append(reconcileErrors, fmt.Errorf("project %d: %w", project.ID, reconcileErr))
			continue
		}
		switch action {
		case "create", "would_create":
			report.HooksCreated++
		case "update", "would_update":
			report.HooksUpdated++
		case "unchanged":
			report.HooksUnchanged++
		}
	}
	return report, errors.Join(reconcileErrors...)
}

type projectSummary struct {
	ID int `json:"id"`
}

func (c *HookController) listProjects(ctx context.Context) ([]projectSummary, error) {
	var projects []projectSummary
	for page := 1; ; page++ {
		var pageProjects []projectSummary
		nextPage, err := c.requestJSON(ctx, http.MethodGet, "/projects?archived=false&per_page=100&page="+strconv.Itoa(page)+"&simple=true", nil, &pageProjects)
		if err != nil {
			return nil, err
		}
		projects = append(projects, pageProjects...)
		if nextPage == "" || len(pageProjects) == 0 {
			break
		}
	}
	return projects, nil
}

type systemHook struct {
	ID                     int    `json:"id"`
	Name                   string `json:"name"`
	URL                    string `json:"url"`
	PushEvents             bool   `json:"push_events"`
	TagPushEvents          bool   `json:"tag_push_events"`
	MergeRequestsEvents    bool   `json:"merge_requests_events"`
	RepositoryUpdateEvents bool   `json:"repository_update_events"`
	EnableSSLVerification  bool   `json:"enable_ssl_verification"`
}

func (c *HookController) ensureSystemHook(ctx context.Context, dryRun bool) (string, error) {
	var hooks []systemHook
	if _, err := c.requestJSON(ctx, http.MethodGet, "/hooks", nil, &hooks); err != nil {
		return "", err
	}
	for _, hook := range hooks {
		if hook.Name != c.config.HookName {
			continue
		}
		if systemHookMatches(hook, c.config) {
			return "unchanged", nil
		}
		if dryRun {
			return "would_update", nil
		}
		_, err := c.requestJSON(ctx, http.MethodPut, "/hooks/"+strconv.Itoa(hook.ID), systemHookRequest(c.config), nil)
		if err != nil {
			return "", err
		}
		return "update", nil
	}
	if dryRun {
		return "would_create", nil
	}
	_, err := c.requestJSON(ctx, http.MethodPost, "/hooks", systemHookRequest(c.config), nil)
	if err != nil {
		return "", err
	}
	return "create", nil
}

func systemHookRequest(config HookConfig) map[string]any {
	return map[string]any{
		"name":                     config.HookName,
		"description":              "Managed by Crowsnest",
		"url":                      config.WebhookURL,
		"token":                    config.WebhookToken,
		"push_events":              false,
		"tag_push_events":          false,
		"merge_requests_events":    true,
		"repository_update_events": false,
		"enable_ssl_verification":  config.EnableSSLVerification,
	}
}

func systemHookMatches(hook systemHook, config HookConfig) bool {
	return hook.URL == config.WebhookURL &&
		hook.PushEvents == false &&
		hook.TagPushEvents == false &&
		hook.MergeRequestsEvents &&
		hook.RepositoryUpdateEvents == false &&
		hook.EnableSSLVerification == config.EnableSSLVerification
}

type projectHook struct {
	ID                       int    `json:"id"`
	Name                     string `json:"name"`
	URL                      string `json:"url"`
	PushEvents               bool   `json:"push_events"`
	TagPushEvents            bool   `json:"tag_push_events"`
	MergeRequestsEvents      bool   `json:"merge_requests_events"`
	IssuesEvents             bool   `json:"issues_events"`
	ConfidentialIssuesEvents bool   `json:"confidential_issues_events"`
	NoteEvents               bool   `json:"note_events"`
	ConfidentialNoteEvents   bool   `json:"confidential_note_events"`
	PipelineEvents           bool   `json:"pipeline_events"`
	EnableSSLVerification    bool   `json:"enable_ssl_verification"`
}

func (c *HookController) ensureProjectHook(ctx context.Context, projectID int, dryRun bool) (string, error) {
	path := "/projects/" + strconv.Itoa(projectID) + "/hooks"
	var hooks []projectHook
	if _, err := c.requestJSON(ctx, http.MethodGet, path, nil, &hooks); err != nil {
		return "", err
	}
	for _, hook := range hooks {
		if hook.Name != c.config.HookName {
			continue
		}
		if projectHookMatches(hook, c.config) {
			return "unchanged", nil
		}
		if dryRun {
			return "would_update", nil
		}
		_, err := c.requestJSON(ctx, http.MethodPut, path+"/"+strconv.Itoa(hook.ID), projectHookRequest(c.config), nil)
		if err != nil {
			return "", err
		}
		return "update", nil
	}
	if dryRun {
		return "would_create", nil
	}
	_, err := c.requestJSON(ctx, http.MethodPost, path, projectHookRequest(c.config), nil)
	if err != nil {
		return "", err
	}
	return "create", nil
}

func projectHookRequest(config HookConfig) map[string]any {
	return map[string]any{
		"name":                       config.HookName,
		"url":                        config.WebhookURL,
		"token":                      config.WebhookToken,
		"push_events":                false,
		"tag_push_events":            false,
		"merge_requests_events":      false,
		"issues_events":              true,
		"note_events":                true,
		"pipeline_events":            true,
		"confidential_issues_events": false,
		"confidential_note_events":   false,
		"enable_ssl_verification":    config.EnableSSLVerification,
	}
}

func projectHookMatches(hook projectHook, config HookConfig) bool {
	return hook.URL == config.WebhookURL &&
		!hook.PushEvents &&
		!hook.TagPushEvents &&
		!hook.MergeRequestsEvents &&
		hook.IssuesEvents &&
		!hook.ConfidentialIssuesEvents &&
		hook.NoteEvents &&
		!hook.ConfidentialNoteEvents &&
		hook.PipelineEvents &&
		hook.EnableSSLVerification == config.EnableSSLVerification
}

func (c *HookController) requestJSON(ctx context.Context, method, path string, payload any, output any) (string, error) {
	maxAttempts := 1
	if method == http.MethodGet || method == http.MethodPut {
		maxAttempts = 3
	}
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		nextPage, err := c.requestJSONOnce(ctx, method, path, payload, output)
		if err == nil {
			return nextPage, nil
		}
		lastErr = err
		if attempt+1 >= maxAttempts {
			break
		}
		var responseErr *apiError
		if errors.As(err, &responseErr) && responseErr.StatusCode != http.StatusTooManyRequests && responseErr.StatusCode < 500 {
			break
		}
		wait := time.Duration(attempt+1) * 250 * time.Millisecond
		if responseErr != nil && responseErr.RetryAfter > 0 {
			wait = responseErr.RetryAfter
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", lastErr
}

func (c *HookController) requestJSONOnce(ctx context.Context, method, path string, payload any, output any) (string, error) {
	var requestReader io.Reader
	if payload != nil {
		requestBody, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("marshal GitLab API request: %w", err)
		}
		requestReader = bytes.NewReader(requestBody)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.apiURL(path), requestReader)
	if err != nil {
		return "", fmt.Errorf("create GitLab API request: %w", err)
	}
	request.Header.Set("PRIVATE-TOKEN", c.config.APIToken)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("GitLab API transport: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxGitLabResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read GitLab API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", &apiError{StatusCode: response.StatusCode, Message: responseMessage(responseBody), RetryAfter: retryAfter(response.Header.Get("Retry-After"))}
	}
	if output != nil && len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, output); err != nil {
			return "", fmt.Errorf("decode GitLab API response: %w", err)
		}
	}
	return response.Header.Get("X-Next-Page"), nil
}

func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (c *HookController) apiURL(path string) string {
	base := c.config.BaseURL
	if strings.HasSuffix(base, "/api/v4") {
		return base + path
	}
	return base + "/api/v4" + path
}

func responseMessage(body []byte) string {
	var response struct {
		Message any    `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &response) == nil {
		if response.Message != nil {
			return truncateAPIMessage(fmt.Sprint(response.Message))
		}
		if response.Error != "" {
			return truncateAPIMessage(response.Error)
		}
	}
	return "unavailable"
}

func truncateAPIMessage(value string) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= 500 {
		return string(runes)
	}
	return string(runes[:500]) + "…"
}
