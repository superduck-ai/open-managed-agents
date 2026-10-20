package config

import (
	"errors"
	"time"

	"go.yaml.in/yaml/v3"
)

type optional[T any] struct {
	value T
	set   bool
}

func (o *optional[T]) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!null" {
		return errors.New("must not be null")
	}
	var value T
	if err := node.Decode(&value); err != nil {
		return err
	}
	o.value = value
	o.set = true
	return nil
}

func (optional[T]) yamlOptional() {}

func (o optional[T]) valueOr(fallback T) T {
	if o.set {
		return o.value
	}
	return fallback
}

type yamlConfig struct {
	EnvironmentPackages  EnvironmentPackageConfig  `yaml:"environment_packages"`
	EnvironmentPrebuilds EnvironmentPrebuildConfig `yaml:"environment_prebuilds"`
	TranscriptArchive    TranscriptArchiveConfig   `yaml:"transcript_archive"`
	Env                  string                    `yaml:"env"`
	Server               ServerConfig              `yaml:"server"`
	Database             yamlDatabaseConfig        `yaml:"database"`
	Redis                RedisConfig               `yaml:"redis"`
	NATS                 yamlNATSConfig            `yaml:"nats"`
	Auth                 AuthConfig                `yaml:"auth"`
	Tunnel               TunnelConfig              `yaml:"tunnel"`
	Storage              StorageConfig             `yaml:"storage"`
	Batch                BatchConfig               `yaml:"batch"`
	SandboxLifecycle     SandboxLifecycleConfig    `yaml:"sandbox_lifecycle"`
	E2B                  E2BConfig                 `yaml:"e2b"`
	EnvironmentRunner    EnvironmentRunnerConfig   `yaml:"environment_runner"`
	CodeSession          yamlCodeSessionConfig     `yaml:"code_session"`
	Observability        ObservabilityConfig       `yaml:"observability"`
	Webhook              yamlWebhookConfig         `yaml:"webhook"`
	Vault                VaultConfig               `yaml:"vault"`
	Bootstrap            yamlBootstrapConfig       `yaml:"bootstrap"`
}

// Legacy flat fields exist only at the YAML boundary. All consumers use Local.
type yamlMasterKeyConfig struct {
	Provider       string                           `yaml:"provider"`
	Local          *LocalKeyConfig                  `yaml:"local"`
	AliyunKMS      *AliyunKMSConfig                 `yaml:"aliyun_kms"`
	HashicorpVault *HashicorpVaultConfig            `yaml:"hashicorp_vault"`
	Kek            optional[string]                 `yaml:"kek"`
	KekFile        optional[string]                 `yaml:"kek_file"`
	Version        optional[int64]                  `yaml:"version"`
	DecryptOnly    optional[[]DecryptOnlyKeyConfig] `yaml:"decrypt_only"`
}

func (m *MasterKeyConfig) UnmarshalYAML(node *yaml.Node) error {
	var input yamlMasterKeyConfig
	if err := node.Decode(&input); err != nil {
		return err
	}
	legacy := input.Kek.set || input.KekFile.set || input.Version.set || input.DecryptOnly.set
	if input.Local != nil && legacy {
		return errors.New("vault.master_key: local cannot be combined with legacy kek, kek_file, version or decrypt_only fields")
	}
	*m = MasterKeyConfig{
		Provider: input.Provider, Local: input.Local,
		AliyunKMS: input.AliyunKMS, HashicorpVault: input.HashicorpVault,
	}
	if legacy {
		local := &LocalKeyConfig{
			Kek: input.Kek.value, KekFile: input.KekFile.value,
			Version: input.Version.value, DecryptOnly: input.DecryptOnly.value,
		}
		// Older serialized remote configs included empty local fields and version 0/1.
		if m.EffectiveProvider() == "local" || local.KEKConfigured() || len(local.DecryptOnly) > 0 || (local.Version != 0 && local.Version != 1) {
			m.Local = local
		}
	}
	return nil
}

type yamlDatabaseConfig struct {
	URL         string         `yaml:"url"`
	AutoMigrate optional[bool] `yaml:"auto_migrate"`
}

