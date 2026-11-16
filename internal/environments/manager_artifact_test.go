package environments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func testArtifactSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type managerArtifactTestProvider struct {
	writeErr         error
	activateExitCode int
	commands         []string
	writes           map[string][]byte
}

func (*managerArtifactTestProvider) SetTimeout(context.Context, string, time.Duration) error {
	return nil
}

func (*managerArtifactTestProvider) Create(context.Context, db.Environment, *db.EnvironmentWork, e2bruntime.Resolution) (e2bruntime.Sandbox, error) {
	panic("unexpected Create call")
}

func (*managerArtifactTestProvider) Kill(context.Context, string) error {
	panic("unexpected Kill call")
}

func (*managerArtifactTestProvider) Resolve(db.Environment, *db.EnvironmentWork) (e2bruntime.Resolution, error) {
	panic("unexpected Resolve call")
}

func (*managerArtifactTestProvider) FileExists(context.Context, string, string) (bool, error) {
	panic("unexpected FileExists call")
}

func (*managerArtifactTestProvider) StartBackgroundCommand(context.Context, string, string, []byte) error {
	panic("unexpected StartBackgroundCommand call")
}

func (*managerArtifactTestProvider) WriteFile(context.Context, string, string, []byte) error {
	panic("unexpected buffered WriteFile call")
}

func (p *managerArtifactTestProvider) WriteFileFrom(_ context.Context, _ string, path string, reader io.Reader) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if p.writes == nil {
		p.writes = map[string][]byte{}
	}
	p.writes[path] = data
	return p.writeErr
}

func (p *managerArtifactTestProvider) RunCommand(_ context.Context, _ string, request e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
	p.commands = append(p.commands, request.Command)
	return e2bruntime.CommandResult{ExitCode: p.activateExitCode}, nil
}

func managerArtifactTestRunner(t *testing.T, provider e2bruntime.Provider, artifact config.EnvironmentManagerArtifactConfig) *Runner {
	t.Helper()
	return &Runner{
		provider:         provider,
		managerArtifacts: newManagerArtifactSource(artifact, nil, nil),
		logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func writeManagerArtifactFile(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "environment-manager_linux_arm64")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return path
}

func loadManagerArtifactData(t *testing.T, source *managerArtifactSource) (managerArtifact, string) {
	t.Helper()
	artifact, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	defer artifact.File.Close()
	data, err := io.ReadAll(artifact.File)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	return artifact, string(data)
}

func waitManagerArtifactIdle(t *testing.T, source *managerArtifactSource) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		source.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("artifact load did not finish")
	}
}

func runManagerArtifactShell(t *testing.T, script string) error {
	t.Helper()
	for _, tool := range []string{"sh", "sha256sum", "cut"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable: %v", tool, err)
		}
	}
	return exec.Command("sh", "-c", script).Run()
}

func TestCopyManagerArtifactFailures(t *testing.T) {
	if _, _, err := copyManagerArtifact(io.Discard, strings.NewReader(""), ""); err == nil {
		t.Fatal("empty artifact accepted")
	}
	wrong := strings.Repeat("0", 64)
	if _, _, err := copyManagerArtifact(io.Discard, strings.NewReader("binary"), wrong); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
}

func TestManagerArtifactSourceFileFailures(t *testing.T) {
	missing := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: filepath.Join(t.TempDir(), "missing")}, nil, nil)
	if _, err := missing.Load(context.Background()); err == nil {
		t.Fatal("missing artifact accepted")
	}
	directory := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: t.TempDir()}, nil, nil)
	if _, err := directory.Load(context.Background()); err == nil {
		t.Fatal("directory artifact accepted")
	}
}

func TestManagerArtifactSourceRejectsFileChangedWhileReading(t *testing.T) {
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: writeManagerArtifactFile(t, []byte("v1"))}, nil, nil)
	_, err := source.populate(context.Background(), t.TempDir(), managerArtifactFileStamp{size: 1})
	if err == nil || !strings.Contains(err.Error(), "changed while reading") {
		t.Fatalf("populate() error = %v, want changed-while-reading failure", err)
	}
}

func TestManagerArtifactSourceURLFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: strings.Repeat("a", 64)}, server.Client(), nil)
	if _, err := source.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("Load() error = %v, want status failure", err)
	}
}

func TestManagerArtifactDownloadFailsWhenStalled(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		w.(http.Flusher).Flush()
		<-release
	}))
	defer server.Close()
	defer close(release)
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: strings.Repeat("a", 64)}, server.Client(), nil)
	source.idleTimeout = 50 * time.Millisecond
	if _, err := source.Load(context.Background()); !errors.Is(err, errManagerArtifactIdle) {
		t.Fatalf("Load() error = %v, want stalled download", err)
	}
}

