package devinit

import (
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestStateAndCredentialsRoundTrip(t *testing.T) {
	binding := testBinding()
	layout, err := NewLayout(binding, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveBinding(layout, binding); err != nil {
		t.Fatal(err)
	}
	loadedBinding, found, err := LoadBinding(layout.BindingFile)
	if err != nil || !found || !reflect.DeepEqual(loadedBinding, binding) {
		t.Fatal("binding did not survive persistence")
	}
	spec, state, _ := pendingKeyFixture()
	spec.FixedSecretsDir = t.TempDir()
	for _, name := range fixedSecretFiles {
		if err := os.WriteFile(filepath.Join(spec.FixedSecretsDir, name), []byte("fixed-key"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state.EnvID, state.Name = binding.EnvID, binding.Name
	credentials := Credentials{}
	if err := copyFixedSecretsAndCredentials(layout, spec, &state, &credentials, false); err != nil {
		t.Fatal(err)
	}
	if err := prepareResourceIdentities(layout, spec, &state, &credentials); err != nil {
		t.Fatal(err)
	}
	state.Phase = phaseKeys
	state.Runtime = &RuntimePorts{APIPort: 40080, WebPort: 61080}
	if err := SaveState(layout, state); err != nil {
		t.Fatal(err)
	}
	loadedState, found, err := LoadState(layout.StateFile)
	if err != nil || !found || !reflect.DeepEqual(loadedState, state) {
		t.Fatal("state lost identity, resource mappings or runtime ports")
	}
	loadedCredentials, found, err := LoadCredentials(layout.CredentialsFile)
	if err != nil || !found || !reflect.DeepEqual(loadedCredentials, credentials) {
		t.Fatal("persisted credentials differ from generated credentials")
	}
	if len(loadedCredentials.APIKeys) != 4 {
		t.Fatal("expected four client keys")
	}
	seen := make(map[string]bool)
	for _, key := range loadedCredentials.APIKeys {
		if !strings.HasPrefix(key.Key, "sk-ant-api03-") || key.Key == "sk-ant-api03-" || seen[key.Key] {
			t.Fatal("client keys must have the supported prefix and distinct nonempty values")
		}
		seen[key.Key] = true
	}
	tokenPath := filepath.Join(layout.SecretsDir, "brain-claim-token")
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	if err := copyFixedSecretsAndCredentials(layout, spec, &loadedState, &loadedCredentials, true); err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(tokenPath)
	if err != nil || string(token) != credentials.BrainClaimToken || !reflect.DeepEqual(loadedCredentials, credentials) {
		t.Fatal("credential recovery changed the claim token or client keys")
	}
	info, err := os.Stat(layout.CredentialsFile)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("credentials file must remain private")
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
