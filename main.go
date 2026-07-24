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

	"github.com/superduck-ai/open-managed-agents/internal/aiupstream"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/batches"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"
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
	if err := aiupstream.ValidateDeployment(cfg.AnthropicUpstream.BaseURL, cfg.AnthropicUpstream.APIKey); err != nil {
		log.Fatalf("validate AI gateway: %v", err)
	}

	database, err := db.Open(ctx, cfg)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer database.Close()

	if cfg.Database.AutoMigrate {
		if err := database.Migrate(ctx); err != nil {
			log.Fatalf("migrate database: %v", err)
		}
	} else {
		logger.Info("database auto migration disabled", "env", cfg.Env)
	}
	if err := database.Seed(ctx, cfg.Bootstrap.SeedAPIKeys); err != nil {
		log.Fatalf("seed database: %v", err)
	}
	catalog, err := modelcatalog.NewService(
		ctx,
		modelcatalog.NewPostgresStore(database),
		modelcatalog.NewHTTPUpstream(cfg.AnthropicUpstream),
		modelcatalog.Options{
			DefaultModelID:  cfg.ModelCatalog.DefaultModelID,
			RefreshInterval: cfg.ModelCatalog.RefreshInterval,
			RefreshTimeout:  cfg.ModelCatalog.RefreshTimeout,
		},
	)
	if err != nil {
		log.Fatalf("initialize model catalog: %v", err)
	}
	if err := catalog.Refresh(ctx); err != nil {
		logger.Warn("initial model catalog refresh failed", "error", err)
	}
	catalog.StartRefreshLoop(ctx, func(refreshErr error) {
		logger.Warn("model catalog refresh failed", "error", refreshErr)
	})
	platformSessions, err := platformsession.NewRedisStore(ctx, cfg.Redis.URL)
	if err != nil {
		log.Fatalf("open platform session store: %v", err)
	}
	defer platformSessions.Close()

	storageClient, err := storage.New(cfg.Storage)
	if err != nil {
		log.Fatalf("create object storage client: %v", err)
	}
	objectStore, err := storageClient.ForBucket(cfg.Storage.S3.Bucket)
	if err != nil {
		log.Fatalf("bind object storage bucket: %v", err)
	}
	if err := objectStore.Ensure(ctx); err != nil {
		log.Fatalf("ensure object store bucket: %v", err)
	}
	// 启动时只构造一套 code-session 签发器，并同时注入 HTTP server 与 environment runner。
	codeSessionCredentials, err := codesessions.NewSessionCredentials(cfg)
	if err != nil {
		log.Fatalf("load code-session credentials: %v", err)
	}
	// Filestore 与 code-session ingress 使用独立的 claims 与验证器；
	// 生产环境可共用同一 Ed25519 私钥文件，但两种 token 绝不互相代用。
	filestoreCredentials, err := filestore.NewTokenCredentials(cfg)
	if err != nil {
		log.Fatalf("load filestore credentials: %v", err)
	}
	cleanup.StartObjectCleanupWorker(ctx, database, storageClient, 30*time.Second)
	// 常规资源共享默认 bucket；清理任务通过 client 按各自持久化的 bucket 选择对象存储。
	filestore.StartFilestoreCleanupWorker(ctx, database, storageClient)
	if cfg.Batch.WorkerEnabled {
		batches.StartBatchWorker(ctx, database, objectStore, cfg)
		batches.StartBatchExpirySweep(ctx, database, cfg)
	}
	environments.StartRunnerWithStoreAndCredentials(ctx, database, objectStore, cfg, codeSessionCredentials)
	skillprewarm.StartWorker(ctx, database, objectStore, cfg)
	webhooks.StartWorker(ctx, database, cfg.Webhook)

	server := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: api.NewServer(api.ServerDeps{
			Config:                 cfg,
			DB:                     database,
			ObjectStore:            objectStore,
			Logger:                 logger,
			ModelCatalog:           catalog,
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
		logger.Info("Open Managed Agents API server listening", "addr", cfg.Server.Addr)
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
