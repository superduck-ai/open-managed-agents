package main

import (
	"context"
	"errors"
	"log"
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
	"github.com/superduck-ai/open-managed-agents/internal/observability"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/skillprewarm"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
)

func main() {
	logger := slog.New(observability.NewConsoleHandler(os.Stdout, slog.LevelInfo))

	exitCode := 0
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.Open(ctx, cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if cfg.DatabaseAutoMigrate {
		if err := database.Migrate(ctx); err != nil {
			log.Fatalf("migrate database: %v", err)
		}
	} else {
		logger.Info("database auto migration disabled", "app_env", cfg.AppEnv)
	}
	if err := database.Seed(ctx, cfg.SeedAPIKeys); err != nil {
		log.Fatalf("seed database: %v", err)
	}
	platformSessions, err := platformsession.NewRedisStore(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("open platform session store: %v", err)
	}
	defer platformSessions.Close()

	objectStore, err := storage.NewMinIO(cfg)
	if err != nil {
		log.Fatalf("create object store: %v", err)
	}
	if err := objectStore.EnsureBucket(ctx); err != nil {
		log.Fatalf("ensure object store bucket: %v", err)
	}
	// 启动时只构造一套 code-session 签发器，并同时注入 HTTP server 与 environment runner。
	codeSessionCredentials, err := codesessions.NewSessionCredentials(cfg)
	if err != nil {
		log.Fatalf("load code-session credentials: %v", err)
	}
	cleanup.StartObjectCleanupWorker(ctx, database, objectStore, 30*time.Second)
	if cfg.BatchWorkerEnabled {
		batches.StartBatchWorker(ctx, database, objectStore, cfg)
		batches.StartBatchExpirySweep(ctx, database, cfg)
	}
	environments.StartRunnerWithStoreAndCredentials(ctx, database, objectStore, cfg, codeSessionCredentials)
	skillprewarm.StartWorker(ctx, database, objectStore, cfg)
	webhooks.StartWorker(ctx, database, cfg)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewServerWithPlatformSessionsAndCredentials(cfg, database, objectStore, logger, platformSessions, codeSessionCredentials),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("claude api server listening", "addr", cfg.Addr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown server: %v", err)
			exitCode = 1
			return
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("serve: %v", err)
			exitCode = 1
			return
		}
	}
}
