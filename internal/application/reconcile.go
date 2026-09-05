package application

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dhkimxx/crowsnest/internal/ports"
)

type ReconcileScheduler struct {
	controller ports.HookController
	interval   time.Duration
	dryRun     bool
	logger     *slog.Logger
}

func NewReconcileScheduler(controller ports.HookController, interval time.Duration, dryRun bool, logger *slog.Logger) *ReconcileScheduler {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ReconcileScheduler{controller: controller, interval: interval, dryRun: dryRun, logger: logger}
}

func (s *ReconcileScheduler) Run(ctx context.Context) error {
	if s.controller == nil {
		return nil
	}
	if err := s.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("initial hook reconciliation failed", "error", err)
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				s.logger.Error("hook reconciliation failed", "error", err)
			}
		}
	}
}

func (s *ReconcileScheduler) runOnce(ctx context.Context) error {
	report, err := s.controller.Reconcile(ctx, ports.HookReconcileOptions{DryRun: s.dryRun})
	if err != nil {
		return err
	}
	s.logger.Info("hook reconciliation completed", "dry_run", report.DryRun, "projects_scanned", report.ProjectsScanned, "hooks_created", report.HooksCreated, "hooks_updated", report.HooksUpdated, "hooks_unchanged", report.HooksUnchanged, "hooks_failed", report.HooksFailed)
	return nil
}
