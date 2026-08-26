package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	if err := validatePositiveValues(cfg); err != nil {
		return err
	}
	if err := validateVaultMasterKey(cfg.Vault); err != nil {
		return err
	}
	if err := validatePlatformOAuthClients(cfg.Vault.PlatformOAuthClients); err != nil {
		return err
	}
	if err := validateGitSSHtoHTTPSHosts(cfg.EnvironmentRunner.GitSSHtoHTTPSHosts); err != nil {
		return err
	}
	return validateCodeSessionUpstreamProxyMITMConfig(cfg.CodeSession)
}

// FindPlatformOAuthClient returns the registry entry whose mcp_server_url
// exactly matches (after TrimSpace) the given MCP server URL.
func FindPlatformOAuthClient(clients []PlatformOAuthClientConfig, mcpServerURL string) (PlatformOAuthClientConfig, bool) {
	want := strings.TrimSpace(mcpServerURL)
	if want == "" {
		return PlatformOAuthClientConfig{}, false
	}
	for _, client := range clients {
		if strings.TrimSpace(client.MCPServerURL) == want {
			client.MCPServerURL = want
			client.ClientID = strings.TrimSpace(client.ClientID)
			client.ClientSecret = strings.TrimSpace(client.ClientSecret)
			return client, true
		}
	}
	return PlatformOAuthClientConfig{}, false
}

func validatePlatformOAuthClients(clients []PlatformOAuthClientConfig) error {
	seen := make(map[string]struct{}, len(clients))
	for i, client := range clients {
		prefix := fmt.Sprintf("vault.platform_oauth_clients[%d]", i)
		mcpURL := strings.TrimSpace(client.MCPServerURL)
		clientID := strings.TrimSpace(client.ClientID)
		if mcpURL == "" {
			return fmt.Errorf("%s.mcp_server_url is required", prefix)
		}
		if clientID == "" {
			return fmt.Errorf("%s.client_id is required", prefix)
		}
		if _, ok := seen[mcpURL]; ok {
			return fmt.Errorf("%s.mcp_server_url %q is duplicated", prefix, mcpURL)
		}
		seen[mcpURL] = struct{}{}
	}
	return nil
}

// validateGitSSHtoHTTPSHosts validates and normalizes hosts in place (trim + lower-case).
// The slice header is shared with Config after YAML load, so Load observes the rewrite.
func validateGitSSHtoHTTPSHosts(hosts []string) error {
	seen := make(map[string]struct{}, len(hosts))
	for i, raw := range hosts {
		prefix := fmt.Sprintf("environment_runner.git_ssh_to_https_hosts[%d]", i)
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" {
			return fmt.Errorf("%s is empty", prefix)
		}
		if err := validateGitSSHtoHTTPSHost(host); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
		if _, ok := seen[host]; ok {
			return fmt.Errorf("%s %q is duplicated", prefix, host)
		}
		seen[host] = struct{}{}
		hosts[i] = host
	}
	return nil
}

// validateGitSSHtoHTTPSHost accepts only bare DNS hostnames ([a-z0-9.-]).
// Callers must pass already lower-cased hostnames.
func validateGitSSHtoHTTPSHost(host string) error {
	if host == "" || strings.Contains(host, "..") || host[0] == '.' || host[len(host)-1] == '.' {
		return errors.New("must be a bare hostname")
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '.', c == '-':
		default:
			return errors.New("must be a bare hostname")
		}
	}
	return nil
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
		{name: "code_session.otlp_log_body_preview_bytes", valid: cfg.CodeSession.OTLPLogBodyPreviewBytes > 0},
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
