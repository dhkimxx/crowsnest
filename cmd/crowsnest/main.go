package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/dhkimxx/crowsnest/internal/adapter/database/sqlite"
	"github.com/dhkimxx/crowsnest/internal/adapter/git/gitlab/v17_6"
	"github.com/dhkimxx/crowsnest/internal/adapter/messenger/feishu"
	"github.com/dhkimxx/crowsnest/internal/application"
	"github.com/dhkimxx/crowsnest/internal/platform/config"
	"github.com/dhkimxx/crowsnest/internal/platform/httpserver"
	"github.com/dhkimxx/crowsnest/internal/ports"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	var err error
	switch command {
	case "serve":
		err = serve(logger)
	case "reconcile":
		err = reconcile(logger)
	case "import-users":
		err = importUsers(logger)
	case "import-preferences":
		err = importPreferences(logger)
	case "sync-users":
		err = syncUsers(logger)
	default:
		err = errors.New("usage: crowsnest [serve|reconcile|import-users|import-preferences|sync-users]")
	}
	if err != nil {
		logger.Error("crowsnest stopped", "error", err)
		os.Exit(1)
	}
}

func serve(logger *slog.Logger) error {
	settings := config.Load()
	store, err := sqlite.Open(settings.DBPath, sqlite.Config{
		LeaseDuration: 5 * time.Minute,
		MaxAttempts:   5,
		RetryDelay:    time.Second,
	})
	if err != nil {
		return err
	}
	defer store.Close()

	decoder := gitlabv176.NewDecoder()
	registry := application.NewDecoderRegistry(decoder)
	router := application.NewRouter(store, store, store, settings.AllowedEmailDomains)
	ingest := application.NewIngestService(registry, router, store, store, logger)
	messenger, err := buildMessenger(settings, logger)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var workers sync.WaitGroup

	deliveryWorker := application.NewDeliveryWorker(store, messenger, application.DeliveryWorkerConfig{
		WorkerID:     "crowsnest-delivery",
		BatchSize:    10,
		PollInterval: settings.DeliveryPollInterval,
	}, logger)
	workers.Add(1)
	go func() {
		defer workers.Done()
		if workerErr := deliveryWorker.Run(ctx); workerErr != nil && !errors.Is(workerErr, context.Canceled) {
			logger.Error("delivery worker stopped", "error", workerErr)
		}
	}()

	controller, err := buildHookController(settings)
	if err != nil {
		return err
	}
	if controller != nil {
		scheduler := application.NewReconcileScheduler(controller, settings.ReconcileInterval, settings.ReconcileDryRun, logger)
		workers.Add(1)
		go func() {
			defer workers.Done()
			if schedulerErr := scheduler.Run(ctx); schedulerErr != nil && !errors.Is(schedulerErr, context.Canceled) {
				logger.Error("hook reconcile scheduler stopped", "error", schedulerErr)
			}
		}()
	}
	if settings.IdentitySyncEnabled {
		if controller == nil {
			return errors.New("GitLab user synchronization requires the GitLab API configuration")
		}
		identitySync, syncErr := buildIdentitySyncService(settings, store, controller)
		if syncErr != nil {
			return syncErr
		}
		scheduler := application.NewIdentitySyncScheduler(identitySync, settings.IdentitySyncInterval, settings.IdentitySyncDryRun, logger)
		workers.Add(1)
		go func() {
			defer workers.Done()
			if schedulerErr := scheduler.Run(ctx); schedulerErr != nil && !errors.Is(schedulerErr, context.Canceled) {
				logger.Error("identity synchronization scheduler stopped", "error", schedulerErr)
			}
		}()
	}

	handler := httpserver.New(ingest, settings.WebhookSecret, logger)
	server := &http.Server{
		Addr:              settings.HTTPAddress,
		Handler:           handler.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			logger.Error("http server shutdown failed", "error", shutdownErr)
		}
	}()

	logger.Info("crowsnest serving", "address", settings.HTTPAddress, "dry_run", settings.DryRun)
	serverErr := server.ListenAndServe()
	stop()
	workers.Wait()
	if serverErr != nil && !errors.Is(serverErr, http.ErrServerClosed) {
		return serverErr
	}
	return nil
}

func reconcile(logger *slog.Logger) error {
	settings := config.Load()
	controller, err := buildHookController(settings)
	if err != nil {
		return err
	}
	if controller == nil {
		return errors.New("GitLab Hook Reconciler is not configured")
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
	report, err := controller.Reconcile(context.Background(), ports.HookReconcileOptions{DryRun: dryRun})
	logger.Info("hook reconciliation completed", "dry_run", report.DryRun, "projects_scanned", report.ProjectsScanned, "hooks_created", report.HooksCreated, "hooks_updated", report.HooksUpdated, "hooks_unchanged", report.HooksUnchanged, "hooks_failed", report.HooksFailed)
	return err
}

func buildHookController(settings config.Settings) (ports.HookController, error) {
	if settings.GitLabBaseURL == "" && settings.GitLabAPIToken == "" && settings.GitLabWebhookURL == "" {
		return nil, nil
	}
	return gitlabv176.NewHookController(gitlabv176.HookConfig{
		BaseURL:               settings.GitLabBaseURL,
		APIToken:              settings.GitLabAPIToken,
		WebhookURL:            settings.GitLabWebhookURL,
		WebhookToken:          settings.GitLabHookToken,
		HookName:              settings.GitLabHookName,
		EnableSSLVerification: settings.GitLabEnableSSLVerify,
	})
}

func buildIdentitySyncService(settings config.Settings, store ports.IdentitySyncStore, controller ports.HookController) (*application.IdentitySyncService, error) {
	if controller == nil {
		return nil, errors.New("GitLab user synchronization requires a GitLab user directory")
	}
	users, ok := controller.(ports.UserDirectory)
	if !ok {
		return nil, errors.New("configured GitLab adapter does not support user synchronization")
	}
	directory, err := feishu.NewClient(feishu.Config{
		BaseURL:   settings.FeishuBaseURL,
		AppID:     settings.FeishuAppID,
		AppSecret: settings.FeishuAppSecret,
	}, nil)
	if err != nil {
		return nil, err
	}
	return application.NewIdentitySyncService(users, directory, store, settings.AllowedEmailDomains), nil
}

func buildMessenger(settings config.Settings, logger *slog.Logger) (ports.Messenger, error) {
	if settings.DryRun {
		return application.NewDryRunMessenger(logger), nil
	}
	return feishu.NewClient(feishu.Config{
		BaseURL:   settings.FeishuBaseURL,
		AppID:     settings.FeishuAppID,
		AppSecret: settings.FeishuAppSecret,
	}, nil)
}
