package config

import (
	"testing"
	"time"
)

func TestLoadDefaultsToSafeDryRun(t *testing.T) {
	clearConfigEnvironment(t)
	settings := Load()
	if settings.HTTPAddress != ":8080" || settings.DBPath != "data/crowsnest.sqlite3" || !settings.DryRun || !settings.ReconcileDryRun || settings.IdentitySyncEnabled || !settings.IdentitySyncDryRun || settings.IdentitySyncVerifyFeishu {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestLoadSeparatesWebhookAndReconcileSettings(t *testing.T) {
	clearConfigEnvironment(t)
	t.Setenv("CROWSNEST_GITLAB_WEBHOOK_TOKEN", "webhook-token")
	t.Setenv("CROWSNEST_WEBHOOK_SECRET", "incoming-token")
	t.Setenv("CROWSNEST_GITLAB_BASE_URL", "https://gitlab.example.com/")
	t.Setenv("CROWSNEST_RECONCILE_DRY_RUN", "false")
	t.Setenv("CROWSNEST_IDENTITY_SYNC_ENABLED", "true")
	t.Setenv("CROWSNEST_IDENTITY_SYNC_DRY_RUN", "false")
	t.Setenv("CROWSNEST_IDENTITY_SYNC_INTERVAL", "30m")
	t.Setenv("CROWSNEST_IDENTITY_SYNC_VERIFY_FEISHU", "true")
	t.Setenv("CROWSNEST_RECIPIENT_ALLOWLIST", "Carol@example.com, other@example.com")
	settings := Load()
	if settings.WebhookSecret != "incoming-token" || settings.GitLabHookToken != "webhook-token" || settings.GitLabBaseURL != "https://gitlab.example.com" || settings.ReconcileDryRun || !settings.IdentitySyncEnabled || settings.IdentitySyncDryRun || settings.IdentitySyncInterval != 30*time.Minute || !settings.IdentitySyncVerifyFeishu || len(settings.RecipientAllowlist) != 2 || settings.RecipientAllowlist[0] != "carol@example.com" {
		t.Fatalf("settings = %#v", settings)
	}
}

func clearConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"CROWSNEST_HTTP_ADDR", "CROWSNEST_DB_PATH", "CROWSNEST_WEBHOOK_SECRET", "CROWSNEST_DRY_RUN", "CROWSNEST_RECONCILE_DRY_RUN",
		"CROWSNEST_DELIVERY_POLL_INTERVAL", "CROWSNEST_RECONCILE_INTERVAL", "CROWSNEST_IDENTITY_SYNC_ENABLED", "CROWSNEST_IDENTITY_SYNC_DRY_RUN", "CROWSNEST_IDENTITY_SYNC_INTERVAL", "CROWSNEST_IDENTITY_SYNC_VERIFY_FEISHU", "CROWSNEST_RECIPIENT_ALLOWLIST", "CROWSNEST_ALLOWED_EMAIL_DOMAINS", "CROWSNEST_GITLAB_BASE_URL",
		"CROWSNEST_GITLAB_API_TOKEN", "CROWSNEST_GITLAB_WEBHOOK_URL", "CROWSNEST_GITLAB_WEBHOOK_TOKEN", "CROWSNEST_GITLAB_HOOK_TOKEN",
		"CROWSNEST_GITLAB_HOOK_NAME", "CROWSNEST_GITLAB_SSL_VERIFY", "CROWSNEST_FEISHU_BASE_URL", "CROWSNEST_FEISHU_APP_ID", "CROWSNEST_FEISHU_APP_SECRET",
	} {
		t.Setenv(key, "")
	}
}
