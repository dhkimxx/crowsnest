package application

import (
	"context"
	"net/http"
	"testing"

	"github.com/dhkimxx/crowsnest/internal/adapter/git/gitlab/v17_6"
	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestIngestServicePersistsEventAndDeliveries(t *testing.T) {
	store := &fakeEventStore{}
	stateStore := &fakePipelineStateStore{}
	router := NewRouter(nil, nil, stateStore, []string{"example.com"})
	service := NewIngestService(NewDecoderRegistry(gitlabv176.NewDecoder()), router, store, stateStore, nil)

	body := []byte(`{
      "object_kind":"pipeline",
      "user":{"id":7,"username":"runner"},
      "project":{"id":76,"path_with_namespace":"group/project","web_url":"https://gitlab.example/group/project"},
      "object_attributes":{"id":31,"status":"failed","ref":"main","url":"https://gitlab.example/group/project/-/pipelines/31"},
      "commit":{"message":"fix","author":{"name":"Carol","email":"carol@example.com"}}
    }`)
	result, err := service.Handle(context.Background(), http.Header{"X-Gitlab-Event": []string{"Pipeline Hook"}}, body)
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != "accepted" || result.DeliveryCount != 1 || store.event.EventKey == "" || len(store.deliveries) != 1 {
		t.Fatalf("result=%#v event=%#v deliveries=%#v", result, store.event, store.deliveries)
	}
	if stateStore.state == nil || stateStore.state.Status != "failed" {
		t.Fatalf("pipeline state = %#v", stateStore.state)
	}
}

func TestIngestServiceIgnoresUnsupportedSystemEvents(t *testing.T) {
	store := &fakeEventStore{}
	service := NewIngestService(
		NewDecoderRegistry(gitlabv176.NewDecoder()),
		NewRouter(nil, nil, nil, nil),
		store,
		nil,
		nil,
	)
	result, err := service.Handle(context.Background(), http.Header{"X-Gitlab-Event": []string{"System Hook"}}, []byte(`{"event_name":"project_create","project_id":1}`))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if result.Status != "ignored" || store.event.EventKey != "" {
		t.Fatalf("result=%#v store=%#v", result, store)
	}
}

type fakeEventStore struct {
	event      domain.CanonicalEvent
	deliveries []domain.Delivery
}

func (s *fakeEventStore) Enqueue(_ context.Context, event domain.CanonicalEvent, deliveries []domain.Delivery) error {
	s.event = event
	s.deliveries = deliveries
	return nil
}
