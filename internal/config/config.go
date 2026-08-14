package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/modelmapping"
)

const (
	DefaultAPIKey             = "sk-ant-local-default"
	OfficialSDKResourceAPIKey = "my-anthropic-api-key"
)

func Load() (Config, error) {
	configPath, found, err := findConfigFile()
	if err != nil {
		return Config{}, err
	}
	if !found {
		return Config{}, fmt.Errorf("%s is required; create it in the repository or set CONFIG_FILE", defaultConfigFilePath)
	}

	cfg, err := loadYAMLConfig(configPath)
	if err != nil {
		return Config{}, err
	}

	if err := resolveConfigPaths(&cfg, configFileDirectory(configPath)); err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if strings.TrimSpace(cfg.Env) == "" {
		return errors.New("env is required")
	}
	if cfg.Env != EnvironmentDev && cfg.Env != EnvironmentProd {
		return fmt.Errorf("env must be %q or %q", EnvironmentDev, EnvironmentProd)
	}
	if strings.TrimSpace(cfg.Server.Addr) == "" {
		return errors.New("server.addr is required")
	}
	if strings.TrimSpace(cfg.Database.URL) == "" {
		return errors.New("database.url is required")
	}
	if strings.TrimSpace(cfg.Redis.URL) == "" {
		return errors.New("redis.url is required")
	}
	if strings.TrimSpace(cfg.Storage.Type) == "" {
		return errors.New("storage.type is required")
	}
	if cfg.Storage.Type != StorageTypeS3 {
		return fmt.Errorf("storage.type must be %q", StorageTypeS3)
	}
	if strings.TrimSpace(cfg.Storage.S3.Endpoint) == "" {
		return errors.New("storage.s3.endpoint is required")
	}
	if strings.TrimSpace(cfg.Storage.S3.Bucket) == "" {
		return errors.New("storage.s3.bucket is required")
	}
	if strings.TrimSpace(cfg.Storage.S3.Region) == "" {
		return errors.New("storage.s3.region is required")
	}
	if strings.TrimSpace(cfg.Storage.S3.AccessKeyID) == "" || strings.TrimSpace(cfg.Storage.S3.SecretAccessKey) == "" {
		return errors.New("storage.s3.access_key_id and storage.s3.secret_access_key are required")
	}
	if err := modelmapping.Validate(cfg.AnthropicUpstream.ModelMappings); err != nil {
		return fmt.Errorf("anthropic_upstream.model_mappings: %w", err)
	}
	if err := validatePositiveValues(cfg); err != nil {
		return err
	}
	if err := validateVaultMasterKey(cfg.Vault); err != nil {
		return err
	}
	if err := validateObservabilityConfig(cfg.Observability); err != nil {
		return err
	}
	if err := validateCodeSessionSandboxAPIBaseURL(cfg.Env, cfg.CodeSession, cfg.Observability.Enabled); err != nil {
		return err
	}
	return validateCodeSessionUpstreamProxyMITMConfig(cfg.CodeSession)
}

func (m MasterKeyConfig) inlineKEKSet() bool {
	return strings.TrimSpace(m.Kek) != ""
}

func (m MasterKeyConfig) fileKEKSet() bool {
	return strings.TrimSpace(m.KekFile) != ""
}

// KEKConfigured reports whether either inline kek or kek_file is set.
func (m MasterKeyConfig) KEKConfigured() bool {
	return m.inlineKEKSet() || m.fileKEKSet()
}

// EffectiveVersion returns the current wrap key version. Unset/0 defaults to 1
// so single-key deployments need not declare version explicitly.
func (m MasterKeyConfig) EffectiveVersion() int64 {
	if m.Version == 0 {
		return 1
	}
	return m.Version
}

// validateVaultMasterKey enforces the vault KEK input contract: exactly one of
// kek / kek_file on the current key (required in every env, same as S3 keys),
// at most one source on each decrypt_only entry, and unique positive versions
// that do not collide with the current wrap version.
func validateVaultMasterKey(cfg VaultConfig) error {
	mk := cfg.MasterKey
	switch {
	case mk.inlineKEKSet() && mk.fileKEKSet():
		return errors.New("configure at most one of vault.master_key.kek or kek_file")
	case !mk.KEKConfigured():
		return errors.New("vault.master_key.kek or kek_file is required")
	}
	if mk.Version < 0 {
		return errors.New("vault.master_key.version must be >= 0")
	}
	currentVersion := mk.EffectiveVersion()
	seen := map[int64]struct{}{currentVersion: {}}
	for i, entry := range mk.DecryptOnly {
		prefix := fmt.Sprintf("vault.master_key.decrypt_only[%d]", i)
		if entry.Version <= 0 {
			return fmt.Errorf("%s.version must be a positive integer", prefix)
		}
		if _, ok := seen[entry.Version]; ok {
			if entry.Version == currentVersion {
				return fmt.Errorf("%s.version %d collides with vault.master_key.version", prefix, entry.Version)
			}
			return fmt.Errorf("%s.version %d is duplicated", prefix, entry.Version)
		}
		seen[entry.Version] = struct{}{}
		inline := strings.TrimSpace(entry.Kek) != ""
		file := strings.TrimSpace(entry.KekFile) != ""
		switch {
		case inline && file:
			return fmt.Errorf("%s: configure at most one of kek or kek_file", prefix)
		case !inline && !file:
			return fmt.Errorf("%s: kek or kek_file is required", prefix)
		}
	}
	return nil
}

