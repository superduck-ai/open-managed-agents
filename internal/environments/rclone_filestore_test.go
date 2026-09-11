package environments

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func TestBuildRcloneMultimountConfig(t *testing.T) {
	const (
		filesystemID = "claude_chat_test"
		serviceURL   = "http://host.docker.internal:38080/"
		readWrite    = "rw-token"
		readonly     = "ro-token"
	)
	got := buildRcloneMultimountConfig(filesystemID, serviceURL, readWrite, readonly, nil)
	if got.ReadyFile != rcloneReadyPath || got.StateDir != rcloneStateDirectory || got.ServiceURL != "http://host.docker.internal:38080" {
		t.Fatalf("unexpected multimount envelope: %+v", got)
	}
	if len(got.Mounts) != 5 {
		t.Fatalf("mount count = %d, want 5", len(got.Mounts))
	}
	wantSources := []string{"/outputs", "/uploads", "/transcripts", "/tool_results", "/skills"}
	wantDestinations := []string{"/mnt/user-data/outputs", "/mnt/session/uploads", "/mnt/transcripts", "/mnt/user-data/tool_results", "/root/.claude/skills"}
	wantCaches := []float64{3600, 1, 10, 3, 60}
	for index, mount := range got.Mounts[:4] {
		if mount.Source != wantSources[index] || mount.Destination != wantDestinations[index] || mount.CacheDurationSeconds != wantCaches[index] {
			t.Fatalf("mount %d = %+v", index, mount)
		}
		if mount.FilesystemID != filesystemID || mount.UID != 999 || mount.GID != 1000 ||
			mount.DirectoryPermissions != "0755" || mount.FilePermissions != "0644" ||
			mount.VFSCacheMode != "full" || mount.VFSCacheMaxSize != "1G" {
			t.Fatalf("mount %d has unexpected fixed settings: %+v", index, mount)
		}
		wantReadonly := index != 0
		wantToken := readonly
		if index == 0 {
			wantToken = readWrite
		}
		if mount.Readonly != wantReadonly || mount.AuthToken != wantToken {
			t.Fatalf("mount %d authority = readonly:%t token:%q", index, mount.Readonly, mount.AuthToken)
		}
	}
	skills := got.Mounts[4]
	if skills.Source != wantSources[4] || skills.Destination != wantDestinations[4] ||
		skills.CacheDurationSeconds != wantCaches[4] || !skills.Readonly || skills.AuthToken != readonly ||
		skills.UID != 999 || skills.GID != 1000 || skills.DirectoryPermissions != "0755" ||
		skills.FilePermissions != "0644" {
		t.Fatalf("skills mount = %+v", skills)
	}
}

func TestRcloneReadyProbeContract(t *testing.T) {
	if rcloneReadyPollInterval != 200*time.Millisecond {
		t.Fatalf("rcloneReadyPollInterval = %s, want 200ms", rcloneReadyPollInterval)
	}
	if rcloneReadyTimeout != 20*time.Second {
		t.Fatalf("rcloneReadyTimeout = %s, want 20s", rcloneReadyTimeout)
	}
}

func TestRcloneCommandsKeepTokensOutOfCommandText(t *testing.T) {
	const secret = "filestore-secret-token"
	configPayload, err := json.Marshal(buildRcloneMultimountConfig("fs_test", "http://service.test", secret, secret, nil))
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	start := rcloneStartCommand()
	permissions := rcloneConfigPermissionsCommand()
	cleanup := rcloneConfigCleanupCommand()
	if strings.Contains(start+permissions+cleanup, secret) {
		t.Fatal("rclone command text contains token")
	}
	if !strings.Contains(string(configPayload), secret) {
		t.Fatal("rclone file config does not contain expected token")
	}
	for _, want := range []string{"/opt/rclone/rclone-filestore", "multimount --config"} {
		if !strings.Contains(start, want) {
			t.Fatalf("rclone start command missing %q:\n%s", want, start)
		}
	}
	for _, removedBootstrap := range []string{"cat >", "trap ", "umask ", "exec "} {
		if strings.Contains(start, removedBootstrap) {
			t.Fatalf("rclone start command still contains bootstrap %q:\n%s", removedBootstrap, start)
		}
	}
	if permissions != "chmod 0600 '/tmp/rclone-mount-config.json'" {
		t.Fatalf("rclone permissions command = %q", permissions)
	}
}

func TestRunSandboxCommandReportsBoundedOutput(t *testing.T) {
	provider := &rcloneTestProvider{runResult: e2bruntime.CommandResult{
		ExitCode: 17,
		Stdout:   []byte("readiness probe output"),
		Stderr:   []byte(strings.Repeat("x", 2049)),
	}}
	runner := &Runner{provider: provider}
	err := runner.runSandboxCommand(context.Background(), "sandbox_test", "probe", time.Second)
	if err == nil || !strings.Contains(err.Error(), `stdout="readiness probe output"`) ||
		!strings.Contains(err.Error(), `stderr="`+strings.Repeat("x", 2048)+`...[truncated]"`) {
		t.Fatalf("runSandboxCommand() error = %v, want bounded stdout and stderr", err)
	}
}

