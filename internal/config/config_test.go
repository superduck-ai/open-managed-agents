package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/aiupstream"

	"go.yaml.in/yaml/v3"
)

const requiredConfigTestYAML = `
env: dev
server:
  addr: 127.0.0.1:38080
database:
  url: postgresql://test/database
redis:
  url: redis://test:6379
storage:
  type: s3
  s3:
    endpoint: http://test-storage:9000
    bucket: test-bucket
    region: us-east-1
    access_key_id: test-access-key
    secret_access_key: test-secret-key
`

const (
	dockerComposeTemplatePath = "deploy/docker-compose/oma-server.yaml"
	dockerComposeLocalPath    = "deploy/docker-compose/oma-server.local.yaml"
	dockerComposeConfigTarget = "/etc/open-managed-agents/config.yaml"
)

type dockerComposeTestFile struct {
	Services struct {
		OMAServer struct {
			Ports   []string                  `yaml:"ports"`
			Volumes []dockerComposeTestVolume `yaml:"volumes"`
		} `yaml:"oma-server"`
	} `yaml:"services"`
}

type dockerComposeTestVolume struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
	Bind     struct {
		CreateHostPath *bool `yaml:"create_host_path"`
	} `yaml:"bind"`
}

func TestLoadYAMLConfigAndResolvePaths(t *testing.T) {
	prepareLoadTest(t)
	root := t.TempDir()
	nested := filepath.Join(root, "cmd", "server")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("create nested working directory: %v", err)
	}
	writeConfigTestContents(t, filepath.Join(root, "go.mod"), "module example.com/config-test\n")
	writeConfigTestContents(t, filepath.Join(root, "config", "config.yaml"), `
env: prod
server:
  addr: 127.0.0.1:38080
database:
  url: postgresql://yaml/database
redis:
  url: redis://yaml:6379
storage:
  type: s3
  s3:
    endpoint: http://yaml-storage:9000
    bucket: yaml-bucket
    region: us-east-1
    access_key_id: yaml-access-key
    secret_access_key: yaml-secret-key
    force_path_style: false
batch:
  worker_concurrency: 7
  upstream_timeout: 45s
code_session:
  otlp_log_root: runtime/otlp
  jwt_signing_private_key_file: ${CONFIG_TEST_HOME}/jwt.pem
webhook:
  endpoint_url: https://example.com/webhooks
  signing_key: yaml-signing-key
  event_types:
    - session.created
bootstrap:
  workspace_name: yaml-workspace
`)
	t.Setenv("CONFIG_TEST_HOME", filepath.Join(root, "home"))
	t.Chdir(nested)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != "prod" || cfg.Server.Addr != "127.0.0.1:38080" {
		t.Fatalf("unexpected app/server config: env=%q addr=%q", cfg.Env, cfg.Server.Addr)
	}
	if cfg.Database.URL != "postgresql://yaml/database" || cfg.Database.AutoMigrate {
		t.Fatalf("unexpected database config: url=%q auto_migrate=%t", cfg.Database.URL, cfg.Database.AutoMigrate)
	}
	if cfg.Storage.Type != StorageTypeS3 || cfg.Storage.S3.ForcePathStyle {
		t.Fatalf("unexpected storage config: type=%q force_path_style=%t", cfg.Storage.Type, cfg.Storage.S3.ForcePathStyle)
	}
	if cfg.Batch.WorkerConcurrency != 7 || cfg.Batch.UpstreamTimeout != 45*time.Second {
		t.Fatalf("unexpected batch config: concurrency=%d timeout=%s", cfg.Batch.WorkerConcurrency, cfg.Batch.UpstreamTimeout)
	}
	if cfg.CodeSession.OTLPFileLogEnabled {
		t.Fatal("CodeSession.OTLPFileLogEnabled = true, want production default false")
	}
	if cfg.CodeSession.OTLPLogRoot != filepath.Join(root, "config", "runtime", "otlp") {
		t.Fatalf("CodeSession.OTLPLogRoot = %q, want config-relative path", cfg.CodeSession.OTLPLogRoot)
	}
	if cfg.CodeSession.JWTSigningPrivateKeyFile != filepath.Join(root, "home", "jwt.pem") {
		t.Fatalf("CodeSession.JWTSigningPrivateKeyFile = %q, want expanded path", cfg.CodeSession.JWTSigningPrivateKeyFile)
	}
	if !cfg.Webhook.WorkerEnabled {
		t.Fatal("Webhook.WorkerEnabled = false, want derived true")
	}
	if cfg.Bootstrap.WorkspaceName != "yaml-workspace" {
		t.Fatalf("Bootstrap.WorkspaceName = %q, want yaml-workspace", cfg.Bootstrap.WorkspaceName)
	}
}

