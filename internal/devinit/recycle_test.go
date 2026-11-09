package devinit

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recycleFixture(t *testing.T) (string, string) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(parent, "main")
	tree := filepath.Join(parent, "linked")
	if err := os.Mkdir(main, 0o700); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		if _, err := recycleGit(context.Background(), main, args...); err != nil {
			t.Fatal(err)
		}
	}
	git("init")
	git("config", "user.email", "test@example.invalid")
	git("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(main, ".gitignore"), []byte(".run/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".gitignore")
	git("commit", "-m", "fixture")
	git("worktree", "add", "-b", "fixture", tree)
	return main, tree
}

func bindRecycleFixture(t *testing.T, root string) (Registry, EnvBinding) {
	t.Helper()
	registry, err := OpenRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	binding, _, err := registry.Create(root, filepath.Join(root, ".run", "dev-init.yaml"), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(envFilePath(root), 0o600, binding); err != nil {
		t.Fatal(err)
	}
	return registry, binding
}

func TestRecycleRejectsUnsafeTargets(t *testing.T) {
	for _, scenario := range []string{"confirmation", "foreign-binding", "missing-binding", "foreign-worktree"} {
		t.Run(scenario, func(t *testing.T) {
			main, root := recycleFixture(t)
			opts := RecycleOptions{Yes: true, Worktree: root}
			switch scenario {
			case "confirmation":
				opts.Yes = false
			case "foreign-worktree":
				_, other := recycleFixture(t)
				opts.Worktree = other
			case "foreign-binding":
				_, binding := bindRecycleFixture(t, root)
				binding.EnvID = "different"
				if err := writePrivateFile(envFilePath(root), 0o600, binding); err != nil {
					t.Fatal(err)
				}
			case "missing-binding":
				bindRecycleFixture(t, root)
				if err := os.Remove(envFilePath(root)); err != nil {
					t.Fatal(err)
				}
			}
			if err := Recycle(context.Background(), main, opts, &bytes.Buffer{}); err == nil {
				t.Fatal("expected rejection")
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("target was removed:", err)
			}
		})
	}
}

func fakeRecycleDocker(t *testing.T, failure bool) string {
	t.Helper()
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$RECYCLE_TEST_LOG\"\nif [ \"$1\" = compose ] && [ -n \"$RECYCLE_TEST_SERVICE_PID\" ] && kill -0 \"$RECYCLE_TEST_SERVICE_PID\" 2>/dev/null; then exit 42; fi\n"
	if failure {
		script += "if [ \"$1\" = compose ]; then exit 2; fi\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "portless"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RECYCLE_TEST_LOG", log)
	return log
}

func TestRecycleComposeFailureRetainsWorktreeAndBinding(t *testing.T) {
	main, root := recycleFixture(t)
	registry, binding := bindRecycleFixture(t, root)
	fakeRecycleDocker(t, true)
	err := Recycle(context.Background(), main, RecycleOptions{Yes: true, Worktree: root}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "compose down") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(envFilePath(root)); err != nil {
		t.Fatal(err)
	}
	if _, found, err := registry.Get(binding.Name); err != nil || !found {
		t.Fatal("registry removed", err)
	}
}

func TestRecycleStopsServicesAndRetainsWorktree(t *testing.T) {
	main, root := recycleFixture(t)
	registry, binding := bindRecycleFixture(t, root)
	log := fakeRecycleDocker(t, false)
	service := startRecycleService(t, root)
	t.Setenv("RECYCLE_TEST_SERVICE_PID", fmt.Sprint(service.Process.Pid))
	if err := os.MkdirAll(filepath.Join(root, ".run", "secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".run", "secrets", "key"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Recycle(context.Background(), root, RecycleOptions{Yes: true}, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("worktree was removed", err)
	}
	if _, found, err := LoadBinding(envFilePath(root)); err != nil || found {
		t.Fatal("binding remains", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".run", "secrets", "key"))
	if err != nil || string(data) != "fixture" {
		t.Fatal("manual secrets changed", err)
	}
	if err := Recycle(context.Background(), main, RecycleOptions{Yes: true, Worktree: root}, &bytes.Buffer{}); err != nil {
		t.Fatal("cleanup retry failed", err)
	}
	if _, found, err := registry.Get(binding.Name); err != nil || found {
		t.Fatal("registry remains", err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("compose -p oma-%s --project-directory %s -f deploy/docker-compose/dev-env.yaml --profile * down --volumes --remove-orphans", binding.Name, root)
	if !strings.Contains(string(calls), expected) {
		t.Fatalf("missing volume cleanup: %s", calls)
	}
	if _, err := recycleGit(context.Background(), main, "show-ref", "--verify", "refs/heads/fixture"); err != nil {
		t.Fatal("branch removed", err)
	}
}

func startRecycleService(t *testing.T, root string) *exec.Cmd {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "exe")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "open-managed-agents")
	if err := os.Symlink(sleep, path); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(path, "30")
	command.Dir = root
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	return command
}

func TestRecycleUnboundWorktree(t *testing.T) {
	main, root := recycleFixture(t)
	if err := Recycle(context.Background(), main, RecycleOptions{Yes: true, Worktree: root}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal("worktree was removed", err)
	}
}

func TestRecycleServiceCommandOwnership(t *testing.T) {
	root := "/fixture"
	for _, command := range []string{"go run ./cmd/dev-init recycle --yes", "bash", "node other.js", "bun test", "go test ./..."} {
		if serviceCommand(root, command) {
			t.Fatalf("unexpected service: %s", command)
		}
	}
	for _, command := range []string{"go run .", "bun dist/main.js", "bun run src/main.ts", "node /fixture/web/node_modules/.bin/vite --port 61000", "bun run dev -- --host 127.0.0.1", "/tmp/go-build123/b001/exe/open-managed-agents"} {
		if !serviceCommand(root, command) {
			t.Fatalf("missing service: %s", command)
		}
	}
	if serviceDirectory(root, "/fixture/nested/worktree") {
		t.Fatal("nested checkout accepted")
	}
}

func TestStopLocalServicesLeavesOtherDirectoryAlone(t *testing.T) {
	main, root := recycleFixture(t)
	command := exec.Command("sleep", "30")
	command.Dir = main
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	if err := stopLocalServices(context.Background(), root, []localServiceProcess{{pid: command.Process.Pid, command: "sleep 30"}}); err != nil {
		t.Fatal(err)
	}
	data, err := exec.Command("ps", "-p", fmt.Sprint(command.Process.Pid), "-o", "command=").Output()
	if err != nil || !strings.Contains(string(data), "sleep") {
		t.Fatal("foreign process stopped", err)
	}
}

func TestRecycleRejectsForeignDockerResources(t *testing.T) {
	main, root := recycleFixture(t)
	bindRecycleFixture(t, root)
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$2\" = ls ]; then echo foreign; else echo '{\"oma.devinit.env_id\":\"other\"}'; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	err := Recycle(context.Background(), main, RecycleOptions{Yes: true, Worktree: root}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(envFilePath(root)); err != nil {
		t.Fatal(err)
	}
}

func TestStopLocalServicesWaitsForExit(t *testing.T) {
	_, root := recycleFixture(t)
	command := exec.Command("sleep", "30")
	command.Dir = root
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill() })
	if err := stopLocalServices(context.Background(), root, []localServiceProcess{{pid: command.Process.Pid, command: "sleep 30"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-exited:
		if err == nil {
			t.Fatal("process unexpectedly exited successfully")
		}
	case <-time.After(time.Second):
		t.Fatal("stop returned before the process exited")
	}
}

func TestRecycleRejectsReusedServicePort(t *testing.T) {
	main, root := recycleFixture(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port
	if err := writePrivateFile(filepath.Join(generatedDir(root), "state.json"), 0o600, State{Runtime: &RuntimePorts{APIPort: port}}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Recycle(context.Background(), main, RecycleOptions{Yes: true, Worktree: root}, &output)
	if err == nil || !strings.Contains(err.Error(), "outside target worktree") {
		t.Fatalf("unexpected error: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("cleanup started before preflight passed: %s", output.String())
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

func TestRecycleRetainsDirtyAndLockedWorktrees(t *testing.T) {
	for _, scenario := range []string{"primary", "dirty", "untracked", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			main, root := recycleFixture(t)
			if scenario == "primary" {
				root = main
			}
			path := filepath.Join(root, ".gitignore")
			if scenario == "untracked" {
				path = filepath.Join(root, "unsaved")
			}
			if scenario == "dirty" || scenario == "untracked" {
				if err := os.WriteFile(path, []byte("keep this change"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "locked" {
				if _, err := recycleGit(context.Background(), main, "worktree", "lock", root); err != nil {
					t.Fatal(err)
				}
			}
			if err := Recycle(context.Background(), main, RecycleOptions{Yes: true, Worktree: root}, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("checkout removed", err)
			}
			if scenario == "dirty" || scenario == "untracked" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "keep this change" {
					t.Fatal("source changes lost", err)
				}
			}
		})
	}
}
