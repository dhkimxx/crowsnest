package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dhkimxx/crowsnest/internal/domain"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

const (
	defaultBaseURL     = "https://open.feishu.cn"
	tokenPath          = "/open-apis/auth/v3/tenant_access_token/internal"
	messagePath        = "/open-apis/im/v1/messages?receive_id_type=email"
	userLookupPath     = "/open-apis/contact/v3/users/batch_get_id?user_id_type=open_id"
	defaultTimeout     = 10 * time.Second
	defaultTokenMargin = time.Minute
	userLookupBatch    = 50
)

type Config struct {
	BaseURL     string
	AppID       string
	AppSecret   string
	Timeout     time.Duration
	TokenMargin time.Duration
}

type Client struct {
	config       Config
	httpClient   *http.Client
	now          func() time.Time
	mu           sync.Mutex
	tokenMu      sync.Mutex
	token        string
	tokenExpires time.Time
}

type Error struct {
	HTTPStatus int
	Code       int
	Class      string
	Retryable  bool
	Message    string
}

func (e *Error) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("Feishu request failed (class=%s, http_status=%d, code=%d): %s", e.Class, e.HTTPStatus, e.Code, e.Message)
	}
	return fmt.Sprintf("Feishu request failed (class=%s, code=%d): %s", e.Class, e.Code, e.Message)
}

func (e *Error) DeliveryFailure() domain.DeliveryFailure {
	return domain.DeliveryFailure{Class: e.Class, Retryable: e.Retryable, Message: e.Error()}
}

func NewClient(config Config, httpClient *http.Client) (*Client, error) {
	if strings.TrimSpace(config.AppID) == "" {
		return nil, errors.New("Feishu App ID is required")
	}
	if strings.TrimSpace(config.AppSecret) == "" {
		return nil, errors.New("Feishu App Secret is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultBaseURL
	}
	config.BaseURL = strings.TrimRight(config.BaseURL, "/")
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}
	if config.TokenMargin <= 0 {
		config.TokenMargin = defaultTokenMargin
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	}
	return &Client{config: config, httpClient: httpClient, now: time.Now}, nil
}

func (c *Client) Send(ctx context.Context, notification domain.Notification) (domain.DeliveryReceipt, error) {
	if notification.Recipient.Kind != domain.AddressKindEmail {
		return domain.DeliveryReceipt{}, &Error{Class: "invalid_recipient", Retryable: false, Message: "Feishu personal messages require an email recipient"}
	}
	email := strings.ToLower(strings.TrimSpace(notification.Recipient.Value))
	if email == "" || !strings.Contains(email, "@") {
		return domain.DeliveryReceipt{}, &Error{Class: "invalid_recipient", Retryable: false, Message: "recipient email is invalid"}
	}
	content, err := RenderCard(notification)
	if err != nil {
		return domain.DeliveryReceipt{}, err
	}
	token, err := c.accessToken(ctx, false)
	if err != nil {
		return domain.DeliveryReceipt{}, err
	}
	response, err := c.sendMessage(ctx, token, email, content)
	if err == nil {
		return domain.DeliveryReceipt{ProviderMessageID: response.MessageID}, nil
	}
	var feishuErr *Error
	if errors.As(err, &feishuErr) && isTokenError(feishuErr) {
		token, refreshErr := c.accessToken(ctx, true)
		if refreshErr != nil {
			return domain.DeliveryReceipt{}, refreshErr
		}
		response, retryErr := c.sendMessage(ctx, token, email, content)
		if retryErr != nil {
			return domain.DeliveryReceipt{}, retryErr
		}
		return domain.DeliveryReceipt{ProviderMessageID: response.MessageID}, nil
	}
	return domain.DeliveryReceipt{}, err
}

func (c *Client) LookupEmails(ctx context.Context, emails []string) (map[string]ports.DirectoryUser, error) {
	unique := uniqueEmails(emails)
	if len(unique) == 0 {
		return map[string]ports.DirectoryUser{}, nil
	}
	result, err := c.lookupEmailsWithToken(ctx, unique, false)
	if err != nil {
		var feishuErr *Error
		if errors.As(err, &feishuErr) && isTokenError(feishuErr) {
			return c.lookupEmailsWithToken(ctx, unique, true)
		}
		return nil, err
	}
	return result, nil
}

func (c *Client) lookupEmailsWithToken(ctx context.Context, emails []string, forceToken bool) (map[string]ports.DirectoryUser, error) {
	result := make(map[string]ports.DirectoryUser)
	token, err := c.accessToken(ctx, forceToken)
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(emails); start += userLookupBatch {
		end := start + userLookupBatch
		if end > len(emails) {
			end = len(emails)
		}
		var response struct {
			Code    int    `json:"code"`
			Message string `json:"msg"`
			Data    struct {
				Users []struct {
					UserID string `json:"user_id"`
					Email  string `json:"email"`
				} `json:"user_list"`
			} `json:"data"`
		}
		status, requestErr := c.postJSON(ctx, userLookupPath, map[string][]string{"emails": emails[start:end]}, token, &response)
		if requestErr != nil {
			return nil, requestErr
		}
		if status < 200 || status >= 300 || response.Code != 0 {
			return nil, classifyError(status, response.Code, response.Message, false)
		}
		for _, user := range response.Data.Users {
			email := strings.ToLower(strings.TrimSpace(user.Email))
			if email == "" || !strings.Contains(email, "@") {
				continue
			}
			result[email] = ports.DirectoryUser{Email: email, ID: user.UserID}
		}
	}
	return result, nil
}

