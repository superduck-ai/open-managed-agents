package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const minimalBaseConfig = `
env: dev
server:
  addr: 127.0.0.1:38080
database:
  url: postgresql://claude:123456@localhost:5432/claude_api?sslmode=disable
redis:
  url: redis://localhost:6379
nats:
  url: nats://localhost:4222
storage:
  type: s3
  s3:
    endpoint: http://localhost:9000
    bucket: claude-files
    region: us-east-1
    access_key_id: minioadmin
    secret_access_key: minioadmin
    force_path_style: true
vault:
  master_key:
    local:
      version: 1
      kek: "rFF/9K42dW9xtX3zvE3rm6HzJshGCVntS62F2884M78="
`

func TestGenerateConfigProducesLoadableEnvironmentConfig(t *testing.T) {
	repoRoot := t.TempDir()
	configDir := filepath.Join(repoRoot, ".run")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	baseConfigPath := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(baseConfigPath, []byte(minimalBaseConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", baseConfigPath)
	base, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	base.CodeSession.BrainClaimTokenFile = "/base/shared-brain-token"
	base.CodeSession.JWTSigningPrivateKeyFile = "/base/jwt.pem"
	base.CodeSession.UpstreamProxyCAKeyFile = "/base/ca.pem"
	binding := EnvBinding{
		Name: "feature-x", EnvID: "7d5adf3a-9d08-4179-9cc4-8721d3e83964",
		Worktree: repoRoot, InitFile: "/repo/dev-init.yaml",
	}
	layout, err := NewLayout(binding, repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	runtime := RuntimePorts{APIPort: 40080, WebPort: 61080, PGPort: 45480, RedisPort: 62080, NATSPort: 44280, NATSPort2: 44281, NATSPort3: 44282, MinIOPort: 49080, E2BPort: 43080}
	spec := Spec{
		Version:          1,
		FixedSecretsDir:  "/tmp",
		OrganizationName: "OMA Local",
		UserEmail:        "test@oma.ai",
		UserName:         "Local Test",
		UserRole:         "admin",
	}
	state := State{
		EnvID:        binding.EnvID,
		Name:         binding.Name,
		Phase:        phaseKeys,
		Organization: &OrganizationState{UUID: "11111111-1111-1111-1111-111111111111", Name: "OMA Local"},
		User:         &UserState{UUID: "22222222-2222-2222-2222-222222222222", ExternalID: "user_devinit", Email: "test@oma.ai", Name: "Local Test"},
		Workspaces: []WorkspaceState{
			{
				Ref: "default", UUID: "33333333-3333-3333-3333-333333333333", ExternalID: "wrkspc_default", Name: "default", IsDefault: true,
				APIKeys: []APIKeyState{{Ref: "key-1", UUID: "44444444-4444-4444-4444-444444444444", ExternalID: "apikey_default_1", Name: "default-key-1"}},
			},
			{
				Ref: "secondary", UUID: "55555555-5555-5555-5555-555555555555", ExternalID: "wrkspc_secondary", Name: "secondary",
				APIKeys: []APIKeyState{
					{Ref: "key-1", UUID: "66666666-6666-6666-6666-666666666666", ExternalID: "apikey_secondary_1", Name: "secondary-key-1"},
					{Ref: "key-2", UUID: "77777777-7777-7777-7777-777777777777", ExternalID: "apikey_secondary_2", Name: "secondary-key-2"},
				},
			},
		},
	}
	if err := GenerateConfig(baseConfigPath, layout, base, spec, state, runtime); err != nil {
		t.Fatal(err)
	}
	generated, err := os.ReadFile(layout.GeneratedConfig)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("CONFIG_FILE", layout.GeneratedConfig)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("generated config must load: %v\n%s", err, generated)
	}
	for name, path := range map[string]string{"brain-claim-token": cfg.CodeSession.BrainClaimTokenFile, "code-session-jwt-ed25519.pem": cfg.CodeSession.JWTSigningPrivateKeyFile, "upstream-proxy-ca-key.pem": cfg.CodeSession.UpstreamProxyCAKeyFile} {
		if path != filepath.Join(layout.SecretsDir, name) {
			t.Errorf("%s points outside the environment: %s", name, path)
		}
	}
	if cfg.Server.Addr != "127.0.0.1:40080" {
		t.Errorf("Server.Addr = %q", cfg.Server.Addr)
	}
	if !strings.Contains(cfg.NATS.URL, "nats://127.0.0.1:44280") || !strings.Contains(cfg.NATS.URL, "nats://127.0.0.1:44282") {
		t.Errorf("NATS.URL = %q, want three seeds", cfg.NATS.URL)
	}
	if cfg.NATS.WebhookStream.Replicas != 3 || cfg.NATS.WorkerEventStream.Replicas != 3 {
		t.Errorf("stream replicas = %d/%d, want 3/3 for the three-node cluster", cfg.NATS.WebhookStream.Replicas, cfg.NATS.WorkerEventStream.Replicas)
	}
	if !strings.Contains(cfg.Database.URL, "/claude_api?") {
		t.Errorf("Database.URL = %q", cfg.Database.URL)
	}
	if cfg.Redis.URL != "redis://localhost:62080" {
		t.Errorf("Redis.URL = %q", cfg.Redis.URL)
	}
	if cfg.Storage.S3.Bucket != "claude-files" {
		t.Errorf("Bucket = %q", cfg.Storage.S3.Bucket)
	}
	if cfg.E2B.APIURL != "http://127.0.0.1:43080" {
		t.Errorf("E2B.APIURL = %q", cfg.E2B.APIURL)
	}
	if cfg.Auth.ConsoleURL != "http://oma-feature-x-web.localhost" {
		t.Errorf("ConsoleURL = %q", cfg.Auth.ConsoleURL)
	}
	if cfg.Tunnel.PublicBaseURL != "http://127.0.0.1:40080" {
		t.Errorf("PublicBaseURL = %q", cfg.Tunnel.PublicBaseURL)
	}
	if cfg.Bootstrap.UserEmail != "test@oma.ai" || cfg.Bootstrap.OrganizationName != "OMA Local" {
		t.Errorf("Bootstrap = %+v", cfg.Bootstrap)
	}
	if len(cfg.Bootstrap.SeedAPIKeys) != 0 {
		t.Errorf("SeedAPIKeys = %+v", cfg.Bootstrap.SeedAPIKeys)
	}
	if !filepath.IsAbs(cfg.CodeSession.JWTSigningPrivateKeyFile) || !strings.Contains(cfg.CodeSession.JWTSigningPrivateKeyFile, "code-session-jwt-ed25519.pem") {
		t.Errorf("JWTSigningPrivateKeyFile = %q", cfg.CodeSession.JWTSigningPrivateKeyFile)
	}
	if cfg.CodeSession.BrainClaimTokenFile == "" || !filepath.IsAbs(cfg.CodeSession.BrainClaimTokenFile) {
		t.Errorf("BrainClaimTokenFile = %q", cfg.CodeSession.BrainClaimTokenFile)
	}
	state.Runtime = &runtime
	result, err := finalizeRun(t.Context(), preparedRun{repoRoot: repoRoot, binding: binding, layout: layout, base: base, spec: spec, state: state})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.Runtime.APIPort != runtime.APIPort || result.State.Runtime.WebPort != runtime.WebPort {
		t.Fatal("re-initialization lost listener ports")
	}
}
