package config

import "time"

const DefaultE2BTemplate = "managed-agent-sandbox"

func defaultConfig() Config {
	cfg := Config{
		NATS: NATSConfig{
			ConnectTimeout: 5 * time.Second,
			DrainTimeout:   10 * time.Second,
		},
		Storage: StorageConfig{
			MaxFileBytes:        500 * 1024 * 1024,
			WorkspaceLimitBytes: 500 * 1024 * 1024 * 1024,
			S3: S3Config{
				ForcePathStyle: true,
			},
		},
		Tunnel: TunnelConfig{
			DomainSuffix:        "tunnel.invalid",
			PollTimeout:         30 * time.Second,
			RequestTimeout:      2 * time.Minute,
			PresenceTTL:         60 * time.Second,
			TombstoneTTL:        5 * time.Minute,
			MaxPendingRequests:  256,
			MaxStoredRequests:   4096,
			MaxPendingBytes:     32 * 1024 * 1024,
			MaxBodyBytes:        1024 * 1024,
			MaxHeaderBytes:      32 * 1024,
			MaxHeaderValueBytes: 8 * 1024,
		},
		Batch: BatchConfig{
			WorkerEnabled:             true,
			WorkerConcurrency:         2,
			MaxRequests:               100000,
			MaxBodyBytes:              256 * 1024 * 1024,
			ResultRetentionDays:       29,
			UpstreamTimeout:           10 * time.Minute,
			JobLeaseDuration:          2 * time.Minute,
			JobLeaseHeartbeatInterval: 30 * time.Second,
			ExpirySweepInterval:       5 * time.Minute,
		},
		TranscriptArchive: TranscriptArchiveConfig{DryRun: true, TerminalSweepEnabled: true, TerminalDwell: 24 * time.Hour, ArchiveMinAge: 168 * time.Hour, SoftDeleteWindow: 336 * time.Hour, TargetSegmentRawBytes: 8388608, DeleteBatchRows: 500, MaxRowsPerJob: 50000},
		SandboxLifecycle:  SandboxLifecycleConfig{Enabled: true, DryRun: true, IdleTimeout: 24 * time.Hour},
		E2B: E2BConfig{
			Template:       DefaultE2BTemplate,
			RequestTimeout: 60 * time.Second,
			SandboxTimeout: 30 * time.Second,
		},
		EnvironmentRunner: EnvironmentRunnerConfig{
			Enabled:                 true,
			Concurrency:             2,
			PackageProvisionTimeout: 2 * time.Minute,
			ManagerPath:             "/usr/local/bin/environment-manager",
			ClaudeAgentVersion:      "2.1.251",
			ClaudePath:              "/opt/claude-code/bin/claude",
		},
		Observability: ObservabilityConfig{
			ContentCaptureEnabled: true,
			OTLP: ObservabilityOTLPConfig{
				MaxRequestBytes: 16 << 20,
				ForwardTimeout:  8 * time.Second,
			},
			Backend: ObservabilityBackendOpenObserve,
			OpenObserve: OpenObserveConfig{
				BaseURL:      "http://openobserve:5080",
				Organization: "oma",
				LogsStream:   "oma_claude_code",
				TracesStream: "oma_claude_code",
				Query: BackendQueryConfig{
					Timeout: 15 * time.Second,
				},
			},
		},
		Webhook: WebhookConfig{
			EventTypes:  defaultWebhookEventTypes(),
			Timeout:     10 * time.Second,
			MaxAttempts: 10,
		},
		Bootstrap: BootstrapConfig{
			WorkspaceName:       "default",
			OrganizationName:    "default",
			WorkspaceExternalID: "workspace_default",
			UserExternalID:      "user_default",
			APIKeyExternalID:    "api_key_default",
		},
	}
	setDefaultSeedAPIKeys(&cfg)
	return cfg
}

func defaultDatabaseAutoMigrate(appEnv string) bool {
	return appEnv != EnvironmentProd
}

func setDefaultSeedAPIKeys(cfg *Config) {
	cfg.Bootstrap.SeedAPIKeys = []SeedAPIKey{
		{ExternalID: cfg.Bootstrap.APIKeyExternalID, Key: DefaultAPIKey},
	}
}

func defaultWebhookEventTypes() []string {
	return []string{
		"session.created",
		"session.pending",
		"session.running",
		"session.idled",
		"session.requires_action",
		"session.archived",
		"session.deleted",
		"session.status_rescheduled",
		"session.status_run_started",
		"session.status_idled",
		"session.status_terminated",
		"session.updated",
		"session.error",
		"session.thread_created",
		"session.thread_status_running",
		"session.thread_status_idle",
		"session.thread_status_rescheduled",
		"session.thread_status_terminated",
		"session.thread_idled",
		"session.thread_terminated",
		"session.outcome_evaluation_ended",
		"vault.created",
		"vault.archived",
		"vault.deleted",
		"vault_credential.created",
		"vault_credential.archived",
		"vault_credential.deleted",
		"vault_credential.refresh_failed",
	}
}
