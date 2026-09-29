package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeDocker(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DOCKER_CALLS"
case "$1" in
 create) exit 0;;
 start) if [ "$PROBE_FAIL" = 1 ]; then exit 1; fi; printf 'Filesystem 1024-blocks Used Available Capacity Mounted\nvolume 9000000 100 8000000 1%% /data\n';;
 inspect) printf '0\n';;
 rm) if [ "$CLEANUP_FAIL" = 1 ]; then exit 1; fi;;
 ps) if [ "$CLEANUP_FAIL" = 1 ]; then printf 'owned-worker\n'; fi;;
 compose) case "$*" in *' port '*) printf '127.0.0.1:32123\n';; esac;;
esac
`
	if err := os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	calls := filepath.Join(directory, "calls")
	t.Setenv("DOCKER_CALLS", calls)
	return calls
}

func TestDoctorDiskAndProbeFailures(t *testing.T) {
	for _, value := range []string{"", "bad", "disk 10 5 NaN", "disk 10 5 1024"} {
		if _, err := availableDisk(value); err == nil {
			t.Fatalf("accepted disk output %q", value)
		}
	}
	if n, err := availableDisk("header\ndisk 2000000 1 1048576 1% /data\n"); err != nil || n != minimumDiskKiB {
		t.Fatalf("boundary n=%d err=%v", n, err)
	}
	for _, cleanupFails := range []bool{true, false} {
		t.Run(stringBool(cleanupFails), func(t *testing.T) {
			calls := fakeDocker(t)
			t.Setenv("PROBE_FAIL", "1")
			if cleanupFails {
				t.Setenv("CLEANUP_FAIL", "1")
			}
			_, err := doctorProbe(t.Context(), t.TempDir(), "local-image", true)
			if err == nil || errors.Is(err, errDoctorCleanup) != cleanupFails {
				t.Fatalf("probe verdict %v", err)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"--device /dev/fuse", "--cap-add SYS_ADMIN", "--security-opt apparmor=unconfined", "rm -fv verify-chat-doctor-"} {
				if !strings.Contains(string(data), want) {
					t.Fatalf("missing %s in %s", want, data)
				}
			}
		})
	}
}

func stringBool(value bool) string {
	if value {
		return "cleanup fails"
	}
	return "cleanup succeeds"
}

func TestEnvironmentCleanupOwnershipAndFailure(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(stringBool(fail), func(t *testing.T) {
			calls := fakeDocker(t)
			if fail {
				t.Setenv("CLEANUP_FAIL", "1")
			}
			directory := t.TempDir()
			for _, file := range []string{"config.json", "jwt.pem", "server", "server.pid", "report.md"} {
				if err := os.WriteFile(filepath.Join(directory, file), []byte("private"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			env := newEnvironment(directory, directory, "test-owned-run")
			failures := env.close()
			if (len(failures) > 0) != fail {
				t.Fatalf("cleanup=%v", failures)
			}
			data, err := os.ReadFile(calls)
			if err != nil {
				t.Fatal(err)
			}
			workerIndex := strings.Index(string(data), "ps -aq --filter label=oma.verify-chat.run=test-owned-run")
			downIndex := strings.Index(string(data), "down --volumes --remove-orphans")
			if workerIndex < 0 || downIndex <= workerIndex || !strings.Contains(string(data), "label=com.docker.compose.project=test-owned-run") {
				t.Fatalf("unsafe cleanup sequence: %s", data)
			}
			for _, file := range []string{"config.json", "jwt.pem", "server", "server.pid"} {
				if _, err := os.Stat(filepath.Join(directory, file)); !os.IsNotExist(err) {
					t.Fatalf("secret remains %s: %v", file, err)
				}
			}
			if _, err := os.Stat(filepath.Join(directory, "report.md")); err != nil {
				t.Fatal("evidence removed", err)
			}
		})
	}
}

func TestEnvironmentConfigurationAndTopology(t *testing.T) {
	fakeDocker(t)
	directory := t.TempDir()
	env := newEnvironment(directory, directory, "test-config")
	env.diagnostics = true
	if err := env.writeConfig(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer env.close()
	data, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Server struct {
			Addr            string `json:"addr"`
			DiagnosticsAddr string `json:"diagnostics_addr"`
		} `json:"server"`
		Runner struct {
			Enabled bool `json:"enabled"`
		} `json:"environment_runner"`
		Database struct {
			URL string `json:"url"`
		} `json:"database"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Runner.Enabled || cfg.Server.Addr != "127.0.0.1:18080" || !strings.Contains(cfg.Database.URL, "127.0.0.1:32123/verify") {
		t.Fatalf("nonisolated config %+v", cfg)
	}
	if listener, err := net.Listen("tcp", cfg.Server.DiagnosticsAddr); err == nil {
		listener.Close()
		t.Fatal("diagnostic port not reserved")
	}
	images := map[string]string{"postgres": "pg", "redis": "redis", "minio": "s3", "nats": "nats"}
	spec := dependencySpec(images)
	if len(spec.Services) != 6 {
		t.Fatal("dependency count", len(spec.Services))
	}
	for name, svc := range spec.Services {
		if len(svc.Ports) != 1 || !strings.HasPrefix(svc.Ports[0], "127.0.0.1::") || len(svc.Healthcheck.Test) == 0 {
			t.Fatalf("unsafe service %s", name)
		}
		if strings.HasPrefix(name, "nats") {
			routes := svc.Command[len(svc.Command)-1]
			if strings.Contains(routes, "nats://"+name+":") || len(strings.Split(routes, ",")) != 2 {
				t.Fatalf("invalid cluster routes %s", routes)
			}
		}
	}
}

func TestCommandCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := execute(ctx, t.TempDir(), nil, io.Discard, io.Discard, time.Second, "sh", "-c", "sleep 30 & wait")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled command: %v", err)
	}
	started := time.Now()
	err = execute(t.Context(), t.TempDir(), nil, io.Discard, io.Discard, 50*time.Millisecond, "sh", "-c", "sleep 30 & wait")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 3*time.Second {
		t.Fatalf("process group timeout: %v", err)
	}
}

func TestDiagnosticDownloadBounds(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status, size int
		failed       bool
	}{
		{"HTTP error", 503, 0, true}, {"oversized", 200, (64 << 20) + 1, true}, {"empty", 200, 0, true}, {"valid", 200, 100, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				chunk := make([]byte, 4096)
				for remaining := tc.size; remaining > 0; {
					n := min(remaining, len(chunk))
					if _, err := w.Write(chunk[:n]); err != nil {
						return
					}
					remaining -= n
				}
			}))
			defer server.Close()
			env := newEnvironment("", t.TempDir(), "download")
			env.diagnosticsAddr = strings.TrimPrefix(server.URL, "http://")
			err := env.downloadProfile(t.Context(), "test.pprof", "heap")
			if (err != nil) != tc.failed {
				t.Fatalf("download err=%v", err)
			}
			if tc.failed {
				if _, err := os.Stat(filepath.Join(env.directory, "test.pprof")); !os.IsNotExist(err) {
					t.Fatal("invalid profile retained")
				}
			}
		})
	}
}

func TestMarkdownContainsComparableEnvironment(t *testing.T) {
	directory := t.TempDir()
	r := performanceFixture()
	r.Source = sourceVersion{Commit: "harness-commit", SHA256: "source-hash"}
	r.Timeout = "5m"
	if err := evaluatePerformance(&r, ""); err != nil {
		t.Fatal(err)
	}
	if err := saveReport(directory, r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(directory, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{r.BackendCommit, r.BinarySHA256, "harness-commit", "source-hash", "sha256:worker", "go: same", "test-machine", "5m"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("report missing %s", want)
		}
	}
}

func TestGoToolStderrDoesNotCorruptTestEvidence(t *testing.T) {
	directory := t.TempDir()
	selected := scenarios["chat.roundtrip"]
	evidence := filepath.Join(directory, "fixture.jsonl")
	events := testOutput(t, selected, []testEvent{{Test: selected.Test, Action: "pass"}, {Action: "pass"}}, true)
	if err := os.WriteFile(evidence, []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'go: downloading example.test/module v1.0.0\\n' >&2\ncat \"$VERIFY_TEST_EVENTS\"\n"
	if err := os.WriteFile(filepath.Join(directory, "go"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("VERIFY_TEST_EVENTS", evidence)
	env := newEnvironment(directory, directory, "stderr-fixture")
	result, err := runTest(t.Context(), env, "test-image", selected)
	if err != nil || result.Status != "pass" {
		t.Fatalf("stderr invalidated successful tests: %+v %v", result, err)
	}
	stderr, err := os.ReadFile(filepath.Join(directory, "tests.stderr.log"))
	if err != nil || !strings.Contains(string(stderr), "go: downloading") {
		t.Fatalf("missing tool diagnostics: %s %v", stderr, err)
	}
}
