package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFixture(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o700); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"internal/db/example_mapper.go", "internal/db/example_mapper.xml", "internal/shared/types.go", "cmd/generate-go/main.go", "scripts/generate-go.sh", "go.mod", "go.sum"} {
		writeFixture(t, root, name, "initial")
	}
	writeFixture(t, root, "tools/go", `#!/bin/sh
case "$1" in
env)
  if [ -f environment ]; then cat environment; exit 0; fi
  printf '{"GOVERSION":"%s","GOWORK":""}\n' "$(cat version 2>/dev/null || printf test)"
  ;;
list)
  if [ -f packages ]; then cat packages; exit 0; fi
  if [ -f unresolved ]; then printf '{"Error":{"Err":"unresolved"}}'; exit 0; fi
  printf '{"Dir":"%s/internal/shared","Module":{"Dir":"%s","Main":true}}\n' "$PWD" "$PWD"
  ;;
generate)
  printf 'generate\n' >> calls
  if [ -f fail ]; then exit 1; fi
  touch ready
  while [ -f hold ]; do sleep 0.02; done
  for mapper in internal/db/*_mapper.go; do
    [ -f "$mapper" ] || continue
    cp "$mapper" "${mapper%.go}.sqlmap.gen.go"
  done
  ;;
run)
  printf 'run\n' >> calls
  ;;
*) exit 2 ;;
esac
`)
	t.Setenv("PATH", filepath.Join(root, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root
}

func generateFixture(t *testing.T, root string, cached bool) {
	t.Helper()
	if err := generate(context.Background(), root, cached); err != nil {
		t.Fatal(err)
	}
}

func callCount(t *testing.T, root string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "generate\n")
}

func requireNoCache(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "tmp/go-generation/cache.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no cache, got %v", err)
	}
}

func waitForFile(t *testing.T, root, name string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", name)
}

func TestGenerationFailureInvalidatesCache(t *testing.T) {
	root := fixture(t)
	generateFixture(t, root, true)
	writeFixture(t, root, "fail", "")
	if err := generate(context.Background(), root, false); err == nil {
		t.Fatal("expected generation failure")
	}
	requireNoCache(t, root)
	if err := os.Remove(filepath.Join(root, "fail")); err != nil {
		t.Fatal(err)
	}
	generateFixture(t, root, true)
	if count := callCount(t, root); count != 3 {
		t.Fatalf("expected retry, got %d calls", count)
	}
}

func TestInterruptedGeneration(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "hold", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- generate(ctx, root, true) }()
	waitForFile(t, root, "ready")
	cancel()
	if err := <-result; err == nil {
		t.Fatal("expected interrupted generation to fail")
	}
	requireNoCache(t, root)
	if err := os.Remove(filepath.Join(root, "hold")); err != nil {
		t.Fatal(err)
	}
	generateFixture(t, root, true)
}

func TestOutputChangesInvalidateCache(t *testing.T) {
	for _, change := range []string{"missing", "modified", "extra", "invalid-cache"} {
		t.Run(change, func(t *testing.T) {
			root := fixture(t)
			generateFixture(t, root, true)
			switch change {
			case "missing":
				if err := os.Remove(filepath.Join(root, "internal/db/example_mapper.sqlmap.gen.go")); err != nil {
					t.Fatal(err)
				}
			case "modified":
				writeFixture(t, root, "internal/db/example_mapper.sqlmap.gen.go", "modified")
			case "extra":
				writeFixture(t, root, "internal/db/deleted/old.sqlmap.gen.go", "stale")
			case "invalid-cache":
				writeFixture(t, root, "tmp/go-generation/cache.json", "invalid")
			}
			generateFixture(t, root, true)
			if count := callCount(t, root); count != 2 {
				t.Fatalf("expected regeneration, got %d calls", count)
			}
			if _, err := os.Stat(filepath.Join(root, "internal/db/deleted/old.sqlmap.gen.go")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale output remains: %v", err)
			}
		})
	}
}

func TestUnresolvedDependenciesDisableCache(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "unresolved", "")
	generateFixture(t, root, true)
	generateFixture(t, root, true)
	requireNoCache(t, root)
	if count := callCount(t, root); count != 2 {
		t.Fatalf("expected two full generations, got %d", count)
	}
}

