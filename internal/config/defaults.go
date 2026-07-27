package config

import "time"

func defaultConfig() Config {
	cfg := Config{
		Storage: StorageConfig{
			MaxFileBytes:        500 * 1024 * 1024,
			WorkspaceLimitBytes: 500 * 1024 * 1024 * 1024,
			S3: S3Config{
				ForcePathStyle: true,
			},
		},
		AnthropicUpstream: AnthropicUpstreamConfig{
			BaseURL: "https://api.anthropic.com",
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
		E2B: E2BConfig{
			Template:       "claude-code-interpreter",
			RequestTimeout: 60 * time.Second,
			SandboxTimeout: 5 * time.Minute,
		},
		EnvironmentRunner: EnvironmentRunnerConfig{
			Enabled:            true,
			Concurrency:        2,
			ManagerPath:        "/usr/local/bin/environment-manager",
			ClaudeAgentVersion: "2.1.120",
			ClaudePath:         "/opt/claude-code/bin/claude",
		},
		CodeSession: CodeSessionConfig{
			OTLPLogRoot:             "logs",
			OTLPLogBodyPreviewBytes: 256 * 1024,
		},
		Webhook: WebhookConfig{
			EventTypes:  defaultWebhookEventTypes(),
			Timeout:     10 * time.Second,
			MaxAttempts: 10,
		},
		Bootstrap: BootstrapConfig{
			WorkspaceName:          "default",
			OrganizationName:       "default",
			OrganizationExternalID: "org_default",
			WorkspaceExternalID:    "workspace_default",
			UserExternalID:         "user_default",
			APIKeyExternalID:       "api_key_default",
		},
		SDKFixtures: SDKFixtureConfig{
			FileID:            "file_id",
			BatchID:           "message_batch_id",
			AgentID:           "agent_011CZkYpogX7uDKUyvBTophP",
			ReferenceAgentID:  "agent_011CZkYqphY8vELVzwCUpqiQ",
			EnvironmentID:     "env_011CZkZ9X2dpNyB7HsEFoRfW",
			WorkID:            "work_id",
			SessionID:         "sesn_011CZkZAtmR3yMPDzynEDxu7",
			SessionResourceID: "sesrsc_011CZkZBJq5dWxk9fVLNcPht",
			SessionThreadID:   "sthr_011CZkZVWa6oIjw0rgXZpnBt",
			SessionEventID:    "sevt_011CZkZbF9oBV2h6c7qWZfnE",
			SkillID:           "skill_id",
			SkillVersion:      "version",
			DeploymentID:      "deployment_id",
			DeploymentRunID:   "deployment_run_id",
			APIKey:            OfficialSDKResourceAPIKey,
			APIKeyExternalID:  "api_key_official_sdk_resource_tests",
		},
	}
	setDefaultSeedAPIKeys(&cfg)
	return cfg
}

func defaultDatabaseAutoMigrate(appEnv string) bool {
	return appEnv != EnvironmentProd
}

func defaultCodeSessionOTLPFileLogEnabled(appEnv string) bool {
	return appEnv != EnvironmentProd
}

func setDefaultSeedAPIKeys(cfg *Config) {
	cfg.Bootstrap.SeedAPIKeys = []SeedAPIKey{
		{ExternalID: cfg.Bootstrap.APIKeyExternalID, Key: DefaultAPIKey},
		{ExternalID: cfg.SDKFixtures.APIKeyExternalID, Key: cfg.SDKFixtures.APIKey},
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
