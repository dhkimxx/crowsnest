package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/adapter/git/gitlab/v17_6"
	"github.com/dhkimxx/crowsnest/internal/application"
	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestServerAcceptsAuthenticatedWebhook(t *testing.T) {
	store := &eventStore{}
	service := application.NewIngestService(
		application.NewDecoderRegistry(gitlabv176.NewDecoder()),
		application.NewRouter(nil, nil, nil, []string{"example.com"}),
		store,
		nil,
		nil,
	)
	handler := New(service, "webhook-secret", nil).Handler()
	request := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", webhookBody())
	request.Header.Set("X-Gitlab-Token", "webhook-secret")
	request.Header.Set("X-Gitlab-Event", "Pipeline Hook")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var result application.IngestResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Status != "accepted" || len(store.deliveries) != 1 {
		t.Fatalf("result=%#v deliveries=%#v", result, store.deliveries)
	}
}

func TestServerRejectsInvalidWebhookToken(t *testing.T) {
	service := application.NewIngestService(nil, nil, nil, nil, nil)
	handler := New(service, "webhook-secret", nil).Handler()
	request := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", webhookBody())
	request.Header.Set("X-Gitlab-Token", "wrong")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestServerRejectsWebhookWhenSecretIsNotConfigured(t *testing.T) {
	service := application.NewIngestService(nil, nil, nil, nil, nil)
	handler := New(service, "", nil).Handler()
	request := httptest.NewRequest(http.MethodPost, "/webhook/gitlab", webhookBody())
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", response.Code)
	}
}

type eventStore struct {
	deliveries []domain.Delivery
}

func (s *eventStore) Enqueue(_ context.Context, _ domain.CanonicalEvent, deliveries []domain.Delivery) error {
	s.deliveries = deliveries
	return nil
}

func webhookBody() *strings.Reader {
	return strings.NewReader(`{
      "object_kind":"pipeline",
      "user":{"id":7,"username":"runner"},
      "project":{"id":76,"path_with_namespace":"group/project"},
      "object_attributes":{"id":31,"status":"failed","ref":"main"},
      "commit":{"message":"fix","author":{"email":"carol@example.com"}}
    }`)
}