func TestManagerArtifactPreloadFailureRetriesOnLoad(t *testing.T) {
	data := []byte("remote-binary")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: testArtifactSHA256(data)}, server.Client(), nil)
	source.Preload(context.Background())
	waitManagerArtifactIdle(t, source)
	if _, got := loadManagerArtifactData(t, source); got != string(data) {
		t.Fatalf("artifact data = %q, want retried download", got)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("download requests = %d, want 2", got)
	}
}

func TestManagerArtifactLoadCancelWhileWaitingKeepsDownload(t *testing.T) {
	data := []byte("remote-binary")
	release := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		<-release
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: testArtifactSHA256(data)}, server.Client(), nil)
	source.Preload(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := source.Load(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load() error = %v, want deadline exceeded while waiting", err)
	}
	close(release)
	if _, got := loadManagerArtifactData(t, source); got != string(data) {
		t.Fatalf("artifact data = %q", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("download requests = %d, want 1", got)
	}
}

func TestManagerArtifactWaitReturnsAfterShutdownCancel(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: strings.Repeat("a", 64)}, server.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	source.Preload(ctx)
	waitCtx, stopWaiting := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer stopWaiting()
	if _, err := source.Load(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Load() error = %v, want wait on preload", err)
	}
	cancel()
	waitManagerArtifactIdle(t, source)
}

func TestInstallEnvironmentManagerFailures(t *testing.T) {
	path := writeManagerArtifactFile(t, []byte("binary"))
	tests := []struct {
		name     string
		provider *managerArtifactTestProvider
		artifact config.EnvironmentManagerArtifactConfig
	}{
		{name: "load", provider: &managerArtifactTestProvider{}, artifact: config.EnvironmentManagerArtifactConfig{Path: path, SHA256: strings.Repeat("b", 64)}},
		{name: "upload", provider: &managerArtifactTestProvider{writeErr: errors.New("upload failed")}, artifact: config.EnvironmentManagerArtifactConfig{Path: path}},
		{name: "activate", provider: &managerArtifactTestProvider{activateExitCode: 1}, artifact: config.EnvironmentManagerArtifactConfig{Path: path}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := managerArtifactTestRunner(t, test.provider, test.artifact)
			managerPath, err := runner.installEnvironmentManager(context.Background(), "sbx_test")
			if !errors.Is(err, errEnvironmentManagerInstall) || managerPath != "" {
				t.Fatalf("installEnvironmentManager() = %q, %v; want install failure", managerPath, err)
			}
		})
	}
}

func TestManagerArtifactActivateCommandRejectsCorruptStaging(t *testing.T) {
	target := filepath.Join(t.TempDir(), "environment-manager")
	if err := os.WriteFile(target+".partial", []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := runManagerArtifactShell(t, managerArtifactActivateCommand(target, strings.Repeat("c", 64))); err == nil {
		t.Fatal("activation accepted corrupt staging file")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(target + ".partial"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging stat error = %v, want removed", err)
	}
}

func TestManagerArtifactActivateCommandRejectsUnrunnableBinary(t *testing.T) {
	target := filepath.Join(t.TempDir(), "environment-manager")
	data := []byte("#!/bin/sh\nexit 3\n")
	if err := os.WriteFile(target+".partial", data, 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := runManagerArtifactShell(t, managerArtifactActivateCommand(target, testArtifactSHA256(data))); err == nil {
		t.Fatal("activation accepted binary whose --version fails")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target stat error = %v, want not exist", err)
	}
}

func TestCopyManagerArtifactComputesChecksum(t *testing.T) {
	data := []byte("binary")
	var copied strings.Builder
	sha, size, err := copyManagerArtifact(&copied, strings.NewReader(string(data)), testArtifactSHA256(data))
	if err != nil {
		t.Fatalf("copyManagerArtifact() error = %v", err)
	}
	if sha != testArtifactSHA256(data) || size != int64(len(data)) || copied.String() != "binary" {
		t.Fatalf("sha=%s size=%d copied=%q", sha, size, copied.String())
	}
}

func TestNewManagerArtifactSourceDisabledWithoutLocation(t *testing.T) {
	if source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{}, nil, nil); source != nil {
		t.Fatalf("source = %+v, want nil", source)
	}
}

func TestManagerArtifactSourceReloadsReplacedFile(t *testing.T) {
	path := writeManagerArtifactFile(t, []byte("v1"))
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: path}, nil, nil)
	first, data := loadManagerArtifactData(t, source)
	if first.SHA256 != testArtifactSHA256([]byte("v1")) || data != "v1" {
		t.Fatalf("first artifact = %s %q", first.SHA256, data)
	}
	firstCache := source.cached.path
	replacement := path + ".next"
	if err := os.WriteFile(replacement, []byte("version-2"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("rename replacement: %v", err)
	}
	second, data := loadManagerArtifactData(t, source)
	if second.SHA256 != testArtifactSHA256([]byte("version-2")) || data != "version-2" {
		t.Fatalf("second artifact = %s %q, want replacement", second.SHA256, data)
	}
	if _, err := os.Stat(firstCache); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("previous cache file stat error = %v, want removed", err)
	}
}

