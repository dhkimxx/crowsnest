package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	HTTPAddress           string
	DBPath                string
	WebhookSecret         string
	DryRun                bool
	ReconcileDryRun       bool
	DeliveryPollInterval  time.Duration
	ReconcileInterval     time.Duration
	IdentitySyncEnabled   bool
	IdentitySyncDryRun    bool
	IdentitySyncInterval  time.Duration
	AllowedEmailDomains   []string
	GitLabBaseURL         string
	GitLabAPIToken        string
	GitLabWebhookURL      string
	GitLabWebhookToken    string
	GitLabHookToken       string
	GitLabHookName        string
	GitLabEnableSSLVerify bool
	FeishuBaseURL         string
	FeishuAppID           string
	FeishuAppSecret       string
}

func Load() Settings {
	webhookToken := os.Getenv("CROWSNEST_GITLAB_WEBHOOK_TOKEN")
	secret := os.Getenv("CROWSNEST_WEBHOOK_SECRET")
	if secret == "" {
		secret = webhookToken
	}
	hookToken := os.Getenv("CROWSNEST_GITLAB_HOOK_TOKEN")
	if hookToken == "" {
		hookToken = webhookToken
	}
	return Settings{
		HTTPAddress:           envOr("CROWSNEST_HTTP_ADDR", ":8080"),
		DBPath:                envOr("CROWSNEST_DB_PATH", "data/crowsnest.sqlite3"),
		WebhookSecret:         secret,
		DryRun:                boolEnv("CROWSNEST_DRY_RUN", true),
		ReconcileDryRun:       boolEnv("CROWSNEST_RECONCILE_DRY_RUN", true),
		DeliveryPollInterval:  durationEnv("CROWSNEST_DELIVERY_POLL_INTERVAL", 2*time.Second),
		ReconcileInterval:     durationEnv("CROWSNEST_RECONCILE_INTERVAL", 15*time.Minute),
		IdentitySyncEnabled:   boolEnv("CROWSNEST_IDENTITY_SYNC_ENABLED", false),
		IdentitySyncDryRun:    boolEnv("CROWSNEST_IDENTITY_SYNC_DRY_RUN", true),
		IdentitySyncInterval:  durationEnv("CROWSNEST_IDENTITY_SYNC_INTERVAL", time.Hour),
		AllowedEmailDomains:   csvEnv("CROWSNEST_ALLOWED_EMAIL_DOMAINS"),
		GitLabBaseURL:         strings.TrimRight(os.Getenv("CROWSNEST_GITLAB_BASE_URL"), "/"),
		GitLabAPIToken:        os.Getenv("CROWSNEST_GITLAB_API_TOKEN"),
		GitLabWebhookURL:      os.Getenv("CROWSNEST_GITLAB_WEBHOOK_URL"),
		GitLabWebhookToken:    webhookToken,
		GitLabHookToken:       hookToken,
		GitLabHookName:        envOr("CROWSNEST_GITLAB_HOOK_NAME", "Crowsnest"),
		GitLabEnableSSLVerify: boolEnv("CROWSNEST_GITLAB_SSL_VERIFY", true),
		FeishuBaseURL:         envOr("CROWSNEST_FEISHU_BASE_URL", "https://open.feishu.cn"),
		FeishuAppID:           os.Getenv("CROWSNEST_FEISHU_APP_ID"),
		FeishuAppSecret:       os.Getenv("CROWSNEST_FEISHU_APP_SECRET"),
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func boolEnv(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func csvEnv(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}
