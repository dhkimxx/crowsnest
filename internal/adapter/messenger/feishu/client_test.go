package feishu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
)

func TestClientSendsInteractiveEmailMessage(t *testing.T) {
	var tokenCalls, messageCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case tokenPath:
			tokenCalls++
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/im/v1/messages":
			messageCalls++
			if request.URL.Query().Get("receive_id_type") != "email" {
				t.Fatalf("receive_id_type = %q", request.URL.Query().Get("receive_id_type"))
			}
			if request.Header.Get("Authorization") != "Bearer tenant-token" {
				t.Fatalf("authorization header was not set")
			}
			var body struct {
				ReceiveID string `json:"receive_id"`
				MsgType   string `json:"msg_type"`
				Content   string `json:"content"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode message body: %v", err)
			}
			if body.ReceiveID != "carol@example.com" || body.MsgType != "interactive" {
				t.Fatalf("message body = %#v", body)
			}
			var card map[string]any
			if err := json.Unmarshal([]byte(body.Content), &card); err != nil {
				t.Fatalf("decode card: %v", err)
			}
			if card["header"] == nil || card["elements"] == nil {
				t.Fatalf("card = %#v", card)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{"message_id": "om_test"}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AppID: "app-id", AppSecret: "app-secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	receipt, err := client.Send(context.Background(), domain.Notification{
		EventKey:  "event-1",
		Kind:      domain.EventKindPipeline,
		Action:    "failed",
		Title:     "Pipeline 실패",
		Summary:   "group/project · failed",
		URL:       "https://gitlab.example/group/project/-/pipelines/1",
		Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "carol@example.com"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if receipt.ProviderMessageID != "om_test" || tokenCalls != 1 || messageCalls != 1 {
		t.Fatalf("receipt=%#v tokenCalls=%d messageCalls=%d", receipt, tokenCalls, messageCalls)
	}
}

func TestClientRefreshesTokenOnceAfterAuthenticationError(t *testing.T) {
	tokenCalls := 0
	messageCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == tokenPath {
			tokenCalls++
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "tenant_access_token": "token-" + string(rune('0'+tokenCalls)), "expire": 7200})
			return
		}
		messageCalls++
		if messageCalls == 1 {
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 99991663, "msg": "token expired"})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{"message_id": "om_retry"}})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AppID: "app-id", AppSecret: "app-secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Send(context.Background(), domain.Notification{
		Kind:      domain.EventKindNote,
		Action:    "create",
		Title:     "댓글",
		Summary:   "comment",
		Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "user@example.com"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if tokenCalls != 2 || messageCalls != 2 {
		t.Fatalf("tokenCalls=%d messageCalls=%d", tokenCalls, messageCalls)
	}
}

func TestClientClassifiesRateLimitAsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == tokenPath {
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
			return
		}
		writer.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 999, "msg": "rate limit"})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AppID: "app-id", AppSecret: "app-secret", TokenMargin: time.Second}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Send(context.Background(), domain.Notification{
		Kind:      domain.EventKindIssue,
		Action:    "open",
		Title:     "Issue",
		Summary:   "issue",
		Recipient: domain.RecipientAddress{Kind: domain.AddressKindEmail, Value: "user@example.com"},
	})
	if err == nil {
		t.Fatal("Send() error = nil")
	}
	feishuErr, ok := err.(*Error)
	if !ok || !feishuErr.Retryable || feishuErr.Class != "rate_limit" {
		t.Fatalf("error = %#v", err)
	}
}

func TestClientLooksUpEmailUsers(t *testing.T) {
	var lookupCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case tokenPath:
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
		case "/open-apis/contact/v3/users/batch_get_id":
			lookupCalls++
			if request.URL.Query().Get("user_id_type") != "open_id" {
				t.Fatalf("user_id_type = %q", request.URL.Query().Get("user_id_type"))
			}
			if request.Header.Get("Authorization") != "Bearer tenant-token" {
				t.Fatalf("authorization header was not set")
			}
			var body struct {
				Emails []string `json:"emails"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatalf("decode lookup body: %v", err)
			}
			if len(body.Emails) != 2 || body.Emails[0] != "alice@example.com" || body.Emails[1] != "bob@example.com" {
				t.Fatalf("lookup body = %#v", body)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{"user_list": []map[string]string{
					{"user_id": "ou_alice", "email": "alice@example.com"},
				}},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AppID: "app-id", AppSecret: "app-secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	users, err := client.LookupEmails(context.Background(), []string{
		"Alice@example.com", "alice@example.com", "bob@example.com",
	})
	if err != nil {
		t.Fatalf("LookupEmails() error = %v", err)
	}
	if lookupCalls != 1 || len(users) != 1 || users["alice@example.com"].ID != "ou_alice" {
		t.Fatalf("lookupCalls=%d users=%#v", lookupCalls, users)
	}
}

func TestClientClassifiesFeishuContactPermissionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == tokenPath {
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "tenant_access_token": "tenant-token", "expire": 7200})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 99991672, "msg": "permission denied"})
	}))
	defer server.Close()

	client, err := NewClient(Config{BaseURL: server.URL, AppID: "app-id", AppSecret: "app-secret"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.LookupEmails(context.Background(), []string{"alice@example.com"})
	feishuErr, ok := err.(*Error)
	if !ok || feishuErr.Class != "permission" || feishuErr.Retryable {
		t.Fatalf("error = %#v", err)
	}
}
