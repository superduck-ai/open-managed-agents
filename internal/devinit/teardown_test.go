package devinit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveLocalStatePreservesInputs(t *testing.T) {
	root := t.TempDir()
	inputs := []string{"config.yaml", "dev-init.yaml", "model-providers.yaml", "secrets/provider-key"}
	for _, name := range append(inputs, "env.json", "generated/state.json", "generated/credentials.json") {
		path := filepath.Join(root, ".run", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeLocalState(EnvBinding{Worktree: root}); err != nil {
		t.Fatal(err)
	}
	for _, name := range inputs {
		if _, err := os.Stat(filepath.Join(root, ".run", name)); err != nil {
			t.Errorf("input %s was removed: %v", name, err)
		}
	}
	for _, name := range []string{"env.json", "generated"} {
		if _, err := os.Stat(filepath.Join(root, ".run", name)); !os.IsNotExist(err) {
			t.Errorf("runtime %s remains after cleanup: %v", name, err)
		}
	}
}
