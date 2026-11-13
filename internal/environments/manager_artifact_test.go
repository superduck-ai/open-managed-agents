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
	probeExitCode    int
	probeErr         error
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

func (p *managerArtifactTestProvider) WriteFile(_ context.Context, _ string, path string, data []byte) error {
	if p.writes == nil {
		p.writes = map[string][]byte{}
	}
	p.writes[path] = append([]byte(nil), data...)
	return p.writeErr
}

func (p *managerArtifactTestProvider) RunCommand(_ context.Context, _ string, request e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
	p.commands = append(p.commands, request.Command)
	if len(p.commands) == 1 {
		return e2bruntime.CommandResult{ExitCode: p.probeExitCode}, p.probeErr
	}
	return e2bruntime.CommandResult{ExitCode: p.activateExitCode}, nil
}

func managerArtifactTestRunner(t *testing.T, provider e2bruntime.Provider, artifact config.EnvironmentManagerArtifactConfig) *Runner {
	t.Helper()
	return &Runner{
		provider:         provider,
		managerArtifacts: newManagerArtifactSource(artifact, nil),
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

func TestReadManagerArtifactFailures(t *testing.T) {
	if _, err := readManagerArtifact(strings.NewReader(""), ""); err == nil {
		t.Fatal("empty artifact accepted")
	}
	wrong := strings.Repeat("0", 64)
	if _, err := readManagerArtifact(strings.NewReader("binary"), wrong); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("checksum mismatch error = %v", err)
	}
}

func TestReadManagerArtifactComputesChecksum(t *testing.T) {
	data := []byte("binary")
	artifact, err := readManagerArtifact(strings.NewReader(string(data)), testArtifactSHA256(data))
	if err != nil {
		t.Fatalf("readManagerArtifact() error = %v", err)
	}
	if artifact.SHA256 != testArtifactSHA256(data) || string(artifact.Data) != "binary" {
		t.Fatalf("artifact = %+v", artifact)
	}
}

func TestNewManagerArtifactSourceDisabledWithoutLocation(t *testing.T) {
	if source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{}, nil); source != nil {
		t.Fatalf("source = %+v, want nil", source)
	}
}

func TestManagerArtifactSourceFileFailures(t *testing.T) {
	missing := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: filepath.Join(t.TempDir(), "missing")}, nil)
	if _, err := missing.Load(context.Background()); err == nil {
		t.Fatal("missing artifact accepted")
	}
	directory := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: t.TempDir()}, nil)
	if _, err := directory.Load(context.Background()); err == nil {
		t.Fatal("directory artifact accepted")
	}
}

func TestManagerArtifactSourceReloadsReplacedFile(t *testing.T) {
	path := writeManagerArtifactFile(t, []byte("v1"))
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{Path: path}, nil)
	first, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("first Load() error = %v", err)
	}
	if first.SHA256 != testArtifactSHA256([]byte("v1")) {
		t.Fatalf("first sha = %s", first.SHA256)
	}
	replacement := path + ".next"
	if err := os.WriteFile(replacement, []byte("version-2"), 0o644); err != nil {
		t.Fatalf("write replacement: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("rename replacement: %v", err)
	}
	second, err := source.Load(context.Background())
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}
	if second.SHA256 != testArtifactSHA256([]byte("version-2")) {
		t.Fatalf("second sha = %s, want replacement checksum", second.SHA256)
	}
}

func TestManagerArtifactSourceURLFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: strings.Repeat("a", 64)}, server.Client())
	if _, err := source.Load(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("Load() error = %v, want status failure", err)
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
	source := newManagerArtifactSource(config.EnvironmentManagerArtifactConfig{URL: server.URL, SHA256: testArtifactSHA256(data)}, server.Client())
	for range 2 {
		artifact, err := source.Load(context.Background())
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if string(artifact.Data) != string(data) {
			t.Fatalf("artifact data = %q", artifact.Data)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("download requests = %d, want 1", got)
	}
}

func TestInstallEnvironmentManagerFailures(t *testing.T) {
	data := []byte("binary")
	path := writeManagerArtifactFile(t, data)
	tests := []struct {
		name     string
		provider *managerArtifactTestProvider
		artifact config.EnvironmentManagerArtifactConfig
	}{
		{name: "load", provider: &managerArtifactTestProvider{}, artifact: config.EnvironmentManagerArtifactConfig{Path: path, SHA256: strings.Repeat("b", 64)}},
		{name: "probe", provider: &managerArtifactTestProvider{probeErr: errors.New("probe failed")}, artifact: config.EnvironmentManagerArtifactConfig{Path: path}},
		{name: "upload", provider: &managerArtifactTestProvider{probeExitCode: 1, writeErr: errors.New("upload failed")}, artifact: config.EnvironmentManagerArtifactConfig{Path: path}},
		{name: "activate", provider: &managerArtifactTestProvider{probeExitCode: 1, activateExitCode: 1}, artifact: config.EnvironmentManagerArtifactConfig{Path: path}},
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

func TestInstallEnvironmentManagerReusesMatchingBinary(t *testing.T) {
	data := []byte("binary")
	provider := &managerArtifactTestProvider{}
	runner := managerArtifactTestRunner(t, provider, config.EnvironmentManagerArtifactConfig{Path: writeManagerArtifactFile(t, data)})
	managerPath, err := runner.installEnvironmentManager(context.Background(), "sbx_test")
	if err != nil {
		t.Fatalf("installEnvironmentManager() error = %v", err)
	}
	if managerPath != managerArtifactInstallPath(testArtifactSHA256(data)) || len(provider.writes) != 0 || len(provider.commands) != 1 {
		t.Fatalf("path=%q writes=%d commands=%v", managerPath, len(provider.writes), provider.commands)
	}
}

func TestInstallEnvironmentManagerUploadsAndActivates(t *testing.T) {
	data := []byte("binary")
	sha := testArtifactSHA256(data)
	provider := &managerArtifactTestProvider{probeExitCode: 1}
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
	if len(provider.commands) != 2 || !strings.Contains(provider.commands[1], "mv -f -- '"+want+".partial' '"+want+"'") {
		t.Fatalf("commands = %v", provider.commands)
	}
	cfg := runner.environmentManagerConfig(managerPath)
	command := buildEnvironmentManagerCommand("cse_test", cfg, []byte("{}"))
	if !strings.Contains(command.ShellCommand, "exec '"+want+"' task-run") {
		t.Fatalf("manager command does not use installed artifact:\n%s", command.ShellCommand)
	}
	if !strings.HasPrefix(buildPackageProvisionCommand(cfg), "'"+want+"' provision-packages") {
		t.Fatalf("package command = %q", buildPackageProvisionCommand(cfg))
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

func TestManagerArtifactShellCommandsInstallAndProbe(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dir with 'quote'", "environment-manager")
	data := []byte("#!/bin/sh\n[ \"$1\" = --version ] && echo environment-runner 0.1.0\n")
	sha := testArtifactSHA256(data)
	if err := runManagerArtifactShell(t, managerArtifactProbeCommand(target, sha)); err == nil {
		t.Fatal("probe accepted missing binary")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target+".partial", data, 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}
	if err := runManagerArtifactShell(t, managerArtifactActivateCommand(target, sha)); err != nil {
		t.Fatalf("activate: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("installed target info = %v, %v", info, err)
	}
	if err := runManagerArtifactShell(t, managerArtifactProbeCommand(target, sha)); err != nil {
		t.Fatalf("probe after install: %v", err)
	}
	if err := runManagerArtifactShell(t, managerArtifactProbeCommand(target, strings.Repeat("d", 64))); err == nil {
		t.Fatal("probe accepted binary with different checksum")
	}
}
