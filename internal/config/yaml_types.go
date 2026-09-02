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
	Env               string                  `yaml:"env"`
	Server            ServerConfig            `yaml:"server"`
	Database          yamlDatabaseConfig      `yaml:"database"`
	Redis             RedisConfig             `yaml:"redis"`
	Auth              AuthConfig              `yaml:"auth"`
	Storage           StorageConfig           `yaml:"storage"`
	Batch             BatchConfig             `yaml:"batch"`
	SandboxLifecycle  SandboxLifecycleConfig  `yaml:"sandbox_lifecycle"`
	E2B               E2BConfig               `yaml:"e2b"`
	EnvironmentRunner EnvironmentRunnerConfig `yaml:"environment_runner"`
	CodeSession       yamlCodeSessionConfig   `yaml:"code_session"`
	Observability     ObservabilityConfig     `yaml:"observability"`
	Webhook           yamlWebhookConfig       `yaml:"webhook"`
	Vault             VaultConfig             `yaml:"vault"`
	Bootstrap         yamlBootstrapConfig     `yaml:"bootstrap"`
	SDKFixtures       SDKFixtureConfig        `yaml:"sdk_fixtures"`
}

type yamlDatabaseConfig struct {
	URL         string         `yaml:"url"`
	AutoMigrate optional[bool] `yaml:"auto_migrate"`
}

type yamlCodeSessionConfig struct {
	SandboxAPIBaseURL                  string `yaml:"sandbox_api_base_url"`
	JWTSigningPrivateKeyFile           string `yaml:"jwt_signing_private_key_file"`
	UpstreamProxyMITMEnabled           bool   `yaml:"upstream_proxy_mitm_enabled"`
	UpstreamProxyCAKeyFile             string `yaml:"upstream_proxy_ca_key_file"`
	UpstreamProxyDisableSSRFProtection bool   `yaml:"upstream_proxy_disable_ssrf_protection"`
}

type yamlWebhookConfig struct {
	EndpointURL   string         `yaml:"endpoint_url"`
	SigningKey    string         `yaml:"signing_key"`
	EventTypes    []string       `yaml:"event_types"`
	WorkerEnabled optional[bool] `yaml:"worker_enabled"`
	Timeout       time.Duration  `yaml:"timeout"`
	MaxAttempts   int            `yaml:"max_attempts"`
	AllowInsecure bool           `yaml:"allow_insecure"`
}

type yamlBootstrapConfig struct {
	SeedAPIKeys         optional[[]SeedAPIKey] `yaml:"seed_api_keys"`
	WorkspaceName       string                 `yaml:"workspace_name"`
	OrganizationName    string                 `yaml:"organization_name"`
	WorkspaceExternalID string                 `yaml:"workspace_external_id"`
	UserExternalID      string                 `yaml:"user_external_id"`
	APIKeyExternalID    string                 `yaml:"api_key_external_id"`
}

func newYAMLConfig() yamlConfig {
	defaults := defaultConfig()
	return yamlConfig{
		Env:               defaults.Env,
		Server:            defaults.Server,
		Database:          yamlDatabaseConfig{URL: defaults.Database.URL},
		Redis:             defaults.Redis,
		Auth:              defaults.Auth,
		Storage:           defaults.Storage,
		Batch:             defaults.Batch,
		E2B:               defaults.E2B,
		SandboxLifecycle:  defaults.SandboxLifecycle,
		EnvironmentRunner: defaults.EnvironmentRunner,
		CodeSession: yamlCodeSessionConfig{
			SandboxAPIBaseURL:                  defaults.CodeSession.SandboxAPIBaseURL,
			JWTSigningPrivateKeyFile:           defaults.CodeSession.JWTSigningPrivateKeyFile,
			UpstreamProxyMITMEnabled:           defaults.CodeSession.UpstreamProxyMITMEnabled,
			UpstreamProxyCAKeyFile:             defaults.CodeSession.UpstreamProxyCAKeyFile,
			UpstreamProxyDisableSSRFProtection: defaults.CodeSession.UpstreamProxyDisableSSRFProtection,
		},
		Observability: defaults.Observability,
		Webhook: yamlWebhookConfig{
			EndpointURL:   defaults.Webhook.EndpointURL,
			SigningKey:    defaults.Webhook.SigningKey,
			EventTypes:    defaults.Webhook.EventTypes,
			Timeout:       defaults.Webhook.Timeout,
			MaxAttempts:   defaults.Webhook.MaxAttempts,
			AllowInsecure: defaults.Webhook.AllowInsecure,
		},
		Vault: defaults.Vault,
		Bootstrap: yamlBootstrapConfig{
			WorkspaceName:       defaults.Bootstrap.WorkspaceName,
			OrganizationName:    defaults.Bootstrap.OrganizationName,
			WorkspaceExternalID: defaults.Bootstrap.WorkspaceExternalID,
			UserExternalID:      defaults.Bootstrap.UserExternalID,
			APIKeyExternalID:    defaults.Bootstrap.APIKeyExternalID,
		},
		SDKFixtures: defaults.SDKFixtures,
	}
}

func (input yamlConfig) resolve() Config {
	cfg := Config{
		Env:               input.Env,
		Server:            input.Server,
		Database:          DatabaseConfig{URL: input.Database.URL},
		Redis:             input.Redis,
		Auth:              input.Auth,
		Storage:           input.Storage,
		Batch:             input.Batch,
		E2B:               input.E2B,
		SandboxLifecycle:  input.SandboxLifecycle,
		EnvironmentRunner: input.EnvironmentRunner,
		CodeSession: CodeSessionConfig{
			SandboxAPIBaseURL:                  input.CodeSession.SandboxAPIBaseURL,
			JWTSigningPrivateKeyFile:           input.CodeSession.JWTSigningPrivateKeyFile,
			UpstreamProxyMITMEnabled:           input.CodeSession.UpstreamProxyMITMEnabled,
			UpstreamProxyCAKeyFile:             input.CodeSession.UpstreamProxyCAKeyFile,
			UpstreamProxyDisableSSRFProtection: input.CodeSession.UpstreamProxyDisableSSRFProtection,
		},
		Observability: input.Observability,
		Webhook: WebhookConfig{
			EndpointURL:   input.Webhook.EndpointURL,
			SigningKey:    input.Webhook.SigningKey,
			EventTypes:    input.Webhook.EventTypes,
			Timeout:       input.Webhook.Timeout,
			MaxAttempts:   input.Webhook.MaxAttempts,
			AllowInsecure: input.Webhook.AllowInsecure,
		},
		Vault: input.Vault,
		Bootstrap: BootstrapConfig{
			WorkspaceName:       input.Bootstrap.WorkspaceName,
			OrganizationName:    input.Bootstrap.OrganizationName,
			WorkspaceExternalID: input.Bootstrap.WorkspaceExternalID,
			UserExternalID:      input.Bootstrap.UserExternalID,
			APIKeyExternalID:    input.Bootstrap.APIKeyExternalID,
		},
		SDKFixtures: input.SDKFixtures,
	}
	cfg.Database.AutoMigrate = input.Database.AutoMigrate.valueOr(defaultDatabaseAutoMigrate(cfg.Env))
	cfg.Webhook.WorkerEnabled = input.Webhook.WorkerEnabled.valueOr(cfg.Webhook.EndpointURL != "" && cfg.Webhook.SigningKey != "")
	if input.Bootstrap.SeedAPIKeys.set {
		cfg.Bootstrap.SeedAPIKeys = input.Bootstrap.SeedAPIKeys.value
	} else {
		setDefaultSeedAPIKeys(&cfg)
	}
	return cfg
}