func TestManagerArtifactSourceURLCachesVerifiedDownload(t *testing.T) {
	data := []byte("remote-binary")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: testArtifactSHA256(data)}, server.Client(), nil)
	for range 2 {
		if _, got := loadManagerArtifactData(t, source); got != string(data) {
			t.Fatalf("artifact data = %q", got)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("download requests = %d, want 1", got)
	}
}

func TestManagerArtifactSlowDownloadOutlivesIdleTimeout(t *testing.T) {
	chunks := []string{"slow-", "but-", "steady-", "binary"}
	data := []byte(strings.Join(chunks, ""))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for _, chunk := range chunks {
			_, _ = w.Write([]byte(chunk))
			w.(http.Flusher).Flush()
			time.Sleep(40 * time.Millisecond)
		}
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: testArtifactSHA256(data)}, server.Client(), nil)
	source.idleTimeout = 100 * time.Millisecond
	if _, got := loadManagerArtifactData(t, source); got != string(data) {
		t.Fatalf("artifact data = %q", got)
	}
}

func TestManagerArtifactLoadWaitsForPreload(t *testing.T) {
	data := []byte("remote-binary")
	release := make(chan struct{})
	started := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write(data)
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: testArtifactSHA256(data)}, server.Client(), nil)
	source.Preload(context.Background())
	<-started
	results := make(chan error, 3)
	for range 3 {
		go func() {
			artifact, err := source.Load(context.Background())
			if err == nil {
				err = artifact.File.Close()
			}
			results <- err
		}()
	}
	select {
	case err := <-results:
		t.Fatalf("Load returned before preload finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for range 3 {
		if err := <-results; err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("download requests = %d, want 1", got)
	}
}

func TestManagerArtifactPreloadSkipsDisabledAndFileSources(t *testing.T) {
	var disabled *managerArtifactSource
	disabled.Preload(context.Background())
	disabled.Wait()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: writeManagerArtifactFile(t, []byte("v1"))}, nil, nil)
	source.Preload(context.Background())
	if source.inflight != nil || source.cached != nil {
		t.Fatalf("file source preloaded: inflight=%v cached=%v", source.inflight, source.cached)
	}
}

func TestInstallEnvironmentManagerWithoutArtifactKeepsImageBinary(t *testing.T) {
	provider := &managerArtifactTestProvider{}
	runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	managerPath, err := runner.installEnvironmentManager(context.Background(), "sbx_test")
	if err != nil || managerPath != "" || len(provider.commands) != 0 || len(provider.writes) != 0 {
		t.Fatalf("installEnvironmentManager() = %q, %v; commands=%v writes=%d", managerPath, err, provider.commands, len(provider.writes))
	}
	if got := runner.environmentManagerConfig(managerPath).EnvironmentRunner.ManagerPath; got != "" {
		t.Fatalf("manager path override = %q, want unchanged config", got)
	}
}

func TestInstallEnvironmentManagerStreamsAndActivates(t *testing.T) {
	data := []byte("binary")
	sha := testArtifactSHA256(data)
	provider := &managerArtifactTestProvider{}
	runner := managerArtifactTestRunner(t, provider, config.EnvironmentManagerArtifactConfig{Path: writeManagerArtifactFile(t, data)})
	managerPath, err := runner.installEnvironmentManager(context.Background(), "sbx_test")
	if err != nil {
		t.Fatalf("installEnvironmentManager() error = %v", err)
	}
	want := "/tmp/oma/environment-manager/" + sha + "/environment-manager"
	if managerPath != want {
		t.Fatalf("manager path = %q, want %q", managerPath, want)
	}
	if string(provider.writes[want+".partial"]) != "binary" {
		t.Fatalf("staged writes = %v", provider.writes)
	}
	if len(provider.commands) != 1 || !strings.Contains(provider.commands[0], "mv -f -- '"+want+".partial' '"+want+"'") {
		t.Fatalf("commands = %v", provider.commands)
	}
	cfg := runner.environmentManagerConfig(managerPath)
	if command := environmentManagerShellCommand("cse_test", cfg); !strings.Contains(command, "exec '"+want+"' task-run") {
		t.Fatalf("manager command does not use installed artifact:\n%s", command)
	}
	if !strings.HasPrefix(buildPackageProvisionCommand(cfg), "'"+want+"' provision-packages") {
		t.Fatalf("package command = %q", buildPackageProvisionCommand(cfg))
	}
}

func TestManagerArtifactActivateCommandInstalls(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dir with 'quote'", "environment-manager")
	data := []byte("#!/bin/sh\n[ \"$1\" = --version ] && echo environment-runner 0.1.0\n")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target+".partial", data, 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := runManagerArtifactShell(t, managerArtifactActivateCommand(target, testArtifactSHA256(data))); err != nil {
		t.Fatalf("activate: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed target info = %v, %v", info, err)
	}
}