type yamlNATSConfig struct {
	WebhookStream     WebhookStreamConfig     `yaml:"webhook_stream"`
	WorkerEventStream WorkerEventStreamConfig `yaml:"worker_event_stream"`
	URL               string                  `yaml:"url"`
	ConnectTimeout    time.Duration           `yaml:"connect_timeout"`
	DrainTimeout      time.Duration           `yaml:"drain_timeout"`
}

type yamlCodeSessionConfig struct {
	SandboxAPIBaseURL                  string `yaml:"sandbox_api_base_url"`
	JWTSigningPrivateKeyFile           string `yaml:"jwt_signing_private_key_file"`
	UpstreamProxyMITMEnabled           bool   `yaml:"upstream_proxy_mitm_enabled"`
	UpstreamProxyCAKeyFile             string `yaml:"upstream_proxy_ca_key_file"`
	UpstreamProxyDisableSSRFProtection bool   `yaml:"upstream_proxy_disable_ssrf_protection"`
	BrainClaimTokenFile                string `yaml:"brain_claim_token_file"`
}

type yamlWebhookConfig struct {
	Concurrency         int            `yaml:"concurrency"`
	WorkerEnabled       optional[bool] `yaml:"worker_enabled"`
	Timeout             time.Duration  `yaml:"timeout"`
	FailureDisableAfter time.Duration  `yaml:"failure_disable_after"`
	MaxAttempts         int            `yaml:"max_attempts"`
	AllowInsecure       bool           `yaml:"allow_insecure"`
}

type yamlBootstrapConfig struct {
	SeedAPIKeys         optional[[]SeedAPIKey] `yaml:"seed_api_keys"`
	WorkspaceName       string                 `yaml:"workspace_name"`
	OrganizationName    string                 `yaml:"organization_name"`
	WorkspaceExternalID string                 `yaml:"workspace_external_id"`
	UserExternalID      string                 `yaml:"user_external_id"`
	UserEmail           string                 `yaml:"user_email"`
	UserName            string                 `yaml:"user_name"`
	APIKeyExternalID    string                 `yaml:"api_key_external_id"`
}

func newYAMLConfig() yamlConfig {
	defaults := defaultConfig()
	return yamlConfig{
		Env:      defaults.Env,
		Server:   defaults.Server,
		Database: yamlDatabaseConfig{URL: defaults.Database.URL},
		Redis:    defaults.Redis,
		NATS: yamlNATSConfig{
			WorkerEventStream: defaults.NATS.WorkerEventStream,
			WebhookStream:     defaults.NATS.WebhookStream,
			URL:               defaults.NATS.URL,
			ConnectTimeout:    defaults.NATS.ConnectTimeout,
			DrainTimeout:      defaults.NATS.DrainTimeout,
		},
		Auth:                 defaults.Auth,
		Tunnel:               defaults.Tunnel,
		Storage:              defaults.Storage,
		Batch:                defaults.Batch,
		E2B:                  defaults.E2B,
		EnvironmentPrebuilds: defaults.EnvironmentPrebuilds,
		EnvironmentPackages:  defaults.EnvironmentPackages,
		TranscriptArchive:    defaults.TranscriptArchive,
		SandboxLifecycle:     defaults.SandboxLifecycle,
		EnvironmentRunner:    defaults.EnvironmentRunner,
		CodeSession: yamlCodeSessionConfig{
			SandboxAPIBaseURL:                  defaults.CodeSession.SandboxAPIBaseURL,
			JWTSigningPrivateKeyFile:           defaults.CodeSession.JWTSigningPrivateKeyFile,
			UpstreamProxyMITMEnabled:           defaults.CodeSession.UpstreamProxyMITMEnabled,
			UpstreamProxyCAKeyFile:             defaults.CodeSession.UpstreamProxyCAKeyFile,
			UpstreamProxyDisableSSRFProtection: defaults.CodeSession.UpstreamProxyDisableSSRFProtection,
			BrainClaimTokenFile:                defaults.CodeSession.BrainClaimTokenFile,
		},
		Observability: defaults.Observability,
		Webhook: yamlWebhookConfig{
			Concurrency:         defaults.Webhook.Concurrency,
			Timeout:             defaults.Webhook.Timeout,
			MaxAttempts:         defaults.Webhook.MaxAttempts,
			FailureDisableAfter: defaults.Webhook.FailureDisableAfter,
			AllowInsecure:       defaults.Webhook.AllowInsecure,
		},
		Vault: defaults.Vault,
		Bootstrap: yamlBootstrapConfig{
			WorkspaceName:       defaults.Bootstrap.WorkspaceName,
			OrganizationName:    defaults.Bootstrap.OrganizationName,
			WorkspaceExternalID: defaults.Bootstrap.WorkspaceExternalID,
			UserExternalID:      defaults.Bootstrap.UserExternalID,
			UserEmail:           defaults.Bootstrap.UserEmail,
			UserName:            defaults.Bootstrap.UserName,
			APIKeyExternalID:    defaults.Bootstrap.APIKeyExternalID,
		},
	}
}

