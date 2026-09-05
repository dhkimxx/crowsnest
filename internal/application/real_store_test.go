package application

import (
	"context"
	"net/http"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/adapter/database/sqlite"
	"github.com/dhkimxx/crowsnest/internal/adapter/git/gitlab/v17_6"
)

func TestIngestUsesSQLiteTransactionalOutbox(t *testing.T) {
	store, err := sqlite.Open(t.TempDir()+"/crowsnest.sqlite3", sqlite.DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	service := NewIngestService(
		NewDecoderRegistry(gitlabv176.NewDecoder()),
		NewRouter(store, store, store, []string{"example.com"}),
		store,
		store,
		nil,
	)
	body := []byte(`{
      "object_kind":"pipeline",
      "user":{"id":7,"username":"runner"},
      "project":{"id":76,"path_with_namespace":"group/project"},
      "object_attributes":{"id":31,"status":"failed","ref":"main"},
      "commit":{"message":"fix","author":{"id":42,"email":"user@example.com"}}
    }`)
	result, err := service.Handle(context.Background(), http.Header{"X-Gitlab-Event": []string{"Pipeline Hook"}, "Idempotency-Key": []string{"pipeline-transaction"}}, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.DeliveryCount != 1 {
		t.Fatalf("result = %#v", result)
	}
	claimed, err := store.Claim(context.Background(), "test-worker", 1)
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	if len(claimed) != 1 || claimed[0].Notification.Recipient.Value != "user@example.com" {
		t.Fatalf("claimed = %#v", claimed)
	}
	state, err := store.Get(context.Background(), "gitlab:76:ref:main")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state == nil || state.Status != "failed" || len(state.Recipients) != 1 {
		t.Fatalf("state = %#v", state)
	}
}
