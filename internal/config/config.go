package config

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultAPIKey             = "sk-ant-local-default"
	OfficialSDKResourceAPIKey = "my-anthropic-api-key"
)

type Config struct {
	AppEnv                              string
	Addr                                string
	DatabaseURL                         string
	PostgresAdminURL                    string
	DatabaseAutoMigrate                 bool
	RedisURL                            string
	S3Endpoint                          string
	S3Bucket                            string
	S3Region                            string
	S3AccessKeyID                       string
	S3SecretAccessKey                   string
	S3ForcePathStyle                    bool
	AnthropicUpstreamBaseURL            string
	AnthropicUpstreamAPIKey             string
	PublicBaseURL                       string
	MaxFileBytes                        int64
	WorkspaceStorageLimitBytes          int64
	BatchWorkerEnabled                  bool
	BatchWorkerConcurrency              int
	BatchMaxRequests                    int
	BatchMaxBodyBytes                   int64
	BatchResultRetentionDays            int
	BatchUpstreamTimeout                time.Duration
	BatchJobLeaseDuration               time.Duration
	BatchJobLeaseHeartbeatInterval      time.Duration
	BatchExpirySweepInterval            time.Duration
	E2BAPIKey                           string
	E2BAccessToken                      string
	E2BDomain                           string
	E2BAPIURL                           string
	E2BSandboxURL                       string
	E2BDebug                            bool
	E2BTemplate                         string
	E2BRequestTimeout                   time.Duration
	E2BSandboxTimeout                   time.Duration
	EnvironmentRunnerEnabled            bool
	EnvironmentRunnerConcurrency        int
	CodeSessionAPIBaseURL               string
	CodeSessionSandboxAPIBaseURL        string
	EnvironmentManagerPath              string
	ClaudeAgentVersion                  string
	ClaudePath                          string
	CodeSessionOTLPFileLogEnabled       bool
	CodeSessionOTLPLogRoot              string
	CodeSessionOTLPLogBodyPreviewBytes  int
	WebhookEndpointURL                  string
	WebhookSigningKey                   string
	WebhookEventTypes                   []string
	WebhookWorkerEnabled                bool
	WebhookTimeout                      time.Duration
	WebhookMaxAttempts                  int
	WebhookAllowInsecure                bool
	MCPDiscoveryEnabled                 bool
	MCPDiscoveryHMACKey                 string
	MCPDiscoveryProbeTimeout            time.Duration
	MCPDiscoveryWorkerConcurrency       int
	SeedAPIKeys                         []SeedAPIKey
	OfficialSDKFixtureFileID            string
	OfficialSDKFixtureBatchID           string
	OfficialSDKFixtureAgentID           string
	OfficialSDKFixtureReferenceAgentID  string
	OfficialSDKFixtureEnvironmentID     string
	OfficialSDKFixtureWorkID            string
	OfficialSDKFixtureSessionID         string
	OfficialSDKFixtureSessionResourceID string
	OfficialSDKFixtureSessionThreadID   string
	OfficialSDKFixtureSessionEventID    string
	OfficialSDKFixtureSkillID           string
	OfficialSDKFixtureSkillVersion      string
	OfficialSDKFixtureDeploymentID      string
	OfficialSDKFixtureDeploymentRunID   string
	OfficialSDKFixtureAPIKey            string
	DefaultWorkspaceName                string
	DefaultOrganizationName             string
	DefaultOrganizationExternalID       string
	DefaultWorkspaceExternalID          string
	DefaultUserExternalID               string
	DefaultAPIKeyExternalID             string
	OfficialSDKResourceAPIKeyExternalID string
}

type SeedAPIKey struct {
	ExternalID string
	Key        string
}

