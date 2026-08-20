package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
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
	Storage           StorageConfig           `yaml:"storage"`
	AnthropicUpstream AnthropicUpstreamConfig `yaml:"anthropic_upstream"`
	WebSearch         WebSearchConfig         `yaml:"web_search"`
	Batch             BatchConfig             `yaml:"batch"`
	E2B               E2BConfig               `yaml:"e2b"`
	EnvironmentRunner EnvironmentRunnerConfig `yaml:"environment_runner"`
	CodeSession       yamlCodeSessionConfig   `yaml:"code_session"`
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
	SandboxAPIBaseURL                  string         `yaml:"sandbox_api_base_url"`
	OTLPFileLogEnabled                 optional[bool] `yaml:"otlp_file_log_enabled"`
	OTLPLogRoot                        string         `yaml:"otlp_log_root"`
	OTLPLogBodyPreviewBytes            int            `yaml:"otlp_log_body_preview_bytes"`
	JWTSigningPrivateKeyFile           string         `yaml:"jwt_signing_private_key_file"`
	UpstreamProxyMITMEnabled           bool           `yaml:"upstream_proxy_mitm_enabled"`
	UpstreamProxyCAKeyFile             string         `yaml:"upstream_proxy_ca_key_file"`
	UpstreamProxyDisableSSRFProtection bool           `yaml:"upstream_proxy_disable_ssrf_protection"`
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
		Storage:           defaults.Storage,
		AnthropicUpstream: defaults.AnthropicUpstream,
		WebSearch:         defaults.WebSearch,
		Batch:             defaults.Batch,
		E2B:               defaults.E2B,
		EnvironmentRunner: defaults.EnvironmentRunner,
		CodeSession: yamlCodeSessionConfig{
			SandboxAPIBaseURL:                  defaults.CodeSession.SandboxAPIBaseURL,
			OTLPLogRoot:                        defaults.CodeSession.OTLPLogRoot,
			OTLPLogBodyPreviewBytes:            defaults.CodeSession.OTLPLogBodyPreviewBytes,
			JWTSigningPrivateKeyFile:           defaults.CodeSession.JWTSigningPrivateKeyFile,
			UpstreamProxyMITMEnabled:           defaults.CodeSession.UpstreamProxyMITMEnabled,
			UpstreamProxyCAKeyFile:             defaults.CodeSession.UpstreamProxyCAKeyFile,
			UpstreamProxyDisableSSRFProtection: defaults.CodeSession.UpstreamProxyDisableSSRFProtection,
		},
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
		Storage:           input.Storage,
		AnthropicUpstream: input.AnthropicUpstream,
		WebSearch:         input.WebSearch,
		Batch:             input.Batch,
		E2B:               input.E2B,
		EnvironmentRunner: input.EnvironmentRunner,
		CodeSession: CodeSessionConfig{
			SandboxAPIBaseURL:                  input.CodeSession.SandboxAPIBaseURL,
			OTLPLogRoot:                        input.CodeSession.OTLPLogRoot,
			OTLPLogBodyPreviewBytes:            input.CodeSession.OTLPLogBodyPreviewBytes,
			JWTSigningPrivateKeyFile:           input.CodeSession.JWTSigningPrivateKeyFile,
			UpstreamProxyMITMEnabled:           input.CodeSession.UpstreamProxyMITMEnabled,
			UpstreamProxyCAKeyFile:             input.CodeSession.UpstreamProxyCAKeyFile,
			UpstreamProxyDisableSSRFProtection: input.CodeSession.UpstreamProxyDisableSSRFProtection,
		},
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
	cfg.CodeSession.OTLPFileLogEnabled = input.CodeSession.OTLPFileLogEnabled.valueOr(defaultCodeSessionOTLPFileLogEnabled(cfg.Env))
	cfg.Webhook.WorkerEnabled = input.Webhook.WorkerEnabled.valueOr(cfg.Webhook.EndpointURL != "" && cfg.Webhook.SigningKey != "")
	if input.Bootstrap.SeedAPIKeys.set {
		cfg.Bootstrap.SeedAPIKeys = input.Bootstrap.SeedAPIKeys.value
	} else {
		setDefaultSeedAPIKeys(&cfg)
	}
	return cfg
}

// UnmarshalYAML 把 provider 名归一化为小写，使配置键与 web_search.provider 的匹配不
// 依赖书写大小写。默认配置已预置小写键，而 YAML 的 map 解码是合并而非替换，因此不归一化
// 时用户写成 Brave 会同时留下空的 brave 与真实的 Brave 两个条目，读取侧只能靠 map 遍历
// 顺序随机命中其中一个，表现为 web search 时开时关。
func (cfg *WebSearchConfig) UnmarshalYAML(node *yaml.Node) error {
	input := struct {
		Provider                string        `yaml:"provider"`
		Timeout                 time.Duration `yaml:"timeout"`
		MaxServerToolIterations int           `yaml:"max_server_tool_iterations"`
		// Providers 故意不预置默认值：先单独解析调用方写下的键，才能把大小写重复和
		// 默认条目区分开。
		Providers map[string]WebSearchProviderConfig `yaml:"providers"`
	}{
		Provider:                cfg.Provider,
		Timeout:                 cfg.Timeout,
		MaxServerToolIterations: cfg.MaxServerToolIterations,
	}
	if err := node.Decode(&input); err != nil {
		return err
	}
	providers, err := normalizeWebSearchProviders(cfg.Providers, input.Providers)
	if err != nil {
		return err
	}
	*cfg = WebSearchConfig{
		Provider:                input.Provider,
		Timeout:                 input.Timeout,
		MaxServerToolIterations: input.MaxServerToolIterations,
		Providers:               providers,
	}
	return nil
}

func normalizeWebSearchProviders(defaults, configured map[string]WebSearchProviderConfig) (map[string]WebSearchProviderConfig, error) {
	providers := maps.Clone(defaults)
	if providers == nil {
		providers = make(map[string]WebSearchProviderConfig, len(configured))
	}
	seen := make(map[string]string, len(configured))
	for name, provider := range configured {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return nil, errors.New("web_search.providers name is required")
		}
		if previous, duplicate := seen[normalized]; duplicate {
			return nil, fmt.Errorf("web_search.providers %q and %q name the same provider", previous, name)
		}
		seen[normalized] = name
		providers[normalized] = provider
	}
	return providers, nil
}

func (cfg *WebSearchProviderConfig) UnmarshalYAML(node *yaml.Node) error {
	input := struct {
		Endpoint string         `yaml:"endpoint"`
		APIKey   string         `yaml:"api_key"`
		Options  map[string]any `yaml:"options"`
	}{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey,
	}
	if err := node.Decode(&input); err != nil {
		return err
	}
	options := make(map[string]json.RawMessage, len(input.Options))
	for key, value := range input.Options {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		options[key] = encoded
	}
	*cfg = WebSearchProviderConfig{
		Endpoint: input.Endpoint,
		APIKey:   input.APIKey,
		Options:  options,
	}
	return nil
}
