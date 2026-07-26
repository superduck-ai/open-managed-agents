package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/batches"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/skillprewarm"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func main() {
	logger := slog.New(logging.NewConsoleHandler(os.Stdout, slog.LevelInfo))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	database, err := db.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if cfg.Database.AutoMigrate {
		if err := database.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	} else {
		logger.Info("database auto migration disabled", "env", cfg.Env)
	}
	if err := database.Seed(ctx, cfg.Bootstrap.SeedAPIKeys); err != nil {
		return fmt.Errorf("seed database: %w", err)
	}
	platformSessions, err := platformsession.NewRedisStore(ctx, cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("open platform session store: %w", err)
	}
	defer platformSessions.Close()

	storageClient, err := storage.New(cfg.Storage)
	if err != nil {
		return fmt.Errorf("create object storage client: %w", err)
	}
	objectStore, err := storageClient.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		return fmt.Errorf("bind object storage bucket: %w", err)
	}
	if err := objectStore.Ensure(ctx); err != nil {
		return fmt.Errorf("ensure object store bucket: %w", err)
	}
	// 启动时只构造一套 code-session 签发器，并同时注入 HTTP server 与 environment runner。
	codeSessionCredentials, err := codesessions.NewSessionCredentials(cfg)
	if err != nil {
		return fmt.Errorf("load code-session credentials: %w", err)
	}
	// Filestore 与 code-session ingress 使用独立的 claims 与验证器；
	// 生产环境可共用同一 Ed25519 私钥文件，但两种 token 绝不互相代用。
	filestoreCredentials, err := filestore.NewTokenCredentials(cfg)
	if err != nil {
		return fmt.Errorf("load filestore credentials: %w", err)
	}
	cleanup.NewWorker(database, storageClient, 30*time.Second, logger.With("component", "cleanup")).Start(ctx)
	// 常规资源共享默认 bucket；清理任务通过 client 按各自持久化的 bucket 选择对象存储。
	filestore.NewCleanupWorker(database, storageClient, logger.With("component", "filestore_cleanup")).Start(ctx)
	batches.NewWorker(database, objectStore, cfg, logger.With("component", "batches")).Start(ctx)
	environments.StartRunnerWithStoreAndCredentials(ctx, database, objectStore, cfg, codeSessionCredentials, logger.With("component", "environment_runner"))
	skillprewarm.StartWorker(ctx, database, objectStore, cfg, logger.With("component", "skill_prewarm"))
	webhooks.NewWorker(database, cfg.Webhook, logger.With("component", "webhook_worker")).Start(ctx)

	server := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: api.NewServer(api.ServerDeps{
			Config:                 cfg,
			DB:                     database,
			ObjectStore:            objectStore,
			Logger:                 logger,
			PlatformStore:          platformSessions,
			CodeSessionCredentials: codeSessionCredentials,
			FilestoreCredentials:   filestoreCredentials,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("claude api server listening", "addr", cfg.Server.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	}
	return nil
}
