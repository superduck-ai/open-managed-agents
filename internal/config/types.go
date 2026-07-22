package config

import "time"

const (
	EnvironmentDev  = "dev"
	EnvironmentProd = "prod"
	StorageTypeS3   = "s3"
)

type Config struct {
	Env               string                  `yaml:"env"`
	Server            ServerConfig            `yaml:"server"`
	Database          DatabaseConfig          `yaml:"database"`
	Redis             RedisConfig             `yaml:"redis"`
	Storage           StorageConfig           `yaml:"storage"`
	AnthropicUpstream AnthropicUpstreamConfig `yaml:"anthropic_upstream"`
	WebSearch         WebSearchConfig         `yaml:"web_search"`
	Batch             BatchConfig             `yaml:"batch"`
	E2B               E2BConfig               `yaml:"e2b"`
	EnvironmentRunner EnvironmentRunnerConfig `yaml:"environment_runner"`
	CodeSession       CodeSessionConfig       `yaml:"code_session"`
	Webhook           WebhookConfig           `yaml:"webhook"`
	Bootstrap         BootstrapConfig         `yaml:"bootstrap"`
	SDKFixtures       SDKFixtureConfig        `yaml:"sdk_fixtures"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type DatabaseConfig struct {
	URL         string `yaml:"url"`
	AutoMigrate bool   `yaml:"auto_migrate"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
}

type StorageConfig struct {
	Type                string   `yaml:"type"`
	S3                  S3Config `yaml:"s3"`
	MaxFileBytes        int64    `yaml:"max_file_bytes"`
	WorkspaceLimitBytes int64    `yaml:"workspace_limit_bytes"`
}

