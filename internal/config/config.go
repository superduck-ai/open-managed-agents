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
	return validateCodeSessionUpstreamProxyMITMConfig(cfg.CodeSession)
}

func validatePositiveValues(cfg Config) error {
	checks := []struct {
		name  string
		valid bool
	}{
		{name: "storage.max_file_bytes", valid: cfg.Storage.MaxFileBytes > 0},
		{name: "storage.workspace_limit_bytes", valid: cfg.Storage.WorkspaceLimitBytes > 0},
		{name: "model_catalog.refresh_interval", valid: cfg.ModelCatalog.RefreshInterval > 0},
		{name: "model_catalog.refresh_timeout", valid: cfg.ModelCatalog.RefreshTimeout > 0},
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