func TestLoadIgnoresBusinessEnvironmentVariables(t *testing.T) {
	prepareLoadTest(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigTestYAML(t, configPath, `
env: prod
database:
  url: postgresql://yaml/database
  auto_migrate: false
code_session:
  otlp_file_log_enabled: false
webhook:
  endpoint_url: https://example.com/webhooks
  signing_key: yaml-signing-key
  worker_enabled: true
`)
	t.Setenv("CONFIG_FILE", configPath)
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DATABASE_URL", "postgresql://environment/database")
	t.Setenv("DB_AUTO_MIGRATE", "true")
	t.Setenv("CODE_SESSION_OTLP_FILE_LOG_ENABLED", "true")
	t.Setenv("WEBHOOK_WORKER_ENABLED", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Env != "prod" || cfg.Database.URL != "postgresql://yaml/database" {
		t.Fatalf("environment changed YAML: env=%q database=%q", cfg.Env, cfg.Database.URL)
	}
	if cfg.Database.AutoMigrate || cfg.CodeSession.OTLPFileLogEnabled || !cfg.Webhook.WorkerEnabled {
		t.Fatalf("environment changed YAML booleans: auto_migrate=%t otlp=%t webhook=%t", cfg.Database.AutoMigrate, cfg.CodeSession.OTLPFileLogEnabled, cfg.Webhook.WorkerEnabled)
	}
}

func TestLoadYAMLExplicitDynamicDefaults(t *testing.T) {
	prepareLoadTest(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigTestYAML(t, configPath, `
env: prod
database:
  auto_migrate: true
code_session:
  otlp_file_log_enabled: true
webhook:
  endpoint_url: https://example.com/webhooks
  signing_key: yaml-signing-key
  worker_enabled: false
`)
	t.Setenv("CONFIG_FILE", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Database.AutoMigrate || !cfg.CodeSession.OTLPFileLogEnabled || cfg.Webhook.WorkerEnabled {
		t.Fatalf("explicit YAML values were not preserved: auto_migrate=%t otlp=%t webhook=%t", cfg.Database.AutoMigrate, cfg.CodeSession.OTLPFileLogEnabled, cfg.Webhook.WorkerEnabled)
	}
}

func TestLoadYAMLRejectsUnknownField(t *testing.T) {
	testCases := []struct {
		name      string
		overrides string
		wantField string
	}{
		{name: "regular field", overrides: "database:\n  urll: postgresql://typo/database\n", wantField: "urll"},
		{name: "optional list item field", overrides: "bootstrap:\n  seed_api_keys:\n    - external_idd: typo\n      key: secret\n", wantField: "external_idd"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			prepareLoadTest(t)
			_, err := loadConfigTestYAML(t, testCase.overrides)
			if err == nil || !strings.Contains(err.Error(), "field "+testCase.wantField+" not found") {
				t.Fatalf("Load() error = %v, want strict unknown-field error", err)
			}
		})
	}
}

func TestLoadYAMLRequiresDeploymentFields(t *testing.T) {
	testCases := []struct {
		name      string
		overrides string
		wantError string
	}{
		{name: "environment", overrides: `env: ""`, wantError: "env is required"},
		{name: "server address", overrides: "server:\n  addr: \"\"", wantError: "server.addr is required"},
		{name: "database URL", overrides: "database:\n  url: \"\"", wantError: "database.url is required"},
		{name: "Redis URL", overrides: "redis:\n  url: \"\"", wantError: "redis.url is required"},
		{name: "storage type", overrides: "storage:\n  type: \"\"", wantError: "storage.type is required"},
		{name: "S3 endpoint", overrides: "storage:\n  s3:\n    endpoint: \"\"", wantError: "storage.s3.endpoint is required"},
		{name: "S3 bucket", overrides: "storage:\n  s3:\n    bucket: \"\"", wantError: "storage.s3.bucket is required"},
		{name: "S3 region", overrides: "storage:\n  s3:\n    region: \"\"", wantError: "storage.s3.region is required"},
		{name: "S3 access key", overrides: "storage:\n  s3:\n    access_key_id: \"\"", wantError: "storage.s3.access_key_id and storage.s3.secret_access_key are required"},
		{name: "S3 secret key", overrides: "storage:\n  s3:\n    secret_access_key: \"\"", wantError: "storage.s3.access_key_id and storage.s3.secret_access_key are required"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			prepareLoadTest(t)
			_, err := loadConfigTestYAML(t, testCase.overrides)
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.wantError)
			}
		})
	}
}