func (input yamlConfig) resolve() Config {
	cfg := Config{
		Env:      input.Env,
		Server:   input.Server,
		Database: DatabaseConfig{URL: input.Database.URL},
		Redis:    input.Redis,
		NATS: NATSConfig{
			WorkerEventStream: input.NATS.WorkerEventStream,
			WebhookStream:     input.NATS.WebhookStream,
			URL:               input.NATS.URL,
			ConnectTimeout:    input.NATS.ConnectTimeout,
			DrainTimeout:      input.NATS.DrainTimeout,
		},
		Auth:                 input.Auth,
		Tunnel:               input.Tunnel,
		Storage:              input.Storage,
		Batch:                input.Batch,
		E2B:                  input.E2B,
		EnvironmentPrebuilds: input.EnvironmentPrebuilds,
		EnvironmentPackages:  input.EnvironmentPackages,
		TranscriptArchive:    input.TranscriptArchive,
		SandboxLifecycle:     input.SandboxLifecycle,
		EnvironmentRunner:    input.EnvironmentRunner,
		CodeSession: CodeSessionConfig{
			SandboxAPIBaseURL:                  input.CodeSession.SandboxAPIBaseURL,
			JWTSigningPrivateKeyFile:           input.CodeSession.JWTSigningPrivateKeyFile,
			UpstreamProxyMITMEnabled:           input.CodeSession.UpstreamProxyMITMEnabled,
			UpstreamProxyCAKeyFile:             input.CodeSession.UpstreamProxyCAKeyFile,
			UpstreamProxyDisableSSRFProtection: input.CodeSession.UpstreamProxyDisableSSRFProtection,
			BrainClaimTokenFile:                input.CodeSession.BrainClaimTokenFile,
		},
		Observability: input.Observability,
		Webhook: WebhookConfig{
			Concurrency:         input.Webhook.Concurrency,
			Timeout:             input.Webhook.Timeout,
			MaxAttempts:         input.Webhook.MaxAttempts,
			FailureDisableAfter: input.Webhook.FailureDisableAfter,
			AllowInsecure:       input.Webhook.AllowInsecure,
		},
		Vault: input.Vault,
		Bootstrap: BootstrapConfig{
			WorkspaceName:       input.Bootstrap.WorkspaceName,
			OrganizationName:    input.Bootstrap.OrganizationName,
			WorkspaceExternalID: input.Bootstrap.WorkspaceExternalID,
			UserExternalID:      input.Bootstrap.UserExternalID,
			UserEmail:           input.Bootstrap.UserEmail,
			UserName:            input.Bootstrap.UserName,
			APIKeyExternalID:    input.Bootstrap.APIKeyExternalID,
		},
	}
	cfg.Database.AutoMigrate = input.Database.AutoMigrate.valueOr(defaultDatabaseAutoMigrate(cfg.Env))
	cfg.Webhook.WorkerEnabled = input.Webhook.WorkerEnabled.valueOr(true)
	if input.Bootstrap.SeedAPIKeys.set {
		cfg.Bootstrap.SeedAPIKeys = input.Bootstrap.SeedAPIKeys.value
	} else {
		setDefaultSeedAPIKeys(&cfg)
	}
	return cfg
}