func uniqueEmails(emails []string) []string {
	seen := make(map[string]struct{}, len(emails))
	unique := make([]string, 0, len(emails))
	for _, email := range emails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" || !strings.Contains(email, "@") {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		unique = append(unique, email)
	}
	return unique
}

type messageResponse struct {
	MessageID string
}

func (c *Client) accessToken(ctx context.Context, force bool) (string, error) {
	now := c.now()
	c.mu.Lock()
	if !force && c.token != "" && now.Before(c.tokenExpires) {
		token := c.token
		c.mu.Unlock()
		return token, nil
	}
	c.mu.Unlock()
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if !force {
		now = c.now()
		c.mu.Lock()
		if c.token != "" && now.Before(c.tokenExpires) {
			token := c.token
			c.mu.Unlock()
			return token, nil
		}
		c.mu.Unlock()
	}

	var response struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Token   string `json:"tenant_access_token"`
		Expire  int    `json:"expire"`
	}
	status, err := c.postJSON(ctx, tokenPath, map[string]string{"app_id": c.config.AppID, "app_secret": c.config.AppSecret}, "", &response)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 || response.Code != 0 || response.Token == "" {
		return "", classifyError(status, response.Code, response.Message, true)
	}
	expiresIn := time.Duration(response.Expire) * time.Second
	if expiresIn <= 0 {
		expiresIn = 2 * time.Hour
	}
	expiresAt := c.now().Add(expiresIn)
	if expiresIn > c.config.TokenMargin {
		expiresAt = expiresAt.Add(-c.config.TokenMargin)
	}
	c.mu.Lock()
	c.token = response.Token
	c.tokenExpires = expiresAt
	c.mu.Unlock()
	return response.Token, nil
}

func (c *Client) sendMessage(ctx context.Context, token, email string, content []byte) (messageResponse, error) {
	payload := map[string]string{
		"receive_id": email,
		"msg_type":   "interactive",
		"content":    string(content),
	}
	var response struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	status, err := c.postJSON(ctx, messagePath, payload, token, &response)
	if err != nil {
		return messageResponse{}, err
	}
	if status < 200 || status >= 300 || response.Code != 0 {
		return messageResponse{}, classifyError(status, response.Code, response.Message, false)
	}
	return messageResponse{MessageID: response.Data.MessageID}, nil
}

func (c *Client) postJSON(ctx context.Context, path string, payload any, token string, response any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal Feishu request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create Feishu request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	httpResponse, err := c.httpClient.Do(request)
	if err != nil {
		return 0, &Error{Class: "transport", Retryable: true, Message: "Feishu request could not be completed"}
	}
	defer httpResponse.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(httpResponse.Body, 1<<20))
	if err != nil {
		return httpResponse.StatusCode, &Error{Class: "transport", Retryable: true, Message: "Feishu response could not be read"}
	}
	if err := json.Unmarshal(responseBody, response); err != nil {
		return httpResponse.StatusCode, &Error{HTTPStatus: httpResponse.StatusCode, Class: "invalid_response", Retryable: httpResponse.StatusCode >= 500, Message: "Feishu returned invalid JSON"}
	}
	return httpResponse.StatusCode, nil
}

func classifyError(httpStatus, code int, message string, tokenRequest bool) *Error {
	class := "application"
	retryable := false
	switch {
	case httpStatus == http.StatusUnauthorized || httpStatus == http.StatusForbidden:
		class = "authentication"
	case httpStatus == http.StatusTooManyRequests:
		class = "rate_limit"
		retryable = true
	case httpStatus >= 500:
		class = "server"
		retryable = true
	case httpStatus == http.StatusNotFound:
		class = "recipient_not_found"
	case code == 99991672:
		class = "permission"
	case tokenRequest:
		class = "authentication"
	}
	if code != 0 && tokenErrorCode(code) {
		class = "authentication"
	}
	return &Error{HTTPStatus: httpStatus, Code: code, Class: class, Retryable: retryable, Message: safeMessage(message)}
}

func isTokenError(err *Error) bool {
	return err != nil && (err.Class == "authentication" || tokenErrorCode(err.Code))
}

func tokenErrorCode(code int) bool {
	return code == 99991663 || code == 99991664 || code == 99991668
}

func safeMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return "unknown Feishu error"
	}
	return truncateMessage(message, 500)
}

func truncateMessage(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