func TestInputsChangingDuringGeneration(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "hold", "")
	result := make(chan error, 1)
	go func() { result <- generate(context.Background(), root, true) }()
	waitForFile(t, root, "ready")
	writeFixture(t, root, "internal/db/example_mapper.xml", "changed")
	if err := os.Remove(filepath.Join(root, "hold")); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	requireNoCache(t, root)
}

func TestGenerationLock(t *testing.T) {
	root := fixture(t)
	directory := filepath.Join(root, "tmp/go-generation")
	lock, err := acquireLock(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if second, err := acquireLock(ctx, directory); err == nil {
		second.Close()
		t.Fatal("second lock acquired before first lock was released")
	}
}

func TestGenerationChildRetainsLock(t *testing.T) {
	root := fixture(t)
	directory := filepath.Join(root, "tmp/go-generation")
	lock, err := acquireLock(context.Background(), directory)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	writeFixture(t, root, "hold", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := goCommand(ctx, root, "generate", "./internal/db")
	command.ExtraFiles = []*os.File{lock}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		command.Wait()
	}()
	waitForFile(t, root, "ready")
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	lockCtx, lockCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer lockCancel()
	if second, err := acquireLock(lockCtx, directory); err == nil {
		second.Close()
		t.Fatal("child did not retain generation lock")
	}
	cancel()
}

func TestConcurrentGeneration(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "hold", "")
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- generate(context.Background(), root, false) }()
	waitForFile(t, root, "ready")
	go func() { second <- generate(context.Background(), root, true) }()
	if err := os.Remove(filepath.Join(root, "hold")); err != nil {
		t.Fatal(err)
	}
	for _, result := range []chan error{first, second} {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	if count := callCount(t, root); count != 1 {
		t.Fatalf("expected one generation, got %d", count)
	}
}

func TestGenerationCacheAndForce(t *testing.T) {
	root := fixture(t)
	generateFixture(t, root, true)
	generateFixture(t, root, true)
	writeFixture(t, root, "internal/unrelated/handler.go", "changed")
	generateFixture(t, root, true)
	if count := callCount(t, root); count != 1 {
		t.Fatalf("expected cache hits, got %d calls", count)
	}
	generateFixture(t, root, false)
	if count := callCount(t, root); count != 2 {
		t.Fatalf("expected forced generation, got %d calls", count)
	}
}

func TestGenerationInputChanges(t *testing.T) {
	for _, name := range []string{"internal/db/example_mapper.go", "internal/db/example_mapper.xml", "internal/db/new.go", "internal/shared/types.go", "go.mod", "go.sum", "scripts/generate-go.sh", "cmd/generate-go/main.go", "version"} {
		t.Run(name, func(t *testing.T) {
			root := fixture(t)
			generateFixture(t, root, true)
			writeFixture(t, root, name, "changed")
			generateFixture(t, root, true)
			if count := callCount(t, root); count != 2 {
				t.Fatalf("expected regeneration, got %d calls", count)
			}
		})
	}
}

func TestDeletedMapperRemovesOutput(t *testing.T) {
	root := fixture(t)
	generateFixture(t, root, true)
	if err := os.Remove(filepath.Join(root, "internal/db/example_mapper.go")); err != nil {
		t.Fatal(err)
	}
	generateFixture(t, root, true)
	if _, err := os.Stat(filepath.Join(root, "internal/db/example_mapper.sqlmap.gen.go")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted mapper output remains: %v", err)
	}
}

func TestRestartGenerationFailureKeepsService(t *testing.T) {
	root := fixture(t)
	data, err := os.ReadFile("../../scripts/restart-server.sh")
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "scripts/restart-server.sh", string(data))
	writeFixture(t, root, "config/config.yaml", "")
	writeFixture(t, root, "scripts/generate-go.sh", "#!/bin/sh\nprintf '%s' \"$1\" > \"$(dirname \"$0\")/../arguments\"\nexit 1\n")
	writeFixture(t, root, "tools/lsof", "#!/bin/sh\ntouch \"$CONFIG_FILE.listeners\"\n")
	command := exec.Command("bash", filepath.Join(root, "scripts/restart-server.sh"))
	if err := command.Run(); err == nil {
		t.Fatal("expected restart to fail")
	}
	arguments, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil || string(arguments) != "--cached" {
		t.Fatalf("expected one cached generation call: %s, %v", arguments, err)
	}
	if _, err := os.Stat(filepath.Join(root, "config/config.yaml.listeners")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart inspected listeners after generation failure: %v", err)
	}
}

