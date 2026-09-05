package gitlabv176

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/ports"
)

func TestHookControllerDryRunDoesNotWrite(t *testing.T) {
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost || request.Method == http.MethodPut {
			writes++
		}
		switch request.URL.Path {
		case "/api/v4/hooks":
			_ = json.NewEncoder(writer).Encode([]systemHook{})
		case "/api/v4/projects":
			_ = json.NewEncoder(writer).Encode([]projectSummary{{ID: 1}})
		case "/api/v4/projects/1/hooks":
			_ = json.NewEncoder(writer).Encode([]projectHook{})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	controller, err := NewHookController(HookConfig{
		BaseURL:               server.URL,
		APIToken:              "gitlab-api-token",
		WebhookURL:            "http://crowsnest.example.test:5680/webhook/gitlab",
		WebhookToken:          "webhook-token",
		HookName:              "Crowsnest",
		EnableSSLVerification: false,
		HTTPClient:            server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHookController() error = %v", err)
	}
	report, err := controller.Reconcile(context.Background(), ports.HookReconcileOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !report.DryRun || report.SystemHookAction != "would_create" || report.ProjectsScanned != 1 || report.HooksCreated != 1 || writes != 0 {
		t.Fatalf("report=%#v writes=%d", report, writes)
	}
}

func TestHookControllerCreatesOnlyManagedHooks(t *testing.T) {
	var writes []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost || request.Method == http.MethodPut {
			writes = append(writes, request.Method+" "+request.URL.Path)
		}
		switch request.URL.Path {
		case "/api/v4/hooks":
			if request.Method == http.MethodGet {
				_ = json.NewEncoder(writer).Encode([]systemHook{})
				return
			}
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(systemHook{ID: 1})
		case "/api/v4/projects":
			_ = json.NewEncoder(writer).Encode([]projectSummary{{ID: 1}})
		case "/api/v4/projects/1/hooks":
			if request.Method == http.MethodGet {
				_ = json.NewEncoder(writer).Encode([]projectHook{})
				return
			}
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(projectHook{ID: 1})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	controller, err := NewHookController(HookConfig{
		BaseURL:               server.URL,
		APIToken:              "gitlab-api-token",
		WebhookURL:            "http://crowsnest.example.test:5680/webhook/gitlab",
		WebhookToken:          "webhook-token",
		HookName:              "Crowsnest",
		EnableSSLVerification: false,
		HTTPClient:            server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHookController() error = %v", err)
	}
	report, err := controller.Reconcile(context.Background(), ports.HookReconcileOptions{})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if report.HooksCreated != 1 || report.SystemHookAction != "create" || !containsAll(writes, "POST /api/v4/hooks", "POST /api/v4/projects/1/hooks") {
		t.Fatalf("report=%#v writes=%v", report, writes)
	}
}

func TestHookControllerDoesNotTouchForeignHook(t *testing.T) {
	var writes int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut || request.Method == http.MethodPost {
			writes++
		}
		switch request.URL.Path {
		case "/api/v4/hooks":
			_ = json.NewEncoder(writer).Encode([]systemHook{{ID: 9, Name: "Other", URL: "http://other"}})
		case "/api/v4/projects":
			_ = json.NewEncoder(writer).Encode([]projectSummary{{ID: 1}})
		case "/api/v4/projects/1/hooks":
			_ = json.NewEncoder(writer).Encode([]projectHook{{ID: 10, Name: "Other", URL: "http://other"}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	controller, err := NewHookController(HookConfig{
		BaseURL:               server.URL,
		APIToken:              "gitlab-api-token",
		WebhookURL:            "http://crowsnest.example.test:5680/webhook/gitlab",
		WebhookToken:          "webhook-token",
		HookName:              "Crowsnest",
		EnableSSLVerification: false,
		HTTPClient:            server.Client(),
	})
	if err != nil {
		t.Fatalf("NewHookController() error = %v", err)
	}
	if _, err := controller.Reconcile(context.Background(), ports.HookReconcileOptions{}); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if writes != 2 {
		t.Fatalf("writes=%d, want create-only writes for owned hooks", writes)
	}
}

func containsAll(values []string, expected ...string) bool {
	joined := strings.Join(values, "\n")
	for _, value := range expected {
		if !strings.Contains(joined, value) {
			return false
		}
	}
	return true
}
