package devinit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBaseConfigRequiresRunInput(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(minimalBaseConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBaseConfigWithImages(root, imagesConfig{}); err == nil {
		t.Fatal("expected missing .run/config.yaml error despite legacy config")
	}
}

func TestLoadBaseConfigResolvesPathsFromRun(t *testing.T) {
	root := t.TempDir()
	path := baseConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := minimalBaseConfig + "\ncode_session:\n  brain_claim_token_file: ./generated/secrets/brain-claim-token\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", "/original/config.yaml")
	cfg, err := loadBaseConfigWithImages(root, imagesConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".run", "generated", "secrets", "brain-claim-token"); cfg.CodeSession.BrainClaimTokenFile != want {
		t.Errorf("BrainClaimTokenFile = %q, want %q", cfg.CodeSession.BrainClaimTokenFile, want)
	}
	if os.Getenv("CONFIG_FILE") != "/original/config.yaml" {
		t.Fatal("base config loading changed CONFIG_FILE")
	}
}

func TestLocalSandboxTemplate(t *testing.T) {
	for _, template := range []string{"", "managed-agent-sandbox", "custom/sandbox:dev"} {
		want := defaultImagePrefix + "/managed-agent-sandbox:latest"
		if template == "custom/sandbox:dev" {
			want = template
		}
		if got := localSandboxTemplate(template); got != want {
			t.Errorf("localSandboxTemplate(%q) = %q, want %q", template, got, want)
		}
	}
}