func TestStartRcloneFilestoreFailures(t *testing.T) {
	const secretMarker = "provider-secret-marker"
	providerFailure := errors.New("provider failed with " + secretMarker)
	tests := []struct {
		name            string
		provider        *rcloneTestProvider
		wantError       error
		wantRunCalls    int
		wantLaunchCalls int
	}{
		{
			name:         "write",
			provider:     &rcloneTestProvider{writeErr: providerFailure},
			wantError:    errRcloneConfigWrite,
			wantRunCalls: 1,
		},
		{
			name:         "permissions",
			provider:     &rcloneTestProvider{runErrors: []error{providerFailure, nil}},
			wantError:    errRcloneConfigPermissions,
			wantRunCalls: 2,
		},
		{
			name:            "start",
			provider:        &rcloneTestProvider{backgroundErr: providerFailure},
			wantError:       errRcloneProcessStart,
			wantRunCalls:    2,
			wantLaunchCalls: 1,
		},
		{
			name:            "ready",
			provider:        &rcloneTestProvider{fileExistsErr: providerFailure},
			wantError:       errRcloneReadiness,
			wantRunCalls:    2,
			wantLaunchCalls: 1,
		},
		{
			name:            "cleanup retries without failing ready sandbox",
			provider:        &rcloneTestProvider{ready: true, runErrors: []error{nil, providerFailure, providerFailure, providerFailure}},
			wantRunCalls:    4,
			wantLaunchCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &Runner{
				provider: test.provider,
				logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			err := runner.startRcloneFilestore(context.Background(), "sandbox_test", rcloneFilestoreLaunch{
				ConfigPayload: []byte(`{"mounts":[]}`),
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("startRcloneFilestore() error = %v, want %v", err, test.wantError)
			}
			if err != nil && (strings.Contains(err.Error(), secretMarker) || errors.Is(err, providerFailure)) {
				t.Fatalf("startRcloneFilestore() leaked provider error: %v", err)
			}
			if len(test.provider.runCommands) != test.wantRunCalls {
				t.Fatalf("RunCommand calls = %d, want %d", len(test.provider.runCommands), test.wantRunCalls)
			}
			if test.provider.backgroundCalls != test.wantLaunchCalls {
				t.Fatalf("StartBackgroundCommand calls = %d, want %d", test.provider.backgroundCalls, test.wantLaunchCalls)
			}
		})
	}
}

func TestStartManagedAgentSessionFilesystemWritesLocalMemoryMarkdown(t *testing.T) {
	const secretMarker = "provider-secret-marker"
	providerFailure := errors.New("provider failed with " + secretMarker)
	mounts := []memoryRuntimeMount{{
		Name:         "user-preferences",
		Description:  "个人对西餐的喜好",
		Instructions: "问饮食或语言先读此目录",
		Access:       "read_write",
		MountPath:    "/mnt/memory/user-preferences",
		Slug:         "user-preferences",
	}}
	configPayload, err := json.Marshal(buildRcloneMultimountConfig("fs_test", "http://service.test", "rw", "ro", mounts))
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	launch := rcloneFilestoreLaunch{ConfigPayload: configPayload, MemoryMounts: mounts}

	t.Run("writes mkdir and markdown after rclone ready", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		if err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", launch); err != nil {
			t.Fatalf("startManagedAgentSessionFilesystem() error = %v", err)
		}
		if len(provider.runCommands) == 0 || provider.runCommands[0] != memoryRootMkdirCommand() {
			t.Fatalf("commands = %#v, want mkdir first", provider.runCommands)
		}
		if got := provider.fileData(memoryMarkdownSandboxPath); got != renderMemoryMarkdown(mounts) {
			t.Fatalf("MEMORY.md = %q", got)
		}
		if provider.fileData(rcloneConfigPath) == "" {
			t.Fatal("missing rclone config write")
		}
		for _, command := range provider.runCommands {
			if strings.Contains(command, "cp ") || strings.Contains(command, "rclone copy") {
				t.Fatalf("startup copied store files: %s", command)
			}
		}
	})

	t.Run("skips local memory root without stores", func(t *testing.T) {
		provider := &rcloneTestProvider{ready: true}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		emptyLaunch := rcloneFilestoreLaunch{ConfigPayload: []byte(`{"mounts":[]}`)}
		if err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", emptyLaunch); err != nil {
			t.Fatalf("startManagedAgentSessionFilesystem() error = %v", err)
		}
		for _, command := range provider.runCommands {
			if strings.Contains(command, "/mnt/memory") {
				t.Fatalf("created /mnt/memory without stores: %s", command)
			}
		}
		if provider.fileData(memoryMarkdownSandboxPath) != "" {
			t.Fatal("wrote MEMORY.md without stores")
		}
	})

	t.Run("markdown write failure is fail-closed", func(t *testing.T) {
		provider := &rcloneTestProvider{
			ready: true,
			writeErrByPath: map[string]error{
				memoryMarkdownSandboxPath: providerFailure,
			},
		}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", launch)
		if !errors.Is(err, errMemoryMarkdownWrite) {
			t.Fatalf("error = %v, want %v", err, errMemoryMarkdownWrite)
		}
		if strings.Contains(err.Error(), secretMarker) {
			t.Fatalf("leaked provider error: %v", err)
		}
	})

	t.Run("mkdir failure is fail-closed before rclone", func(t *testing.T) {
		provider := &rcloneTestProvider{runErrors: []error{providerFailure}}
		runner := &Runner{provider: provider, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
		err := runner.startManagedAgentSessionFilesystem(context.Background(), "sandbox_test", launch)
		if !errors.Is(err, errMemoryRootCreate) {
			t.Fatalf("error = %v, want %v", err, errMemoryRootCreate)
		}
		if provider.fileData(rcloneConfigPath) != "" {
			t.Fatal("rclone config was written after mkdir failure")
		}
	})
}

