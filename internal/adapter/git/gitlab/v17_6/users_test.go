package gitlabv176

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/ports"
)

func TestListUsersPaginatesAndFallsBackToAdminEmails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("PRIVATE-TOKEN") != "gitlab-token" {
			t.Fatalf("PRIVATE-TOKEN = %q", request.Header.Get("PRIVATE-TOKEN"))
		}
		switch request.URL.Path {
		case "/api/v4/users":
			if request.URL.Query().Get("per_page") != "100" || request.URL.Query().Get("order_by") != "id" || request.URL.Query().Get("sort") != "asc" {
				t.Fatalf("users query = %v", request.URL.Query())
			}
			if request.URL.Query().Get("page") == "1" {
				writer.Header().Set("X-Next-Page", "2")
				_ = json.NewEncoder(writer).Encode([]map[string]any{
					{"id": 1, "username": "alice", "name": "Alice", "email": "alice@example.com", "state": "active"},
					{"id": 2, "username": "bob", "name": "Bob", "email": "[REDACTED]", "state": "blocked"},
				})
				return
			}
			_ = json.NewEncoder(writer).Encode([]map[string]any{
				{"id": 3, "username": "external", "name": "External", "email": "external@example.com", "state": "active", "external": true},
			})
		case "/api/v4/users/2/emails":
			_ = json.NewEncoder(writer).Encode([]map[string]any{
				{"email": "bob@example.com", "confirmed_at": "2026-09-04T00:00:00Z"},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	controller, err := NewHookController(HookConfig{
		BaseURL:      server.URL,
		APIToken:     "gitlab-token",
		WebhookURL:   "http://crowsnest/webhook/gitlab",
		WebhookToken: "hook-token",
	})
	if err != nil {
		t.Fatalf("NewHookController() error = %v", err)
	}
	users, err := controller.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("users = %#v", users)
	}
	if users[0].Identity.Email != "alice@example.com" || !users[0].Active {
		t.Fatalf("alice = %#v", users[0])
	}
	if users[1].Identity.Email != "bob@example.com" || users[1].Active {
		t.Fatalf("bob = %#v", users[1])
	}
	if users[2].Active {
		t.Fatalf("external user should be inactive: %#v", users[2])
	}
	var _ ports.UserDirectory = controller
}
