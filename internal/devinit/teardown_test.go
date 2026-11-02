package devinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTeardownVerifiesResourceRemovalBeforeDroppingState(t *testing.T) {
	for _, scenario := range []string{"container", "volume", "network", "all", "list-failure", "no-such-project", "removed"} {
		t.Run(scenario, func(t *testing.T) {
			_, root := recycleFixture(t)
			registry, binding := bindRecycleFixture(t, root)
			entry, _, err := registry.Get(binding.Name)
			if err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(generatedDir(root), "state.json")
			if err := writePrivateFile(statePath, 0o600, binding); err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			script := `#!/bin/sh
printf '%s\n' "$*" >> "$TEARDOWN_TEST_LOG"
if [ "$1" = compose ]; then
  case " $* " in
    *' --profile * down --volumes --remove-orphans '*) ;;
    *) exit 41 ;;
  esac
  touch "$TEARDOWN_TEST_DONE"
  if [ "$TEARDOWN_TEST_MODE" = no-such-project ]; then
    echo 'no such project'
    exit 1
  fi
  exit 0
fi
if [ "$2" = inspect ]; then
  printf '{"oma.devinit.env_id":"%s"}\n' "$TEARDOWN_TEST_ENV_ID"
  exit 0
fi
if [ ! -f "$TEARDOWN_TEST_DONE" ]; then
  echo "$1-resource"
elif [ "$TEARDOWN_TEST_MODE" = list-failure ]; then
  exit 2
elif [ "$TEARDOWN_TEST_MODE" = "$1" ] || [ "$TEARDOWN_TEST_MODE" = all ] || [ "$TEARDOWN_TEST_MODE" = no-such-project ]; then
  echo "$1-resource"
fi
`
			if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bin, "portless"), []byte("#!/bin/sh\necho alias >> \"$TEARDOWN_TEST_LOG\"\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(bin, "calls")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TEARDOWN_TEST_LOG", logPath)
			t.Setenv("TEARDOWN_TEST_DONE", filepath.Join(bin, "done"))
			t.Setenv("TEARDOWN_TEST_ENV_ID", binding.EnvID)
			t.Setenv("TEARDOWN_TEST_MODE", scenario)
			target := teardownTarget{name: binding.Name, entry: entry, binding: binding, local: true}
			err = teardownOne(t.Context(), root, registry, target)
			if scenario != "removed" {
				assertTeardownFailurePreservesState(t, err, scenario, registry, binding, statePath, logPath)
				t.Setenv("TEARDOWN_TEST_MODE", "removed")
				if err := teardownOne(t.Context(), root, registry, target); err != nil {
					t.Fatal("retry failed:", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if _, found, err := registry.Get(binding.Name); err != nil || found {
				t.Fatalf("registry remains after cleanup: %v", err)
			}
			for _, path := range []string{envFilePath(root), generatedDir(root)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("runtime state remains at %s: %v", path, err)
				}
			}
		})
	}
}

func assertTeardownFailurePreservesState(t *testing.T, teardownErr error, scenario string, registry Registry, binding EnvBinding, statePath, logPath string) {
	t.Helper()
	want := "still has resources"
	if scenario == "list-failure" {
		want = "list container resources"
	}
	if teardownErr == nil || !strings.Contains(teardownErr.Error(), want) {
		t.Fatalf("expected %s, got %v", want, teardownErr)
	}
	if scenario == "all" {
		for _, kind := range []string{"container", "volume", "network"} {
			if !strings.Contains(teardownErr.Error(), kind+"-resource") {
				t.Fatalf("missing %s in %v", kind, teardownErr)
			}
		}
	}
	for _, path := range []string{envFilePath(binding.Worktree), statePath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("runtime state removed:", err)
		}
	}
	if _, found, err := registry.Get(binding.Name); err != nil || !found {
		t.Fatalf("registry removed: %v", err)
	}
	calls, err := os.ReadFile(logPath)
	if err != nil || strings.Contains(string(calls), "alias") {
		t.Fatalf("aliases removed after failure: %s, %v", calls, err)
	}
}

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
