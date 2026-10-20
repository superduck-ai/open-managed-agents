package devinit

import (
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFixedSecretsFailures(t *testing.T) {
	target := t.TempDir()
	if err := CopyFixedSecrets(filepath.Join(t.TempDir(), "missing"), target); err == nil {
		t.Fatal("CopyFixedSecrets() error = nil for missing source dir, want failure")
	}
}

func TestCopyFixedSecretsCopiesAndIsIdempotent(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	for _, name := range fixedSecretFiles {
		if err := os.WriteFile(filepath.Join(source, name), []byte("key-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CopyFixedSecrets(source, target); err != nil {
		t.Fatal(err)
	}
	for _, name := range fixedSecretFiles {
		info, err := os.Stat(filepath.Join(target, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s perm = %o, want 600", name, info.Mode().Perm())
		}
	}
	if err := CopyFixedSecrets(source, target); err != nil {
		t.Fatalf("second CopyFixedSecrets() error = %v, want idempotent success", err)
	}
}

func TestCopyFixedSecretsRefusesContentConflict(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	for _, name := range fixedSecretFiles {
		if err := os.WriteFile(filepath.Join(source, name), []byte("source-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, name), []byte("different"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := CopyFixedSecrets(source, target); err == nil {
		t.Fatal("CopyFixedSecrets() error = nil for content conflict, want failure")
	}
}

func TestNewRandomTokenAndAPIKey(t *testing.T) {
	token, err := NewRandomToken(brainClaimTokenBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 32 {
		t.Fatalf("token length = %d, want >= 32", len(token))
	}
	key, err := NewAPIKeyValue()
	if err != nil {
		t.Fatal(err)
	}
	if len(key) <= len("sk-ant-api03-") {
		t.Fatalf("api key %q too short", key)
	}
}

func TestStateAndCredentialsRoundTrip(t *testing.T) {
	layout := Layout{
		RunDir:          t.TempDir(),
		StateDir:        t.TempDir(),
		SecretsDir:      t.TempDir(),
		BindingFile:     filepath.Join(t.TempDir(), "env.json"),
		StateFile:       filepath.Join(t.TempDir(), "state.json"),
		CredentialsFile: filepath.Join(t.TempDir(), "credentials.json"),
	}
	binding := testBinding()
	if err := SaveBinding(layout, binding); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := LoadBinding(layout.BindingFile)
	if err != nil || !found {
		t.Fatalf("LoadBinding() = %+v, %v, %v", loaded, found, err)
	}
	if loaded.EnvID != binding.EnvID || loaded.Name != binding.Name {
		t.Fatalf("LoadBinding() = %+v, want %+v", loaded, binding)
	}
	state := State{EnvID: binding.EnvID, Name: binding.Name, Phase: phaseComplete}
	if err := SaveState(layout, state); err != nil {
		t.Fatal(err)
	}
	credentials := Credentials{EnvID: binding.EnvID, BrainClaimToken: "token"}
	if err := SaveCredentials(layout, credentials); err != nil {
		t.Fatal(err)
	}
	if _, found, err := LoadCredentials(layout.CredentialsFile); err != nil || !found {
		t.Fatalf("LoadCredentials() found = %v, err = %v", found, err)
	}
	info, err := os.Stat(layout.CredentialsFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("credentials perm = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadCredentialsRejectsIncompleteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"env_id":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := LoadCredentials(path); err == nil || found {
		t.Fatalf("LoadCredentials() = found %v, err %v, want failure", found, err)
	}
}

func TestEqualStrings(t *testing.T) {
	if !equalStrings([]string{"a", "b"}, []string{"a", "b"}) {
		t.Fatal("equalStrings() = false, want true")
	}
	if equalStrings([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("equalStrings() = true, want false")
	}
}

func TestInterruptedInitRetainsIdentity(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	first, err := loadRunState(repo, "/tmp/init.yaml", config.Config{}, Spec{}, ModelProvidersSpec{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCredentials(first.layout, Credentials{EnvID: first.binding.EnvID, BrainClaimToken: "test-token"}); err != nil {
		t.Fatal(err)
	}
	second, err := loadRunState(repo, "/tmp/init.yaml", config.Config{}, Spec{}, ModelProvidersSpec{}, func() {})
	if err != nil {
		t.Fatalf("resume interrupted initialization: %v", err)
	}
	if second.binding.EnvID != first.binding.EnvID {
		t.Fatal("retry changed environment identity")
	}
}

func TestMovedWorktreeRefreshesBinding(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	first, err := loadRunState(repo, "/tmp/old.yaml", config.Config{}, Spec{}, ModelProvidersSpec{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBinding(first.layout, first.binding); err != nil {
		t.Fatal(err)
	}
	moved := repo + "-moved"
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(moved) })
	second, err := loadRunState(moved, "/tmp/new.yaml", config.Config{}, Spec{}, ModelProvidersSpec{}, func() {})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := OpenRegistry(moved)
	if err != nil {
		t.Fatal(err)
	}
	orphans, err := registry.Orphans()
	if err != nil {
		t.Fatal(err)
	}
	if len(orphans) != 0 {
		t.Fatal("moved environment was classified as an orphan")
	}
	saved, _, err := LoadBinding(envFilePath(moved))
	if err != nil {
		t.Fatal(err)
	}
	if saved.Worktree != moved || saved.InitFile != "/tmp/new.yaml" || saved.EnvID != second.binding.EnvID {
		t.Fatalf("stale binding: %+v", saved)
	}
}