func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}

	appEnv := env("APP_ENV", "development")
	mcpDiscoveryHMACKey := "open-managed-agents-development-mcp-catalog-key"
	if appEnv == "production" {
		mcpDiscoveryHMACKey = ""
	}
	cfg := Config{
		AppEnv:                              appEnv,
		Addr:                                env("ADDR", ":8080"),
		DatabaseURL:                         env("DATABASE_URL", "postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable"),
		PostgresAdminURL:                    env("POSTGRES_ADMIN_URL", "postgresql://postgres@localhost:5432/postgres?sslmode=disable"),
		DatabaseAutoMigrate:                 envBool("DB_AUTO_MIGRATE", defaultDatabaseAutoMigrate(appEnv)),
		RedisURL:                            env("REDIS_URL", "redis://localhost:6379"),
		S3Endpoint:                          env("S3_ENDPOINT", "http://localhost:9000"),
		S3Bucket:                            env("S3_BUCKET", "claude-files"),
		S3Region:                            env("S3_REGION", "us-east-1"),
		S3AccessKeyID:                       env("S3_ACCESS_KEY_ID", "minioadmin"),
		S3SecretAccessKey:                   env("S3_SECRET_ACCESS_KEY", "minioadmin"),
		AnthropicUpstreamBaseURL:            env("ANTHROPIC_UPSTREAM_BASE_URL", "https://api.anthropic.com"),
		AnthropicUpstreamAPIKey:             env("ANTHROPIC_UPSTREAM_API_KEY", ""),
		PublicBaseURL:                       env("PUBLIC_BASE_URL", ""),
		MaxFileBytes:                        envInt64("MAX_FILE_BYTES", 500*1024*1024),
		WorkspaceStorageLimitBytes:          envInt64("WORKSPACE_STORAGE_LIMIT_BYTES", 500*1024*1024*1024),
		BatchWorkerEnabled:                  envBool("BATCH_WORKER_ENABLED", true),
		BatchWorkerConcurrency:              envInt("BATCH_WORKER_CONCURRENCY", 2),
		BatchMaxRequests:                    envInt("BATCH_MAX_REQUESTS", 100000),
		BatchMaxBodyBytes:                   envInt64("BATCH_MAX_BODY_BYTES", 256*1024*1024),
		BatchResultRetentionDays:            envInt("BATCH_RESULT_RETENTION_DAYS", 29),
		BatchUpstreamTimeout:                envDuration("BATCH_UPSTREAM_TIMEOUT", 10*time.Minute),
		BatchJobLeaseDuration:               envDuration("BATCH_JOB_LEASE_DURATION", 2*time.Minute),
		BatchJobLeaseHeartbeatInterval:      envDuration("BATCH_JOB_LEASE_HEARTBEAT_INTERVAL", 30*time.Second),
		BatchExpirySweepInterval:            envDuration("BATCH_EXPIRY_SWEEP_INTERVAL", 5*time.Minute),
		E2BAPIKey:                           env("E2B_API_KEY", ""),
		E2BAccessToken:                      env("E2B_ACCESS_TOKEN", ""),
		E2BDomain:                           env("E2B_DOMAIN", ""),
		E2BAPIURL:                           env("E2B_API_URL", ""),
		E2BSandboxURL:                       env("E2B_SANDBOX_URL", ""),
		E2BDebug:                            envBool("E2B_DEBUG", false),
		E2BTemplate:                         env("E2B_TEMPLATE", "claude-code-interpreter"),
		E2BRequestTimeout:                   envDuration("E2B_REQUEST_TIMEOUT", 60*time.Second),
		E2BSandboxTimeout:                   envDuration("E2B_SANDBOX_TIMEOUT", 5*time.Minute),
		EnvironmentRunnerEnabled:            envBool("ENVIRONMENT_RUNNER_ENABLED", true),
		EnvironmentRunnerConcurrency:        envInt("ENVIRONMENT_RUNNER_CONCURRENCY", 2),
		CodeSessionAPIBaseURL:               firstNonEmpty(env("CODE_SESSION_API_BASE_URL", ""), env("PUBLIC_BASE_URL", "")),
		CodeSessionSandboxAPIBaseURL:        firstNonEmpty(env("CODE_SESSION_SANDBOX_API_BASE_URL", ""), env("CODE_SESSION_API_BASE_URL", ""), env("PUBLIC_BASE_URL", "")),
		EnvironmentManagerPath:              env("ENVIRONMENT_MANAGER_PATH", "/usr/local/bin/environment-manager"),
		ClaudeAgentVersion:                  env("CLAUDE_AGENT_VERSION", "2.1.120"),
		ClaudePath:                          env("CLAUDE_PATH", "/opt/claude-code/bin/claude"),
		CodeSessionOTLPFileLogEnabled:       envBool("CODE_SESSION_OTLP_FILE_LOG_ENABLED", defaultCodeSessionOTLPFileLogEnabled(appEnv)),
		CodeSessionOTLPLogRoot:              env("CODE_SESSION_OTLP_LOG_ROOT", "logs"),
		CodeSessionOTLPLogBodyPreviewBytes:  envInt("CODE_SESSION_OTLP_LOG_BODY_PREVIEW_BYTES", 256*1024),
		WebhookEndpointURL:                  env("WEBHOOK_ENDPOINT_URL", ""),
		WebhookSigningKey:                   env("ANTHROPIC_WEBHOOK_SIGNING_KEY", ""),
		WebhookEventTypes:                   envCSV("WEBHOOK_EVENT_TYPES", defaultWebhookEventTypes()),
		WebhookTimeout:                      envDuration("WEBHOOK_TIMEOUT", 10*time.Second),
		WebhookMaxAttempts:                  envInt("WEBHOOK_MAX_ATTEMPTS", 10),
		WebhookAllowInsecure:                envBool("WEBHOOK_ALLOW_INSECURE", false),
		MCPDiscoveryEnabled:                 envBool("MCP_DISCOVERY_ENABLED", appEnv != "production"),
		MCPDiscoveryHMACKey:                 env("MCP_DISCOVERY_HMAC_KEY", mcpDiscoveryHMACKey),
		MCPDiscoveryProbeTimeout:            envDuration("MCP_DISCOVERY_PROBE_TIMEOUT", 10*time.Second),
		MCPDiscoveryWorkerConcurrency:       envInt("MCP_DISCOVERY_WORKER_CONCURRENCY", 3),
		OfficialSDKFixtureFileID:            "file_id",
		OfficialSDKFixtureBatchID:           "message_batch_id",
		OfficialSDKFixtureAgentID:           env("OFFICIAL_SDK_FIXTURE_AGENT_ID", "agent_011CZkYpogX7uDKUyvBTophP"),
		OfficialSDKFixtureReferenceAgentID:  env("OFFICIAL_SDK_FIXTURE_REFERENCE_AGENT_ID", "agent_011CZkYqphY8vELVzwCUpqiQ"),
		OfficialSDKFixtureEnvironmentID:     env("OFFICIAL_SDK_FIXTURE_ENVIRONMENT_ID", "env_011CZkZ9X2dpNyB7HsEFoRfW"),
		OfficialSDKFixtureWorkID:            env("OFFICIAL_SDK_FIXTURE_WORK_ID", "work_id"),
		OfficialSDKFixtureSessionID:         env("OFFICIAL_SDK_FIXTURE_SESSION_ID", "sesn_011CZkZAtmR3yMPDzynEDxu7"),
		OfficialSDKFixtureSessionResourceID: env("OFFICIAL_SDK_FIXTURE_SESSION_RESOURCE_ID", "sesrsc_011CZkZBJq5dWxk9fVLNcPht"),
		OfficialSDKFixtureSessionThreadID:   env("OFFICIAL_SDK_FIXTURE_SESSION_THREAD_ID", "sthr_011CZkZVWa6oIjw0rgXZpnBt"),
		OfficialSDKFixtureSessionEventID:    env("OFFICIAL_SDK_FIXTURE_SESSION_EVENT_ID", "sevt_011CZkZbF9oBV2h6c7qWZfnE"),
		OfficialSDKFixtureSkillID:           env("OFFICIAL_SDK_FIXTURE_SKILL_ID", "skill_id"),
		OfficialSDKFixtureSkillVersion:      env("OFFICIAL_SDK_FIXTURE_SKILL_VERSION", "version"),
		OfficialSDKFixtureDeploymentID:      env("OFFICIAL_SDK_FIXTURE_DEPLOYMENT_ID", "deployment_id"),
		OfficialSDKFixtureDeploymentRunID:   env("OFFICIAL_SDK_FIXTURE_DEPLOYMENT_RUN_ID", "deployment_run_id"),
		OfficialSDKFixtureAPIKey:            OfficialSDKResourceAPIKey,
		DefaultWorkspaceName:                "default",
		DefaultOrganizationName:             "default",
		DefaultOrganizationExternalID:       "org_default",
		DefaultWorkspaceExternalID:          "workspace_default",
		DefaultUserExternalID:               "user_default",
		DefaultAPIKeyExternalID:             "api_key_default",
		OfficialSDKResourceAPIKeyExternalID: "api_key_official_sdk_resource_tests",
	}
	cfg.S3ForcePathStyle = envBool("S3_FORCE_PATH_STYLE", true)
	cfg.WebhookWorkerEnabled = envBool("WEBHOOK_WORKER_ENABLED", cfg.WebhookEndpointURL != "" && cfg.WebhookSigningKey != "")
	cfg.SeedAPIKeys = []SeedAPIKey{
		{ExternalID: cfg.DefaultAPIKeyExternalID, Key: DefaultAPIKey},
		{ExternalID: cfg.OfficialSDKResourceAPIKeyExternalID, Key: OfficialSDKResourceAPIKey},
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.S3Endpoint == "" {
		return Config{}, errors.New("S3_ENDPOINT is required")
	}
	if cfg.S3Bucket == "" {
		return Config{}, errors.New("S3_BUCKET is required")
	}
	if cfg.S3AccessKeyID == "" || cfg.S3SecretAccessKey == "" {
		return Config{}, errors.New("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY are required")
	}
	if cfg.MCPDiscoveryEnabled && cfg.MCPDiscoveryHMACKey == "" {
		return Config{}, errors.New("MCP_DISCOVERY_HMAC_KEY is required when MCP discovery is enabled")
	}
	return cfg, nil
}

func loadDotEnv() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	for {
		envPath := filepath.Join(dir, ".env")
		if _, err := os.Stat(envPath); err == nil {
			return godotenv.Load(envPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func env(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func envBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func defaultDatabaseAutoMigrate(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "production", "prod":
		return false
	default:
		return true
	}
}

func defaultCodeSessionOTLPFileLogEnabled(appEnv string) bool {
	switch strings.ToLower(strings.TrimSpace(appEnv)) {
	case "production", "prod":
		return false
	default:
		return true
	}
}

func envInt64(key string, fallback int64) int64 {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envInt(key string, fallback int) int {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envCSV(key string, fallback []string) []string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	var result []string
	for _, part := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
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

func envDuration(key string, fallback time.Duration) time.Duration {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