func TestLoadYAMLRejectsUnsupportedStorageType(t *testing.T) {
	prepareLoadTest(t)
	_, err := loadConfigTestYAML(t, "storage:\n  type: filesystem\n")
	if err == nil || !strings.Contains(err.Error(), `storage.type must be "s3"`) {
		t.Fatalf("Load() error = %v, want unsupported object storage type error", err)
	}
}

func TestLoadYAMLRejectsMultipleDocuments(t *testing.T) {
	prepareLoadTest(t)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigTestContents(t, configPath, "env: dev\n---\nenv: prod\n")
	t.Setenv("CONFIG_FILE", configPath)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("Load() error = %v, want multiple-document error", err)
	}
}

func TestLoadYAMLRejectsNullAndNonPositiveValues(t *testing.T) {
	nullCases := []struct {
		name      string
		overrides string
		wantPath  string
	}{
		{name: "optional boolean", overrides: "database:\n  auto_migrate: null\n", wantPath: "database.auto_migrate"},
		{name: "optional list", overrides: "bootstrap:\n  seed_api_keys: null\n", wantPath: "bootstrap.seed_api_keys"},
		{name: "optional list item", overrides: "bootstrap:\n  seed_api_keys:\n    - null\n", wantPath: "bootstrap.seed_api_keys.[0]"},
	}
	for _, testCase := range nullCases {
		t.Run(testCase.name, func(t *testing.T) {
			prepareLoadTest(t)
			_, err := loadConfigTestYAML(t, testCase.overrides)
			if err == nil || !strings.Contains(err.Error(), testCase.wantPath+" must not be null") {
				t.Fatalf("Load() error = %v, want null-value error", err)
			}
		})
	}

	t.Run("non-positive", func(t *testing.T) {
		prepareLoadTest(t)
		_, err := loadConfigTestYAML(t, "batch:\n  worker_concurrency: 0\n")
		if err == nil || !strings.Contains(err.Error(), "batch.worker_concurrency must be greater than zero") {
			t.Fatalf("Load() error = %v, want positive-value error", err)
		}
	})
}

func TestLoadYAMLSeedAPIKeyPresence(t *testing.T) {
	t.Run("omitted uses derived defaults", func(t *testing.T) {
		prepareLoadTest(t)
		cfg, err := loadConfigTestYAML(t, "")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(cfg.Bootstrap.SeedAPIKeys) != 2 {
			t.Fatalf("Bootstrap.SeedAPIKeys = %#v, want two derived defaults", cfg.Bootstrap.SeedAPIKeys)
		}
	})

	t.Run("explicit empty list stays empty", func(t *testing.T) {
		prepareLoadTest(t)
		cfg, err := loadConfigTestYAML(t, "bootstrap:\n  seed_api_keys: []\n")
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Bootstrap.SeedAPIKeys == nil || len(cfg.Bootstrap.SeedAPIKeys) != 0 {
			t.Fatalf("Bootstrap.SeedAPIKeys = %#v, want explicit empty list", cfg.Bootstrap.SeedAPIKeys)
		}
	})
}

func TestExpandConfiguredPathHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve home directory: %v", err)
	}
	resolved, err := expandConfiguredPath("~", "test.path")
	if err != nil {
		t.Fatalf("expandConfiguredPath() error = %v", err)
	}
	if resolved != home {
		t.Fatalf("expandConfiguredPath(~) = %q, want %q", resolved, home)
	}
}

func TestConfigExampleIsMinimalAndRunnable(t *testing.T) {
	configPath, err := filepath.Abs(filepath.Join("..", "..", "config", "config.example.yaml"))
	if err != nil {
		t.Fatalf("resolve config example path: %v", err)
	}
	validateConfigTestFile(t, configPath)
}

