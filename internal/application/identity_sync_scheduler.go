package application

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

type IdentitySyncScheduler struct {
	service  *IdentitySyncService
	interval time.Duration
	dryRun   bool
	logger   *slog.Logger
}

func NewIdentitySyncScheduler(service *IdentitySyncService, interval time.Duration, dryRun bool, logger *slog.Logger) *IdentitySyncScheduler {
	if interval <= 0 {
		interval = time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &IdentitySyncScheduler{service: service, interval: interval, dryRun: dryRun, logger: logger}
}

func (s *IdentitySyncScheduler) Run(ctx context.Context) error {
	if s == nil || s.service == nil {
		return nil
	}
	if err := s.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("initial identity synchronization failed", "error", err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error("identity synchronization failed", "error", err)
			}
		}
	}
}

func (s *IdentitySyncScheduler) runOnce(ctx context.Context) error {
	report, err := s.service.Sync(ctx, s.dryRun)
	s.logger.Info("identity synchronization completed",
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
