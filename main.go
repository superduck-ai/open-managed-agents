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

	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/batches"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deployments"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/redisclient"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
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

	database, err := db.Open(ctx, cfg, logger.With("component", "database"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if cfg.Database.AutoMigrate {
		if err := database.Migrate(ctx); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
		if err := riverjobs.Migrate(ctx, database, logger.With("component", "river_jobs")); err != nil {
			return fmt.Errorf("migrate River: %w", err)
		}
	} else {
		logger.Info("database auto migration disabled", "env", cfg.Env)
	}
	if err := database.Seed(ctx, cfg.Bootstrap.SeedAPIKeys); err != nil {
		return fmt.Errorf("seed database: %w", err)
	}
	redisClient, err := redisclient.Open(ctx, cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("open redis client: %w", err)
	}
	defer redisClient.Close()
	platformSessions := platformsession.NewRedisStore(redisClient)
	platformAuthProvider := platformauth.New(cfg.Auth, database, redisClient, logger.With("component", "platform_auth"))
	sessionEventBus, err := sessionfanout.NewRedis(ctx, redisClient, logger.With("component", "session_event_bus"))
	if err != nil {
		return fmt.Errorf("open session event fanout: %w", err)
	}
	defer sessionEventBus.Close()
	tunnelBroker := tunnels.NewBroker(redisClient, cfg.Tunnel)

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
	vaultSecrets, err := buildVaultSecretsService(ctx, cfg)
	if err != nil {
		return fmt.Errorf("load vault secrets service: %w", err)
	}
	filestoreService := filestore.NewService(cfg, database, objectStore)
	cleanup.NewWorker(database, storageClient, 30*time.Second, logger.With("component", "cleanup")).Start(ctx)
	// 常规资源共享默认 bucket；清理任务通过 client 按各自持久化的 bucket 选择对象存储。
	filestore.NewCleanupWorker(database, storageClient, logger.With("component", "filestore_cleanup")).Start(ctx)
	batches.NewWorker(
		database,
		objectStore,
		cfg.Batch,
		batches.NewHTTPUpstreamClient(database, vaultSecrets, cfg.Batch),
		logger.With("component", "batches"),
	).Start(ctx)
	environmentLogger := logger.With("component", "environment_runner")
	sandboxProvider := e2bruntime.NewProvider(cfg.E2B)
	environmentRunner, err := environments.NewRunner(environments.RunnerDependencies{
		DB:              database,
		Provider:        sandboxProvider,
		Config:          cfg,
		CodeSessions:    codesessions.NewServiceWithCredentials(database, codeSessionCredentials, environmentLogger),
		Skills:          skillsapi.NewRuntimeResolver(database),
		FilestoreTokens: filestoreCredentials,
		Logger:          environmentLogger,
	})
	if err != nil {
		return fmt.Errorf("create environment runner: %w", err)
	}
	environmentRunner.Start(ctx)
	webhooks.NewWorker(database, cfg.Webhook, logger.With("component", "webhook_worker")).Start(ctx)
	workers := river.NewWorkers()
	deployments.RegisterScheduledWorkers(workers, database)
	lifecycle := environments.NewSandboxLifecycle(database, sandboxProvider,
		cfg.SandboxLifecycle, logger.With("component", "sandbox_lifecycle"))
	lifecycle.Register(workers)
	jobClient, err := riverjobs.NewClient(database, logger.With("component", "river_jobs"), workers,
		map[string]river.QueueConfig{deployments.DeploymentScheduleQueue: {MaxWorkers: 10}, environments.SandboxLifecycleQueue: {MaxWorkers: 4}})
	if err != nil {
		return fmt.Errorf("create River client: %w", err)
	}
	if err := lifecycle.Configure(ctx, jobClient); err != nil {
		return fmt.Errorf("configure sandbox lifecycle: %w", err)
	}
	deploymentScheduler := deployments.NewDeploymentScheduler(database, jobClient, logger.With("component", "deployment_scheduler"))
	if err := deploymentScheduler.Start(ctx); err != nil {
		return fmt.Errorf("start deployment scheduler: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := deploymentScheduler.Stop(stopCtx); err != nil {
			logger.Error("stop deployment scheduler", "error", err)
		}
	}()

	server := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: api.NewServer(api.ServerDeps{
			Config:                 cfg,
			DB:                     database,
			ObjectStore:            objectStore,
			Logger:                 logger,
			PlatformStore:          platformSessions,
			PlatformAuth:           platformAuthProvider,
			CodeSessionCredentials: codeSessionCredentials,
			SandboxTimeoutExtender: sandboxProvider,
			FilestoreCredentials:   filestoreCredentials,
			FilestoreService:       filestoreService,
			VaultSecrets:           vaultSecrets,
			Redis:                  redisClient,
			SessionEventBus:        sessionEventBus,
			TunnelBroker:           tunnelBroker,
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

// buildVaultSecretsService loads the vault KEK ring and returns the envelope
// encryption service. The current KEK comes from config (kek base64 or
// kek_file); optional decrypt_only entries keep older versions openable after
// rotation without rewrap. A configured KEK is required in every env.
func buildVaultSecretsService(ctx context.Context, cfg config.Config) (*secrets.Service, error) {
	mk := cfg.Vault.MasterKey
	kek, err := secrets.ResolveKEK(mk.Kek, mk.KekFile)
	if err != nil {
		return nil, err
	}
	current := secrets.LocalKeyMaterial{
		Version: mk.EffectiveVersion(),
		KEK:     kek,
	}
	decryptOnly := make([]secrets.LocalKeyMaterial, 0, len(mk.DecryptOnly))
	for i, entry := range mk.DecryptOnly {
		resolved, err := secrets.ResolveKEK(entry.Kek, entry.KekFile)
		if err != nil {
			return nil, fmt.Errorf("vault.master_key.decrypt_only[%d]: %w", i, err)
		}
		decryptOnly = append(decryptOnly, secrets.LocalKeyMaterial{
			Version: entry.Version,
			KEK:     resolved,
		})
	}
	return secrets.NewLocalServiceWithKeys(ctx, current, decryptOnly)
}