func TestDockerComposeConfigIsValid(t *testing.T) {
	configPath, err := filepath.Abs(filepath.Join("..", "..", dockerComposeTemplatePath))
	if err != nil {
		t.Fatalf("resolve Docker Compose config path: %v", err)
	}
	validateConfigTestFile(t, configPath)
}

func TestDockerComposeTemplateUsesPrivateGatewayPlaceholder(t *testing.T) {
	configPath, err := filepath.Abs(filepath.Join("..", "..", dockerComposeTemplatePath))
	if err != nil {
		t.Fatalf("resolve Docker Compose config path: %v", err)
	}
	cfg := loadValidatedConfigTestFile(t, configPath)
	if _, err := aiupstream.Endpoint(cfg.AnthropicUpstream.BaseURL, "v1/models", ""); err != nil {
		t.Fatalf("Compose anthropic_upstream.base_url must be a valid private gateway placeholder: %v", err)
	}
}

func TestDockerComposeKeepsSecretsOutOfTrackedTemplate(t *testing.T) {
	configPath, err := filepath.Abs(filepath.Join("..", "..", dockerComposeTemplatePath))
	if err != nil {
		t.Fatalf("resolve Docker Compose config path: %v", err)
	}
	cfg := loadValidatedConfigTestFile(t, configPath)
	secretValues := map[string]string{
		"anthropic_upstream.api_key": cfg.AnthropicUpstream.APIKey,
		"e2b.api_key":                cfg.E2B.APIKey,
		"e2b.access_token":           cfg.E2B.AccessToken,
		"webhook.signing_key":        cfg.Webhook.SigningKey,
	}
	for name, value := range secretValues {
		if strings.TrimSpace(value) != "" {
			t.Fatalf("tracked Compose template %s must be empty", name)
		}
	}

	compose := loadDockerComposeTestFile(t)
	mount := findDockerComposeConfigMount(t, compose.Services.OMAServer.Volumes)
	if mount.Source == "./"+dockerComposeTemplatePath {
		t.Fatalf("Compose must not mount tracked config template %q", mount.Source)
	}
	if mount.Source != "./"+dockerComposeLocalPath {
		t.Fatalf("Compose config source = %q, want gitignored local config", mount.Source)
	}
	if mount.Type != "bind" || !mount.ReadOnly {
		t.Fatalf("Compose config mount type/read_only = %q/%t, want bind/true", mount.Type, mount.ReadOnly)
	}
	if mount.Bind.CreateHostPath == nil || *mount.Bind.CreateHostPath {
		t.Fatal("Compose config bind.create_host_path must be explicitly false")
	}

	ignorePath, err := filepath.Abs(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatalf("resolve .gitignore path: %v", err)
	}
	ignoreData, err := os.ReadFile(ignorePath)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	wantIgnore := "/" + dockerComposeLocalPath
	ignored := false
	for line := range strings.SplitSeq(string(ignoreData), "\n") {
		if strings.TrimSpace(line) == wantIgnore {
			ignored = true
			break
		}
	}
	if !ignored {
		t.Fatalf(".gitignore must contain %q", wantIgnore)
	}
}

func TestDockerComposeDeclaresDevelopmentCredentialPosture(t *testing.T) {
	configPath, err := filepath.Abs(filepath.Join("..", "..", dockerComposeTemplatePath))
	if err != nil {
		t.Fatalf("resolve Docker Compose config path: %v", err)
	}
	cfg := loadValidatedConfigTestFile(t, configPath)
	if cfg.Env != EnvironmentDev {
		t.Fatalf("Compose env = %q, want explicit local-development posture %q", cfg.Env, EnvironmentDev)
	}
	if !cfg.Database.AutoMigrate {
		t.Fatal("Compose database.auto_migrate = false, want explicit local-development value true")
	}
	if strings.TrimSpace(cfg.CodeSession.JWTSigningPrivateKeyFile) != "" {
		t.Fatalf("Compose development jwt_signing_private_key_file = %q, want process-local ephemeral key", cfg.CodeSession.JWTSigningPrivateKeyFile)
	}
}