type S3Config struct {
	Endpoint        string `yaml:"endpoint"`
	Bucket          string `yaml:"bucket"`
	Region          string `yaml:"region"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	ForcePathStyle  bool   `yaml:"force_path_style"`
}

type AnthropicUpstreamConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type WebSearchConfig struct {
	Provider     string               `yaml:"provider"`
	Endpoint     string               `yaml:"endpoint"`
	APIKey       string               `yaml:"api_key"`
	Timeout      time.Duration        `yaml:"timeout"`
	MaxToolLoops int                  `yaml:"max_tool_loops"`
	Brave        BraveWebSearchConfig `yaml:"brave"`
}

type BraveWebSearchConfig struct {
	Country        string   `yaml:"country"`
	SearchLanguage string   `yaml:"search_language"`
	UILanguage     string   `yaml:"ui_language"`
	Freshness      string   `yaml:"freshness"`
	SafeSearch     string   `yaml:"safe_search"`
	Spellcheck     *bool    `yaml:"spellcheck"`
	ResultFilter   string   `yaml:"result_filter"`
	Goggles        []string `yaml:"goggles"`
	ExtraSnippets  bool     `yaml:"extra_snippets"`
	Units          string   `yaml:"units"`
}

type BatchConfig struct {
	WorkerEnabled             bool          `yaml:"worker_enabled"`
	WorkerConcurrency         int           `yaml:"worker_concurrency"`
	MaxRequests               int           `yaml:"max_requests"`
	MaxBodyBytes              int64         `yaml:"max_body_bytes"`
	ResultRetentionDays       int           `yaml:"result_retention_days"`
	UpstreamTimeout           time.Duration `yaml:"upstream_timeout"`
	JobLeaseDuration          time.Duration `yaml:"job_lease_duration"`
	JobLeaseHeartbeatInterval time.Duration `yaml:"job_lease_heartbeat_interval"`
	ExpirySweepInterval       time.Duration `yaml:"expiry_sweep_interval"`
}

type E2BConfig struct {
	APIKey         string        `yaml:"api_key"`
	AccessToken    string        `yaml:"access_token"`
	Domain         string        `yaml:"domain"`
	APIURL         string        `yaml:"api_url"`
	SandboxURL     string        `yaml:"sandbox_url"`
	Debug          bool          `yaml:"debug"`
	Template       string        `yaml:"template"`
	RequestTimeout time.Duration `yaml:"request_timeout"`
	SandboxTimeout time.Duration `yaml:"sandbox_timeout"`
}

type EnvironmentRunnerConfig struct {
	Enabled            bool   `yaml:"enabled"`
	Concurrency        int    `yaml:"concurrency"`
	ManagerPath        string `yaml:"manager_path"`
	ClaudeAgentVersion string `yaml:"claude_agent_version"`
	ClaudePath         string `yaml:"claude_path"`
}

type CodeSessionConfig struct {
	SandboxAPIBaseURL        string `yaml:"sandbox_api_base_url"`
	OTLPFileLogEnabled       bool   `yaml:"otlp_file_log_enabled"`
	OTLPLogRoot              string `yaml:"otlp_log_root"`
	OTLPLogBodyPreviewBytes  int    `yaml:"otlp_log_body_preview_bytes"`
	JWTSigningPrivateKeyFile string `yaml:"jwt_signing_private_key_file"`
	// UpstreamProxyMITMEnabled 开启后，CCR CONNECT 会在服务端终止客户端 TLS，按 HTTP 转发，再独立验证真实上游 TLS。
	UpstreamProxyMITMEnabled bool `yaml:"upstream_proxy_mitm_enabled"`
	// UpstreamProxyCAKeyFile 是外部提供的稳定 CA 私钥，仅在 MITM 开启时校验和读取，且只能挂载到 API server。
	// MITM 开启后，服务启动时读取私钥并在内存中签发根证书；私钥绝不能进入 sandbox。
	UpstreamProxyCAKeyFile string `yaml:"upstream_proxy_ca_key_file"`
	// UpstreamProxyDisableSSRFProtection 是仅供本地 fake-IP/TUN 排障使用的危险开关；生产环境必须保持 false。
	UpstreamProxyDisableSSRFProtection bool `yaml:"upstream_proxy_disable_ssrf_protection"`
}

type WebhookConfig struct {
	EndpointURL   string        `yaml:"endpoint_url"`
	SigningKey    string        `yaml:"signing_key"`
	EventTypes    []string      `yaml:"event_types"`
	WorkerEnabled bool          `yaml:"worker_enabled"`
	Timeout       time.Duration `yaml:"timeout"`
	MaxAttempts   int           `yaml:"max_attempts"`
	AllowInsecure bool          `yaml:"allow_insecure"`
}

type BootstrapConfig struct {
	SeedAPIKeys            []SeedAPIKey `yaml:"seed_api_keys"`
	WorkspaceName          string       `yaml:"workspace_name"`
	OrganizationName       string       `yaml:"organization_name"`
	OrganizationExternalID string       `yaml:"organization_external_id"`
	WorkspaceExternalID    string       `yaml:"workspace_external_id"`
	UserExternalID         string       `yaml:"user_external_id"`
	APIKeyExternalID       string       `yaml:"api_key_external_id"`
}

type SDKFixtureConfig struct {
	FileID            string `yaml:"file_id"`
	BatchID           string `yaml:"batch_id"`
	AgentID           string `yaml:"agent_id"`
	ReferenceAgentID  string `yaml:"reference_agent_id"`
	EnvironmentID     string `yaml:"environment_id"`
	WorkID            string `yaml:"work_id"`
	SessionID         string `yaml:"session_id"`
	SessionResourceID string `yaml:"session_resource_id"`
	SessionThreadID   string `yaml:"session_thread_id"`
	SessionEventID    string `yaml:"session_event_id"`
	SkillID           string `yaml:"skill_id"`
	SkillVersion      string `yaml:"skill_version"`
	DeploymentID      string `yaml:"deployment_id"`
	DeploymentRunID   string `yaml:"deployment_run_id"`
	APIKey            string `yaml:"api_key"`
	APIKeyExternalID  string `yaml:"api_key_external_id"`
}

type SeedAPIKey struct {
	ExternalID string `yaml:"external_id"`
	Key        string `yaml:"key"`
}
