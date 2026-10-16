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

	"github.com/nats-io/nats.go"
	"github.com/riverqueue/river"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/batches"
	"github.com/superduck-ai/open-managed-agents/internal/cleanup"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/deploymentjobs"
	"github.com/superduck-ai/open-managed-agents/internal/deployments"
	"github.com/superduck-ai/open-managed-agents/internal/diagnostics"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/listeners"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/natsclient"
	"github.com/superduck-ai/open-managed-agents/internal/opsmetrics"
	"github.com/superduck-ai/open-managed-agents/internal/platformauth"
	"github.com/superduck-ai/open-managed-agents/internal/platformsession"
	"github.com/superduck-ai/open-managed-agents/internal/redisclient"
	"github.com/superduck-ai/open-managed-agents/internal/riverjobs"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/storage"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
	"github.com/superduck-ai/open-managed-agents/internal/transcriptretention"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
	"github.com/superduck-ai/open-managed-agents/internal/webhooks"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func main() {
	logger := slog.New(logging.NewConsoleHandler(os.Stdout, slog.LevelInfo))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(bootstrap *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop)

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	telemetryRuntime, err := telemetry.Setup(ctx, cfg.Telemetry, cfg.Env, bootstrap)
	if err != nil {
		return fmt.Errorf("setup telemetry: %w", err)
	}
	defer shutdownTelemetry(telemetryRuntime, bootstrap)
	logger := newApplicationLogger(bootstrap.Handler(), telemetryRuntime, cfg.Telemetry.Logs.SlogLevel())
	slog.SetDefault(logger)

	closeDiagnostics, err := diagnostics.Start(cfg.Server.DiagnosticsAddr, os.Getenv("OMA_DIAGNOSTICS_LISTENER_FD"), logger.With("component", "diagnostics"))
	if err != nil {
		return fmt.Errorf("start diagnostics: %w", err)
	}
	defer func() { _ = closeDiagnostics() }()

	database, err := db.Open(ctx, cfg, logger.With("component", "database"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	if err := initializeDatabase(ctx, cfg, database, logger); err != nil {
		return err
	}
	redisClient, err := redisclient.Open(ctx, cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("open redis client: %w", err)
	}
	defer redisClient.Close()
	platformSessions := platformsession.NewRedisStore(redisClient)
	platformAuthProvider := platformauth.New(cfg.Auth, database, redisClient, logger.With("component", "platform_auth"))
	natsConnection, err := natsclient.Open(ctx, cfg.NATS, logger.With("component", "nats"))
	if err != nil {
		return fmt.Errorf("open nats client: %w", err)
	}
	defer func() {
		if err := natsConnection.Drain(); err != nil {
			logger.Warn("drain nats connection", "error", err)
			natsConnection.Close()
		}
	}()
	sessionEventBus, err := sessionfanout.NewNATS(ctx, natsConnection, logger.With("component", "session_event_bus"))
	if err != nil {
		return fmt.Errorf("open session event fanout: %w", err)
	}
	defer sessionEventBus.Close()
	workerEventBroker, err := workerevents.NewJetStream(ctx, natsConnection, cfg.NATS.WorkerEventStream)
	if err != nil {
		return fmt.Errorf("open worker event broker: %w", err)
	}
	logger.Info("nats messaging ready", "jetstream", true)

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
	tunnelBroker, err := tunnels.NewBroker(ctx, natsConnection, cfg.Tunnel, tunnels.NewPayloadStore(database, objectStore), tunnels.NewRequestBindings(redisClient, cfg.Tunnel.RequestTimeout+cfg.Tunnel.TombstoneTTL))
	if err != nil {
		return fmt.Errorf("open tunnel broker: %w", err)
	}
	defer tunnelBroker.Close()

	workerEventAcks := workerevents.NewRedisAckStore(redisClient)
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
	vaultSecrets, err := secretservice.New(cfg.Vault.MasterKey)
	if err != nil {
		return fmt.Errorf("load vault secrets service: %w", err)
	}
	filestoreService := filestore.NewService(cfg, database, database, objectStore)
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
	sandboxProvider := e2bruntime.NewProvider(cfg.E2B, cfg.EnvironmentPackages.RegistryHosts()...)
	// runner 与 worker-event 过期处置共享同一个 code-session Service，
	// 过期策略只存在一份实现。
	runnerCodeSessions := codesessions.NewServiceWithCredentials(database, codeSessionCredentials, environmentLogger).
		WithWorkerEventBroker(workerEventBroker).
		WithWorkerEventState(workerEventAcks, objectStore)
	codesessions.NewWorkerEventExpiryWorker(runnerCodeSessions, logger.With("component", "worker_event_expiry")).Start(ctx)
	environmentRunner, err := environments.NewRunner(environments.RunnerDependencies{
		DB:              database,
		Provider:        sandboxProvider,
		Config:          cfg,
		CodeSessions:    runnerCodeSessions,
		Skills:          skillsapi.NewRuntimeResolver(database),
		FilestoreTokens: filestoreCredentials,
		Logger:          environmentLogger,
	})
	if err != nil {
		return fmt.Errorf("create environment runner: %w", err)
	}
	stopRunner := environmentRunner.Start(ctx)
	defer stopRunner()
	webhookQueue, err := webhooks.NewQueue(ctx, natsConnection, cfg.NATS.WebhookStream, cfg.Webhook)
	if err != nil {
		return fmt.Errorf("open webhook queue: %w", err)
	}
	webhookEnqueuer := webhooks.NewEnqueuer(database, webhookQueue, logger.With("component", "webhooks"))
	stopWebhookWorker, err := webhooks.NewWorker(database, webhookQueue, cfg.Webhook, logger.With("component", "webhook_worker")).Start(ctx)
	if err != nil {
		return fmt.Errorf("start webhook worker: %w", err)
	}
	defer stopWebhookWorker()
	workers := river.NewWorkers()
	prebuilds := environments.NewPrebuilds(database, cfg, logger.With("component", "environment_prebuild"))
	prebuilds.Register(workers)
	deploymentStore := deployments.NewStore(database, logger.With("component", "deployments")).
		WithEventPayloadStorage(objectStore).WithWebhooks(webhookEnqueuer)
	deployments.RegisterWorkers(workers, deploymentStore)
	lifecycle := environments.NewSandboxLifecycle(database, sandboxProvider,
		cfg.SandboxLifecycle, logger.With("component", "sandbox_lifecycle"))
	lifecycle.Register(workers)
	transcripts, err := transcriptretention.New(database, objectStore, cfg.TranscriptArchive, logger.With("component", "transcript-archive"))
	if err != nil {
		return fmt.Errorf("create transcript archive service: %w", err)
	}
	transcripts.Register(workers)
	jobClient, err := riverjobs.NewClient(database, logger.With("component", "river_jobs"), workers,
		map[string]river.QueueConfig{
			environments.PrebuildQueue:         {MaxWorkers: 4},
			deploymentjobs.Queue:               {MaxWorkers: 10},
			environments.SandboxLifecycleQueue: {MaxWorkers: 4},
			transcriptretention.Queue:          {MaxWorkers: 2},
		})
	if err != nil {
		return fmt.Errorf("create River client: %w", err)
	}
	if err := lifecycle.Configure(ctx, jobClient); err != nil {
		return fmt.Errorf("configure sandbox lifecycle: %w", err)
	}
	if err := transcripts.Configure(ctx, jobClient); err != nil {
		return fmt.Errorf("configure transcript archive: %w", err)
	}
	deploymentStore.Configure(jobClient)
	prebuilds.Configure(jobClient)
	if err := jobClient.Start(ctx); err != nil {
		return fmt.Errorf("start deployment scheduler: %w", err)
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := jobClient.Stop(stopCtx); err != nil {
			logger.Error("stop deployment scheduler", "error", err)
		}
	}()

	stopReporter, err := startStateMetrics(ctx, cfg, database, natsConnection, logger)
	if err != nil {
		return err
	}
	defer stopReporter()

	server := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: api.NewServer(api.ServerDeps{
			Prebuilds:              prebuilds,
			SandboxLifecycle:       lifecycle,
			Config:                 cfg,
			DB:                     database,
			Deployments:            deploymentStore,
			WebhookEnqueuer:        webhookEnqueuer,
			ObjectStore:            objectStore,
			Logger:                 logger,
			PlatformStore:          platformSessions,
			PlatformAuth:           platformAuthProvider,
			CodeSessionCredentials: codeSessionCredentials,
			SandboxTimeoutExtender: sandboxProvider,
			SandboxProvisioner:     environmentRunner,
			FilestoreCredentials:   filestoreCredentials,
			FilestoreService:       filestoreService,
			VaultSecrets:           vaultSecrets,
			Redis:                  redisClient,
			SessionEventBus:        sessionEventBus,
			WorkerEventBroker:      workerEventBroker,
			TunnelBroker:           tunnelBroker,
			TunnelPresence:         tunnels.NewConnectorPresence(redisClient, cfg.Tunnel.PresenceTTL),
			WorkerEventAcks:        workerEventAcks,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	return serveHTTP(ctx, server, logger)
}

func serveHTTP(ctx context.Context, server *http.Server, logger *slog.Logger) error {
	listener, err := listeners.Open(server.Addr, os.Getenv("OMA_HTTP_LISTENER_FD"))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	server.Handler = httpapi.WithStreamShutdown(ctx, server.Handler)
	errCh := make(chan error, 1)
	go func() {
		logger.Info("claude api server listening", "addr", server.Addr)
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return shutdownHTTP(shutdownCtx, server)
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	}
	return nil
}

func newApplicationLogger(console slog.Handler, telemetryRuntime *telemetry.Runtime, level slog.Level) *slog.Logger {
	handler := console
	if otelHandler := telemetryRuntime.LogHandler(); otelHandler != nil {
		handler = slog.NewMultiHandler(console, logging.NewLevelHandler(otelHandler, level))
	}
	return slog.New(logging.NewContextHandler(handler, telemetry.LogContextAttrs))
}

func shutdownTelemetry(telemetryRuntime *telemetry.Runtime, bootstrap *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := telemetryRuntime.Shutdown(ctx); err != nil {
		bootstrap.WarnContext(ctx, "shutdown telemetry", "error", err)
	}
}

func startStateMetrics(ctx context.Context, cfg config.Config, database *db.DB, natsConnection *nats.Conn, logger *slog.Logger) (func(), error) {
	if !cfg.Telemetry.StateMetricsActive() {
		return func() {}, nil
	}
	reporter, err := opsmetrics.New(opsmetrics.Dependencies{
		DB: database, NATS: natsConnection, Config: cfg.Telemetry.StateMetrics,
		Streams:   []string{workerevents.StreamName, webhooks.StreamName, tunnels.CommandStreamName},
		Consumers: []opsmetrics.ConsumerTarget{{Stream: webhooks.StreamName, Consumer: webhooks.DeliveryConsumerName}},
		Logger:    logger.With("component", "state_metrics"),
	})
	if err != nil {
		return nil, fmt.Errorf("create state metrics reporter: %w", err)
	}
	stop, err := reporter.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start state metrics reporter: %w", err)
	}
	return stop, nil
}

func initializeDatabase(ctx context.Context, cfg config.Config, database *db.DB, logger *slog.Logger) error {
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
	return nil
}

func shutdownHTTP(ctx context.Context, server *http.Server) error {
	if err := server.Shutdown(ctx); err != nil {
		return errors.Join(fmt.Errorf("shutdown server: %w", err), server.Close())
	}
	return nil
}