func TestDockerComposeSandboxCallbackUsesPublishedAPIPort(t *testing.T) {
	configPath, err := filepath.Abs(filepath.Join("..", "..", dockerComposeTemplatePath))
	if err != nil {
		t.Fatalf("resolve Docker Compose config path: %v", err)
	}
	cfg := loadValidatedConfigTestFile(t, configPath)
	if cfg.Server.Addr != ":8080" {
		t.Fatalf("Compose server.addr = %q, want container port :8080", cfg.Server.Addr)
	}
	if cfg.CodeSession.SandboxAPIBaseURL != "http://host.docker.internal:38080" {
		t.Fatalf("Compose code_session.sandbox_api_base_url = %q, want published host port 38080", cfg.CodeSession.SandboxAPIBaseURL)
	}

	compose := loadDockerComposeTestFile(t)
	if !slices.Contains(compose.Services.OMAServer.Ports, "38080:8080") {
		t.Fatalf("Compose oma-server ports = %v, want 38080:8080 callback mapping", compose.Services.OMAServer.Ports)
	}
}

func TestLoadRequiresYAMLAndDoesNotReadDotEnv(t *testing.T) {
	prepareLoadTest(t)
	directory := t.TempDir()
	writeConfigTestContents(t, filepath.Join(directory, ".env"), "APP_ENV=production\n")
	t.Chdir(directory)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), defaultConfigFilePath+" is required") {
		t.Fatalf("Load() error = %v, want required YAML error", err)
	}
}

func TestLoadExplicitMissingConfigFile(t *testing.T) {
	prepareLoadTest(t)
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	t.Setenv("CONFIG_FILE", missing)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), missing) {
		t.Fatalf("Load() error = %v, want missing CONFIG_FILE error", err)
	}
}

func TestLoadStorageS3ForcePathStyleDefault(t *testing.T) {
	prepareLoadTest(t)

	cfg, err := loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.Storage.S3.ForcePathStyle {
		t.Fatal("Storage.S3.ForcePathStyle = false, want true")
	}
}

func TestLoadDatabaseAutoMigrateDefaultDevelopment(t *testing.T) {
	prepareLoadTest(t)

	cfg, err := loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Env != "dev" {
		t.Fatalf("Env = %q, want development", cfg.Env)
	}
	if !cfg.Database.AutoMigrate {
		t.Fatal("DatabaseAutoMigrate = false, want true")
	}
}

func TestLoadYAMLRejectsUnsupportedEnvironment(t *testing.T) {
	prepareLoadTest(t)
	_, err := loadConfigTestYAML(t, "env: production\n")
	if err == nil || !strings.Contains(err.Error(), `env must be "dev" or "prod"`) {
		t.Fatalf("Load() error = %v, want unsupported env error", err)
	}
}

func TestLoadDatabaseAutoMigrateProdDefault(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, "env: prod\n")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Database.AutoMigrate {
		t.Fatal("DatabaseAutoMigrate = true, want false")
	}
}

func TestLoadDatabaseAutoMigrateOverride(t *testing.T) {
	t.Run("production enabled explicitly", func(t *testing.T) {
		prepareLoadTest(t)
		cfg, err := loadConfigTestYAML(t, "env: prod\ndatabase:\n  auto_migrate: true\n")
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.Database.AutoMigrate {
			t.Fatal("DatabaseAutoMigrate = false, want true")
		}
	})

	t.Run("development disabled explicitly", func(t *testing.T) {
		prepareLoadTest(t)
		cfg, err := loadConfigTestYAML(t, "env: dev\ndatabase:\n  auto_migrate: false\n")
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.Database.AutoMigrate {
			t.Fatal("DatabaseAutoMigrate = true, want false")
		}
	})
}

func TestLoadCodeSessionOTLPFileLogDefaults(t *testing.T) {
	t.Run("development enabled", func(t *testing.T) {
		prepareLoadTest(t)

		cfg, err := loadConfigTestYAML(t, "")
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.CodeSession.OTLPFileLogEnabled {
			t.Fatal("CodeSessionOTLPFileLogEnabled = false, want true")
		}
		wantLogRoot := filepath.Join(filepath.Dir(os.Getenv(configFileEnv)), "logs")
		if cfg.CodeSession.OTLPLogRoot != wantLogRoot {
			t.Fatalf("CodeSessionOTLPLogRoot = %q, want %q", cfg.CodeSession.OTLPLogRoot, wantLogRoot)
		}
		if cfg.CodeSession.OTLPLogBodyPreviewBytes != 256*1024 {
			t.Fatalf("CodeSessionOTLPLogBodyPreviewBytes = %d, want %d", cfg.CodeSession.OTLPLogBodyPreviewBytes, 256*1024)
		}
	})

	t.Run("production disabled", func(t *testing.T) {
		prepareLoadTest(t)
		cfg, err := loadConfigTestYAML(t, "env: prod\n")
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.CodeSession.OTLPFileLogEnabled {
			t.Fatal("CodeSessionOTLPFileLogEnabled = true, want false")
		}
	})
}