// validateCodeSessionSandboxAPIBaseURL 校验 sandbox 回连 OMA 的地址
// （启动 payload 里的 startup_context.api_base_url）。常规会话流量走
// environment-manager relay，不依赖该地址，所以平时可以为空；但开启
// observability 后 worker 要用它拼 OTLP 导出 endpoint 把遥测送回 OMA，
// 为空会导致 sandbox 内导出静默失败、数据永远不到达，因此升级为启动期硬错误。
func validateCodeSessionSandboxAPIBaseURL(environment string, cfg CodeSessionConfig, observabilityEnabled bool) error {
	baseURL := strings.TrimSpace(cfg.SandboxAPIBaseURL)
	if baseURL == "" {
		if observabilityEnabled {
			return errors.New("code_session.sandbox_api_base_url is required when observability.enabled is true")
		}
		return nil
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("code_session.sandbox_api_base_url must be an absolute HTTP(S) URL")
	}
	if environment == EnvironmentProd && parsed.Scheme != "https" {
		return errors.New("code_session.sandbox_api_base_url must use HTTPS in production")
	}
	return nil
}

func validatePositiveValues(cfg Config) error {
	checks := []struct {
		name  string
		valid bool
	}{
		{name: "storage.max_file_bytes", valid: cfg.Storage.MaxFileBytes > 0},
		{name: "storage.workspace_limit_bytes", valid: cfg.Storage.WorkspaceLimitBytes > 0},
		{name: "batch.worker_concurrency", valid: cfg.Batch.WorkerConcurrency > 0},
		{name: "batch.max_requests", valid: cfg.Batch.MaxRequests > 0},
		{name: "batch.max_body_bytes", valid: cfg.Batch.MaxBodyBytes > 0},
		{name: "batch.result_retention_days", valid: cfg.Batch.ResultRetentionDays > 0},
		{name: "batch.upstream_timeout", valid: cfg.Batch.UpstreamTimeout > 0},
		{name: "batch.job_lease_duration", valid: cfg.Batch.JobLeaseDuration > 0},
		{name: "batch.job_lease_heartbeat_interval", valid: cfg.Batch.JobLeaseHeartbeatInterval > 0},
		{name: "batch.expiry_sweep_interval", valid: cfg.Batch.ExpirySweepInterval > 0},
		{name: "e2b.request_timeout", valid: cfg.E2B.RequestTimeout > 0},
		{name: "e2b.sandbox_timeout", valid: cfg.E2B.SandboxTimeout > 0},
		{name: "environment_runner.concurrency", valid: cfg.EnvironmentRunner.Concurrency > 0},
		{name: "environment_runner.package_provision_timeout", valid: cfg.EnvironmentRunner.PackageProvisionTimeout > 0},
		{name: "observability.otlp.max_request_bytes", valid: cfg.Observability.OTLP.MaxRequestBytes > 0},
		{name: "observability.otlp.forward_timeout", valid: cfg.Observability.OTLP.ForwardTimeout > 0},
		{name: "webhook.timeout", valid: cfg.Webhook.Timeout > 0},
		{name: "webhook.max_attempts", valid: cfg.Webhook.MaxAttempts > 0},
	}
	for _, check := range checks {
		if !check.valid {
			return fmt.Errorf("%s must be greater than zero", check.name)
		}
	}
	return nil
}

func validateObservabilityConfig(cfg ObservabilityConfig) error {
	if !cfg.Enabled {
		return nil
	}
	switch strings.TrimSpace(cfg.Backend) {
	case ObservabilityBackendOpenObserve:
		return validateOpenObserveConfig(cfg.OpenObserve)
	default:
		return fmt.Errorf("observability.backend must be %q when observability.enabled is true", ObservabilityBackendOpenObserve)
	}
}

func validateOpenObserveConfig(cfg OpenObserveConfig) error {
	required := []struct {
		name  string
		value string
	}{
		{name: "observability.openobserve.base_url", value: cfg.BaseURL},
		{name: "observability.openobserve.organization", value: cfg.Organization},
		{name: "observability.openobserve.logs_stream", value: cfg.LogsStream},
		{name: "observability.openobserve.traces_stream", value: cfg.TracesStream},
		{name: "observability.openobserve.ingestion.username", value: cfg.Ingestion.Username},
		{name: "observability.openobserve.ingestion.password", value: cfg.Ingestion.Password},
		{name: "observability.openobserve.query.username", value: cfg.Query.Username},
		{name: "observability.openobserve.query.password", value: cfg.Query.Password},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required when observability.enabled is true", field.name)
		}
	}
	if cfg.Query.Timeout <= 0 {
		return errors.New("observability.openobserve.query.timeout must be greater than zero")
	}
	baseURL, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || baseURL.Host == "" || (baseURL.Scheme != "http" && baseURL.Scheme != "https") ||
		baseURL.User != nil || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return errors.New("observability.openobserve.base_url must be an absolute HTTP(S) URL without userinfo, query, or fragment")
	}
	return nil
}

// validateCodeSessionUpstreamProxyMITMConfig 只在 MITM 开启时校验稳定私钥输入合同：
// 私钥必须配置为已存在的普通文件，且不会被本服务改写；MITM 关闭时该配置保持休眠。
func validateCodeSessionUpstreamProxyMITMConfig(cfg CodeSessionConfig) error {
	if !cfg.UpstreamProxyMITMEnabled {
		return nil
	}
	keyFile := strings.TrimSpace(cfg.UpstreamProxyCAKeyFile)
	if keyFile == "" {
		return errors.New("CCR upstream proxy MITM requires a stable CA private key")
	}

	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		return fmt.Errorf("CCR upstream proxy stable CA private key must be an existing regular file: %w", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return errors.New("CCR upstream proxy stable CA private key must be an existing regular file")
	}
	return nil
}
