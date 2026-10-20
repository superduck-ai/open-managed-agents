package devinit

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.example.yaml", "dev-init.example.yaml", "model-providers.example.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "config", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "config", name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPrepareLocalRefusesMissingBoundInput(t *testing.T) {
	root := prepareTestRepo(t)
	run := filepath.Join(root, ".run")
	if err := os.MkdirAll(run, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "env.json"), []byte(`{"name":"existing","env_id":"existing-id"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareLocal(root, t.TempDir()); err == nil {
		t.Fatal("expected missing input error for bound environment")
	}
	if _, err := os.Stat(filepath.Join(run, "secrets")); !os.IsNotExist(err) {
		t.Fatal("bound environment must not receive replacement secrets")
	}
}

func TestPrepareLocalRejectsSymlinkInput(t *testing.T) {
	root := prepareTestRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".run"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(target, []byte("untouched"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, baseConfigPath(root)); err != nil {
		t.Fatal(err)
	}
	if err := prepareLocal(root, t.TempDir()); err == nil {
		t.Fatal("expected symlink input rejection")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatal("symlink target was changed")
	}
}

func TestPrepareLocalRejectsOrphanedRuntimeState(t *testing.T) {
	root := prepareTestRepo(t)
	dir := generatedDir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"env_id":"existing"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareLocal(root, t.TempDir()); err == nil {
		t.Fatal("expected orphaned environment state rejection")
	}
	if _, err := os.Stat(filepath.Join(root, ".run", "secrets")); !os.IsNotExist(err) {
		t.Fatal("orphaned state must not receive new secrets")
	}
}

func TestPrepareLocalDoesNotReplaceMissingKeyForExistingConfig(t *testing.T) {
	root := prepareTestRepo(t)
	if err := os.MkdirAll(filepath.Join(root, ".run"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(baseConfigPath(root), []byte("existing config"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareLocal(root, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".run", "secrets", "vault-kek")); !os.IsNotExist(err) {
		t.Fatal("existing configuration must not receive a replacement Vault key")
	}
}

func TestPrepareLocalCreatesBrainConfigAndStableKeys(t *testing.T) {
	root, shared := prepareTestRepo(t), filepath.Join(t.TempDir(), ".oma-dev")
	if err := prepareLocal(root, shared); err != nil {
		t.Fatal(err)
	}
	sharedFiles, err := os.ReadDir(shared)
	if err != nil || len(sharedFiles) != 1 || sharedFiles[0].Name() != "model-providers.yaml" {
		t.Fatal("shared directory must only contain model-providers.yaml")
	}
	cfg, err := loadBaseConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnvironmentRunner.AgentMode != "brain" || cfg.Vault.MasterKey.Local.Kek != "" {
		t.Fatal("fresh config must use Brain and a generated local KEK")
	}
	spec, err := LoadSpec(filepath.Join(root, ".run", "dev-init.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.ModelProviders != filepath.Join(shared, "model-providers.yaml") || spec.FixedSecretsDir != filepath.Join(root, ".run", "secrets") {
		t.Fatalf("unexpected prepared input paths: %+v", spec)
	}
	snapshot := map[string]string{}
	for _, path := range []string{
		baseConfigPath(root), filepath.Join(root, ".run", "dev-init.yaml"), spec.ModelProviders,
		filepath.Join(spec.FixedSecretsDir, "code-session-jwt-ed25519.pem"),
		filepath.Join(spec.FixedSecretsDir, "upstream-proxy-ca-key.pem"),
		filepath.Join(spec.FixedSecretsDir, "vault-kek"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		snapshot[path] = string(data)
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("private file permission: %s", path)
		}
	}
	for name, expectedType := range map[string]string{"code-session-jwt-ed25519.pem": "ed25519", "upstream-proxy-ca-key.pem": "ecdsa"} {
		block, _ := pem.Decode([]byte(snapshot[filepath.Join(spec.FixedSecretsDir, name)]))
		if block == nil {
			t.Fatalf("invalid PEM: %s", name)
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		switch key.(type) {
		case ed25519.PrivateKey:
			if expectedType != "ed25519" {
				t.Fatal("incorrect signing key algorithm")
			}
		case *ecdsa.PrivateKey:
			if expectedType != "ecdsa" {
				t.Fatal("incorrect CA key algorithm")
			}
		default:
			t.Fatal("unsupported key algorithm")
		}
	}
	kek, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(snapshot[filepath.Join(spec.FixedSecretsDir, "vault-kek")], "\n"))
	if err != nil || len(kek) != 32 {
		t.Fatal("expected a 32-byte local KEK")
	}
	if err := prepareLocal(root, shared); err != nil {
		t.Fatal(err)
	}
	for path, want := range snapshot {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("repeated preparation changed %s", path)
		}
	}
}

func TestPrepareLocalSharesProviderInputsAcrossWorktrees(t *testing.T) {
	shared := t.TempDir()
	providerFile := filepath.Join(shared, "model-providers.yaml")
	if err := os.WriteFile(providerFile, []byte("existing shared provider"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		root := prepareTestRepo(t)
		if err := prepareLocal(root, shared); err != nil {
			t.Fatal(err)
		}
		spec, err := LoadSpec(filepath.Join(root, ".run", "dev-init.yaml"))
		if err != nil || spec.ModelProviders != providerFile {
			t.Fatal("worktree does not reference the shared provider file")
		}
	}
	data, err := os.ReadFile(providerFile)
	if err != nil || string(data) != "existing shared provider" {
		t.Fatal("shared provider file was overwritten")
	}
}