func TestLoadCodeSessionOTLPFileLogOverrides(t *testing.T) {
	prepareLoadTest(t)
	cfg, err := loadConfigTestYAML(t, `
env: prod
code_session:
  otlp_file_log_enabled: true
  otlp_log_root: /tmp/custom-otlp
  otlp_log_body_preview_bytes: 1024
`)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.CodeSession.OTLPFileLogEnabled {
		t.Fatal("CodeSessionOTLPFileLogEnabled = false, want true")
	}
	if cfg.CodeSession.OTLPLogRoot != "/tmp/custom-otlp" {
		t.Fatalf("CodeSessionOTLPLogRoot = %q, want /tmp/custom-otlp", cfg.CodeSession.OTLPLogRoot)
	}
	if cfg.CodeSession.OTLPLogBodyPreviewBytes != 1024 {
		t.Fatalf("CodeSessionOTLPLogBodyPreviewBytes = %d, want 1024", cfg.CodeSession.OTLPLogBodyPreviewBytes)
	}
}

func TestLoadCodeSessionUpstreamProxySSRFProtectionOverride(t *testing.T) {
	prepareLoadTest(t)

	// 默认必须保持保护开启，避免新部署在未配置时意外获得私网访问能力。
	cfg, err := loadConfigTestYAML(t, "")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.CodeSession.UpstreamProxyDisableSSRFProtection {
		t.Fatal("CodeSessionUpstreamProxyDisableSSRFProtection = true, want false by default")
	}

	// 只有配置文件显式设置危险开关时才允许本地 fake-IP/TUN 排障模式。
	cfg, err = loadConfigTestYAML(t, "code_session:\n  upstream_proxy_disable_ssrf_protection: true\n")
	if err != nil {
		t.Fatalf("load config with SSRF override: %v", err)
	}
	if !cfg.CodeSession.UpstreamProxyDisableSSRFProtection {
		t.Fatal("CodeSessionUpstreamProxyDisableSSRFProtection = false, want true")
	}
}

func TestLoadCodeSessionUpstreamProxyMITMRejectsInvalidCAKey(t *testing.T) {
	t.Run("MITM enabled without private key", func(t *testing.T) {
		prepareLoadTest(t)
		if _, err := loadConfigTestYAML(t, "code_session:\n  upstream_proxy_mitm_enabled: true\n"); err == nil {
			t.Fatal("Load() error = nil, want missing stable CA private key error")
		}
	})

	t.Run("private key does not exist", func(t *testing.T) {
		prepareLoadTest(t)
		directory := t.TempDir()
		contents := fmt.Sprintf("code_session:\n  upstream_proxy_mitm_enabled: true\n  upstream_proxy_ca_key_file: %q\n", filepath.Join(directory, "missing-key.pem"))
		if _, err := loadConfigTestYAML(t, contents); err == nil {
			t.Fatal("Load() error = nil, want missing stable CA private key file error")
		}
	})

	t.Run("private key is not a regular file", func(t *testing.T) {
		prepareLoadTest(t)
		directory := t.TempDir()
		contents := fmt.Sprintf("code_session:\n  upstream_proxy_mitm_enabled: true\n  upstream_proxy_ca_key_file: %q\n", directory)
		if _, err := loadConfigTestYAML(t, contents); err == nil {
			t.Fatal("Load() error = nil, want non-regular stable CA private key error")
		}
	})
}