func TestWaitForRcloneReady(t *testing.T) {
	t.Run("polls until ready", func(t *testing.T) {
		provider := &rcloneTestProvider{
			readySequence: []bool{false, false, true},
		}
		runner := &Runner{provider: provider}
		if err := runner.waitForRcloneReady(context.Background(), "sandbox_test", time.Millisecond, time.Second); err != nil {
			t.Fatalf("waitForRcloneReady() error = %v", err)
		}
		if provider.fileExistsCalls != 3 {
			t.Fatalf("file probe calls = %d, want 3", provider.fileExistsCalls)
		}
	})

	t.Run("times out", func(t *testing.T) {
		provider := &rcloneTestProvider{}
		runner := &Runner{provider: provider}
		err := runner.waitForRcloneReady(context.Background(), "sandbox_test", time.Millisecond, 5*time.Millisecond)
		if err == nil || !strings.Contains(err.Error(), "timed out after 5ms") {
			t.Fatalf("waitForRcloneReady() error = %v, want timeout", err)
		}
	})
}

type rcloneTestProvider struct {
	writeErr        error
	writeErrByPath  map[string]error
	fileExistsErr   error
	backgroundErr   error
	backgroundCalls int
	fileExistsCalls int
	ready           bool
	readySequence   []bool
	runErrors       []error
	runResult       e2bruntime.CommandResult
	runCommands     []string
	writePath       string
	writeData       []byte
	writes          map[string][]byte
	configExists    bool
}

func (*rcloneTestProvider) Create(context.Context, db.Environment, *db.EnvironmentWork, e2bruntime.Resolution) (e2bruntime.Sandbox, error) {
	panic("unexpected Create call")
}

func (*rcloneTestProvider) Kill(context.Context, string) error {
	panic("unexpected Kill call")
}

func (*rcloneTestProvider) Resolve(db.Environment, *db.EnvironmentWork) (e2bruntime.Resolution, error) {
	panic("unexpected Resolve call")
}

func (p *rcloneTestProvider) WriteFile(_ context.Context, _ string, path string, data []byte) error {
	p.writePath = path
	p.writeData = append([]byte(nil), data...)
	if p.writes == nil {
		p.writes = map[string][]byte{}
	}
	p.writes[path] = append([]byte(nil), data...)
	if err := p.writeErrByPath[path]; err != nil {
		return err
	}
	if p.writeErr == nil && path == rcloneConfigPath {
		p.configExists = true
	}
	return p.writeErr
}

func (p *rcloneTestProvider) fileData(path string) string {
	if p == nil || p.writes == nil {
		return ""
	}
	return string(p.writes[path])
}

func (p *rcloneTestProvider) FileExists(_ context.Context, _ string, path string) (bool, error) {
	p.fileExistsCalls++
	if p.fileExistsErr != nil {
		return false, p.fileExistsErr
	}
	if path == rcloneConfigPath {
		return p.configExists, nil
	}
	index := p.fileExistsCalls - 1
	if index < len(p.readySequence) {
		return p.readySequence[index], nil
	}
	return p.ready, nil
}

func (p *rcloneTestProvider) RunCommand(_ context.Context, _ string, request e2bruntime.CommandRequest) (e2bruntime.CommandResult, error) {
	p.runCommands = append(p.runCommands, request.Command)
	index := len(p.runCommands) - 1
	if index < len(p.runErrors) {
		if p.runErrors[index] != nil {
			return e2bruntime.CommandResult{}, p.runErrors[index]
		}
	}
	if request.Command == rcloneConfigCleanupCommand() {
		p.configExists = false
	}
	return p.runResult, nil
}

func (p *rcloneTestProvider) StartBackgroundCommand(context.Context, string, string, []byte) error {
	p.backgroundCalls++
	return p.backgroundErr
}
