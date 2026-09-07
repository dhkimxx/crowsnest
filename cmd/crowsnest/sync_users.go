package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/dhkimxx/crowsnest/internal/adapter/database/sqlite"
	"github.com/dhkimxx/crowsnest/internal/platform/config"
)

func syncUsers(logger *slog.Logger) error {
	settings := config.Load()
	store, err := sqlite.Open(settings.DBPath, sqlite.DefaultConfig())
	if err != nil {
		return err
	}
	defer store.Close()

	controller, err := buildHookController(settings)
	if err != nil {
		return err
	}
	if controller == nil {
		return errors.New("GitLab user synchronization is not configured")
	}
	service, err := buildIdentitySyncService(settings, store, controller)
	if err != nil {
		return err
	}
	dryRun := true
	for _, argument := range os.Args[2:] {
		switch argument {
		case "--apply":
			dryRun = false
		case "--dry-run":
			dryRun = true
		}
	}
	report, err := service.Sync(context.Background(), dryRun)
	logger.Info("identity synchronization completed",
		"dry_run", report.DryRun,
		"email_verification", report.EmailVerification,
		"gitlab_users", report.GitLabUsers,
		"eligible_users", report.EligibleUsers,
		"feishu_users_found", report.FeishuUsersFound,
		"mappings_considered", report.MappingsConsidered,
		"mappings_upserted", report.MappingsUpserted,
		"mappings_enabled", report.MappingsEnabled,
		"mappings_disabled", report.MappingsDisabled,
		"skipped_no_email", report.SkippedNoEmail,
		"skipped_domain", report.SkippedDomain,
		"skipped_inactive", report.SkippedInactive,
	)
	return err
}