func TestLoadCodeSessionUpstreamProxyMITMConfiguration(t *testing.T) {
	t.Run("MITM disabled ignores dormant private key path", func(t *testing.T) {
		prepareLoadTest(t)
		missingKeyFile := filepath.Join(t.TempDir(), "missing-key.pem")
		cfg, err := loadConfigTestYAML(t, fmt.Sprintf("code_session:\n  upstream_proxy_ca_key_file: %q\n", missingKeyFile))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.CodeSession.UpstreamProxyMITMEnabled || cfg.CodeSession.UpstreamProxyCAKeyFile != missingKeyFile {
			t.Fatalf("unexpected dormant MITM config: enabled=%t key=%q", cfg.CodeSession.UpstreamProxyMITMEnabled, cfg.CodeSession.UpstreamProxyCAKeyFile)
		}
	})

	t.Run("stable private key exists", func(t *testing.T) {
		prepareLoadTest(t)
		keyFile := writeConfigTestFile(t, filepath.Join(t.TempDir(), "ca-key.pem"))
		contents := fmt.Sprintf("code_session:\n  upstream_proxy_mitm_enabled: true\n  upstream_proxy_ca_key_file: %q\n", keyFile)
		cfg, err := loadConfigTestYAML(t, contents)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !cfg.CodeSession.UpstreamProxyMITMEnabled || cfg.CodeSession.UpstreamProxyCAKeyFile != keyFile {
			t.Fatalf("unexpected MITM config: enabled=%t key=%q", cfg.CodeSession.UpstreamProxyMITMEnabled, cfg.CodeSession.UpstreamProxyCAKeyFile)
		}
	})
}

func writeConfigTestFile(t *testing.T, path string) string {
	t.Helper()
	writeConfigTestContents(t, path, "fixture")
	return path
}

func loadConfigTestYAML(t *testing.T, contents string) (Config, error) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigTestYAML(t, configPath, contents)
	t.Setenv(configFileEnv, configPath)
	return Load()
}

func writeConfigTestYAML(t *testing.T, path, overrides string) {
	t.Helper()
	base := decodeConfigTestYAML(t, requiredConfigTestYAML)
	mergeConfigTestMappings(base, decodeConfigTestYAML(t, overrides))
	contents, err := yaml.Marshal(base)
	if err != nil {
		t.Fatalf("encode config test fixture: %v", err)
	}
	writeConfigTestContents(t, path, string(contents))
}

func decodeConfigTestYAML(t *testing.T, contents string) map[string]any {
	t.Helper()
	decoded := map[string]any{}
	if strings.TrimSpace(contents) == "" {
		return decoded
	}
	if err := yaml.Unmarshal([]byte(contents), &decoded); err != nil {
		t.Fatalf("decode config test fixture: %v", err)
	}
	return decoded
}

func mergeConfigTestMappings(base, overrides map[string]any) {
	for key, override := range overrides {
		baseMapping, baseIsMapping := base[key].(map[string]any)
		overrideMapping, overrideIsMapping := override.(map[string]any)
		if baseIsMapping && overrideIsMapping {
			mergeConfigTestMappings(baseMapping, overrideMapping)
			continue
		}
		base[key] = override
	}
}

func loadDockerComposeTestFile(t *testing.T) dockerComposeTestFile {
	t.Helper()
	composePath, err := filepath.Abs(filepath.Join("..", "..", "docker-compose.yml"))
	if err != nil {
		t.Fatalf("resolve Docker Compose file path: %v", err)
	}
	data, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatalf("read Docker Compose file: %v", err)
	}
	var compose dockerComposeTestFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		t.Fatalf("decode Docker Compose file: %v", err)
	}
	return compose
}

func findDockerComposeConfigMount(t *testing.T, volumes []dockerComposeTestVolume) dockerComposeTestVolume {
	t.Helper()
	matches := make([]dockerComposeTestVolume, 0, 1)
	for _, volume := range volumes {
		if volume.Target == dockerComposeConfigTarget {
			matches = append(matches, volume)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("Compose config mounts targeting %q = %d, want exactly one", dockerComposeConfigTarget, len(matches))
	}
	return matches[0]
}

func validateConfigTestFile(t *testing.T, configPath string) {
	t.Helper()
	loadValidatedConfigTestFile(t, configPath)
}

func loadValidatedConfigTestFile(t *testing.T, configPath string) Config {
	t.Helper()
	cfg, err := loadYAMLConfig(configPath)
	if err != nil {
		t.Fatalf("load config %q: %v", configPath, err)
	}
	if err := resolveConfigPaths(&cfg, filepath.Dir(configPath)); err != nil {
		t.Fatalf("resolve config paths for %q: %v", configPath, err)
	}
	if err := validate(cfg); err != nil {
		t.Fatalf("validate config %q: %v", configPath, err)
	}
	return cfg
}

func writeConfigTestContents(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create config fixture directory %q: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config fixture %q: %v", path, err)
	}
}

func prepareLoadTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	unsetEnv(t, configFileEnv)
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	oldValue, hadValue := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
