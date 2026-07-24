package environments

import (
	"context"
	"encoding/json"
	"errors"
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
	got := buildRcloneMultimountConfig(filesystemID, serviceURL, readWrite, readonly)
	if got.ReadyFile != rcloneReadyPath || got.StateDir != rcloneStateDirectory || got.ServiceURL != "http://host.docker.internal:38080" {
		t.Fatalf("unexpected multimount envelope: %+v", got)
	}
	if len(got.Mounts) != 4 {
		t.Fatalf("mount count = %d, want 4", len(got.Mounts))
	}
	wantSources := []string{"/outputs", "/uploads", "/transcripts", "/tool_results"}
	wantDestinations := []string{"/mnt/user-data/outputs", "/mnt/session/uploads", "/mnt/transcripts", "/mnt/user-data/tool_results"}
	wantCaches := []float64{3600, 1, 10, 3}
	for index, mount := range got.Mounts {
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
	configPayload, err := json.Marshal(buildRcloneMultimountConfig("fs_test", "http://service.test", secret, secret))
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
			name:            "cleanup",
			provider:        &rcloneTestProvider{ready: true, runErrors: []error{nil, providerFailure}},
			wantError:       errRcloneConfigCleanup,
			wantRunCalls:    2,
			wantLaunchCalls: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &Runner{
				provider: test.provider,
			}
			err := runner.startRcloneFilestore(context.Background(), "sandbox_test", rcloneFilestoreLaunch{
				ConfigPayload: []byte(`{"mounts":[]}`),
			})
			if !errors.Is(err, test.wantError) {
				t.Fatalf("startRcloneFilestore() error = %v, want %v", err, test.wantError)
			}
			if strings.Contains(err.Error(), secretMarker) || errors.Is(err, providerFailure) {
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
	fileExistsErr   error
	backgroundErr   error
	backgroundCalls int
	fileExistsCalls int
	ready           bool
	readySequence   []bool
	runErrors       []error
	runCommands     []string
	writePath       string
	writeData       []byte
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
	return p.writeErr
}

func (p *rcloneTestProvider) FileExists(context.Context, string, string) (bool, error) {
	p.fileExistsCalls++
	if p.fileExistsErr != nil {
		return false, p.fileExistsErr
	}
	index := p.fileExistsCalls - 1
	if index < len(p.readySequence) {
		return p.readySequence[index], nil
	}
	return p.ready, nil
}

func (p *rcloneTestProvider) RunCommand(_ context.Context, _ string, command string, _ time.Duration) error {
	p.runCommands = append(p.runCommands, command)
	index := len(p.runCommands) - 1
	if index < len(p.runErrors) {
		return p.runErrors[index]
	}
	return nil
}

func (p *rcloneTestProvider) StartBackgroundCommand(context.Context, string, string, []byte) error {
	p.backgroundCalls++
	return p.backgroundErr
}