func TestWorkspaceAndLocalReplacement(t *testing.T) {
	root := fixture(t)
	dependency := t.TempDir()
	writeFixture(t, dependency, "go.mod", "module dependency")
	writeFixture(t, dependency, "types/types.go", "initial")
	workspace := filepath.Join(dependency, "go.work")
	writeFixture(t, dependency, "go.work", "initial")
	writeFixture(t, dependency, "go.work.sum", "initial")
	writeJSONFixture(t, root, "environment", map[string]string{"GOWORK": workspace})
	writeJSONFixture(t, root, "packages", listedPackage{
		Dir:    filepath.Join(dependency, "types"),
		Module: &listedModule{Version: "v1", Replace: &listedModule{Dir: dependency}},
	})
	generateFixture(t, root, true)
	for i, name := range []string{"types/types.go", "go.mod", "go.work", "go.work.sum"} {
		writeFixture(t, dependency, name, "changed")
		generateFixture(t, root, true)
		if count := callCount(t, root); count != i+2 {
			t.Fatalf("expected regeneration after %s, got %d calls", name, count)
		}
	}
}

func writeJSONFixture(t *testing.T, root, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, name, string(data))
}

func TestVendorDependencyChanges(t *testing.T) {
	root := fixture(t)
	writeFixture(t, root, "vendor/dependency/types.go", "initial")
	writeFixture(t, root, "vendor/modules.txt", "initial")
	writeJSONFixture(t, root, "packages", listedPackage{
		Dir:    filepath.Join(root, "vendor/dependency"),
		Module: &listedModule{Version: "v1"},
	})
	generateFixture(t, root, true)
	writeFixture(t, root, "vendor/dependency/types.go", "changed")
	generateFixture(t, root, true)
	writeFixture(t, root, "vendor/modules.txt", "changed")
	generateFixture(t, root, true)
	if count := callCount(t, root); count != 3 {
		t.Fatalf("expected vendor changes to regenerate, got %d", count)
	}
}

func TestAlternateInputsDisableCache(t *testing.T) {
	for _, flag := range []string{"-overlay=overlay.json", "-modfile=alternate.mod"} {
		t.Run(flag, func(t *testing.T) {
			root := fixture(t)
			writeJSONFixture(t, root, "environment", map[string]string{"GOFLAGS": flag})
			generateFixture(t, root, true)
			generateFixture(t, root, true)
			requireNoCache(t, root)
			if count := callCount(t, root); count != 2 {
				t.Fatalf("expected full generation, got %d", count)
			}
		})
	}
}

func TestRestartSuccessfulGeneration(t *testing.T) {
	root := fixture(t)
	data, err := os.ReadFile("../../scripts/restart-server.sh")
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "scripts/restart-server.sh", string(data))
	writeFixture(t, root, "config/config.yaml", "")
	writeFixture(t, root, "scripts/generate-go.sh", "#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$(dirname \"$0\")/../arguments\"\n")
	writeFixture(t, root, "tools/lsof", "#!/bin/sh\nexit 0\n")
	command := exec.Command("bash", filepath.Join(root, "scripts/restart-server.sh"))
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("restart: %s: %v", output, err)
	}
	arguments, err := os.ReadFile(filepath.Join(root, "arguments"))
	if err != nil || string(arguments) != "--cached\n" {
		t.Fatalf("expected one cached generation call: %s, %v", arguments, err)
	}
	calls, err := os.ReadFile(filepath.Join(root, "calls"))
	if err != nil || string(calls) != "run\n" {
		t.Fatalf("expected server start: %s, %v", calls, err)
	}
}

func TestContentChangesWithPreservedModificationTime(t *testing.T) {
	root := fixture(t)
	generateFixture(t, root, true)
	path := filepath.Join(root, "internal/db/example_mapper.xml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "internal/db/example_mapper.xml", "changed")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	generateFixture(t, root, true)
	if count := callCount(t, root); count != 2 {
		t.Fatalf("expected content change to regenerate, got %d", count)
	}
}
