package e2bruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

func TestConnectionOptsFromConfigMapsAllFields(t *testing.T) {
	cfg := config.E2BConfig{
		APIKey:         "api-key",
		AccessToken:    "access-token",
		Domain:         "e2b.example.test",
		APIURL:         "https://api.e2b.example.test",
		SandboxURL:     "https://sandbox.e2b.example.test",
		Debug:          true,
		RequestTimeout: 23 * time.Second,
	}

	got := ConnectionOptsFromConfig(cfg)
	if got.ApiKey != cfg.APIKey || got.AccessToken != cfg.AccessToken || got.Domain != cfg.Domain || got.ApiUrl != cfg.APIURL || got.SandboxUrl != cfg.SandboxURL {
		t.Fatalf("ConnectionOptsFromConfig() = %#v, want all connection fields from E2BConfig", got)
	}
	if got.Debug == nil || !*got.Debug {
		t.Fatalf("ConnectionOptsFromConfig().Debug = %v, want true", got.Debug)
	}
	wantTimeoutMs := int(cfg.RequestTimeout / time.Millisecond)
	if got.RequestTimeoutMs == nil || *got.RequestTimeoutMs != wantTimeoutMs {
		t.Fatalf("ConnectionOptsFromConfig().RequestTimeoutMs = %v, want %d", got.RequestTimeoutMs, wantTimeoutMs)
	}
}

func TestExecuteCommandTransport(t *testing.T) {
	t.Run("send failure kills without closing or waiting", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.sendErr = errors.New("forced send failure")
		_, err := executeCommand(context.Background(), CommandRequest{
			Command: "environment-manager provision-packages --protocol v1 --stdin",
			Stdin:   []byte(`{"version":1}`),
			Timeout: time.Second,
		}, process.start)
		if err == nil || !strings.Contains(err.Error(), "send sandbox command stdin") {
			t.Fatalf("executeCommand() error = %v, want send failure", err)
		}
		assertCommandOperations(t, process.recordedOperations(), "start", "send", "kill", "disconnect")
	})

	t.Run("close failure kills without waiting", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.closeErr = errors.New("forced close failure")
		_, err := executeCommand(context.Background(), CommandRequest{
			Command: "environment-manager provision-packages --protocol v1 --stdin",
			Stdin:   []byte(`{"version":1}`),
			Timeout: time.Second,
		}, process.start)
		if err == nil || !strings.Contains(err.Error(), "close sandbox command stdin") {
			t.Fatalf("executeCommand() error = %v, want close failure", err)
		}
		assertCommandOperations(t, process.recordedOperations(), "start", "send", "close", "kill", "disconnect")
	})

	t.Run("timeout kills a waiting process", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.blockWait = true
		_, err := executeCommand(context.Background(), CommandRequest{
			Command: "environment-manager provision-packages --protocol v1 --stdin",
			Stdin:   []byte(`{"version":1}`),
			Timeout: 10 * time.Millisecond,
		}, process.start)
		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
			t.Fatalf("executeCommand() error = %v, want deadline", err)
		}
		assertCommandOperations(t, process.recordedOperations(), "start", "send", "close", "wait", "kill", "disconnect")
	})

	t.Run("nonzero exit is a protocol result rather than a transport error", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.result = CommandResult{ExitCode: 10, Stdout: []byte(`{"status":"failed"}`), Stderr: []byte("bounded diagnostic")}
		got, err := executeCommand(context.Background(), CommandRequest{
			Command: "environment-manager provision-packages --protocol v1 --stdin",
			Stdin:   []byte(`{"version":1}`),
			Timeout: time.Second,
		}, process.start)
		if err != nil || !reflect.DeepEqual(got, process.result) {
			t.Fatalf("executeCommand() = (%#v, %v), want (%#v, nil)", got, err, process.result)
		}
		assertCommandOperations(t, process.recordedOperations(), "start", "send", "close", "wait", "disconnect")
	})

	t.Run("success sends stdin then closes and waits", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.result = CommandResult{Stdout: []byte(`{"status":"succeeded"}`)}
		request := CommandRequest{
			Command: "environment-manager provision-packages --protocol v1 --stdin",
			Stdin:   []byte(`{"version":1}`),
			Timeout: time.Second,
		}
		got, err := executeCommand(context.Background(), request, process.start)
		if err != nil || !reflect.DeepEqual(got, process.result) {
			t.Fatalf("executeCommand() = (%#v, %v), want (%#v, nil)", got, err, process.result)
		}
		if !reflect.DeepEqual(process.stdin, request.Stdin) {
			t.Fatalf("stdin = %q, want %q", process.stdin, request.Stdin)
		}
		assertCommandOperations(t, process.recordedOperations(), "start", "send", "close", "wait", "disconnect")
	})
}

type recordingCommandProcess struct {
	mu         sync.Mutex
	operations []string
	stdin      []byte
	result     CommandResult
	sendErr    error
	closeErr   error
	waitErr    error
	blockWait  bool
	waitDone   chan struct{}
}

func newRecordingCommandProcess() *recordingCommandProcess {
	return &recordingCommandProcess{waitDone: make(chan struct{})}
}

func (p *recordingCommandProcess) start(_ context.Context, _ CommandRequest) (commandProcess, error) {
	p.record("start")
	return p, nil
}

func (p *recordingCommandProcess) SendStdin(_ context.Context, stdin []byte) error {
	p.record("send")
	p.stdin = append([]byte(nil), stdin...)
	return p.sendErr
}

func (p *recordingCommandProcess) CloseStdin(_ context.Context) error {
	p.record("close")
	return p.closeErr
}

func (p *recordingCommandProcess) Wait() (CommandResult, error) {
	p.record("wait")
	if p.blockWait {
		<-p.waitDone
	}
	return p.result, p.waitErr
}

func (p *recordingCommandProcess) Kill(context.Context) error {
	p.record("kill")
	select {
	case <-p.waitDone:
	default:
		close(p.waitDone)
	}
	return nil
}

func (p *recordingCommandProcess) Disconnect() {
	p.record("disconnect")
}

func (p *recordingCommandProcess) record(operation string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.operations = append(p.operations, operation)
}

func (p *recordingCommandProcess) recordedOperations() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.operations...)
}

func assertCommandOperations(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("command operations = %#v, want %#v", got, want)
	}
}

func TestSandboxVolumeMountsOnlyIncludeUserData(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.E2BConfig
	}{
		{name: "hosted", cfg: config.E2BConfig{Domain: "e2b.example.test"}},
		{name: "local endpoint", cfg: config.E2BConfig{APIURL: "http://127.0.0.1:3000"}},
		{name: "debug", cfg: config.E2BConfig{Debug: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewProvider(tt.cfg)
			mounts := provider.sandboxVolumeMounts(nil)
			if got := mounts[sandboxUserDataMountPath]; got != sandboxUserDataVolumeName {
				t.Fatalf("mount %s = %v, want %s", sandboxUserDataMountPath, got, sandboxUserDataVolumeName)
			}
			if len(mounts) != 1 {
				t.Fatalf("mounts = %#v, want only user-data", mounts)
			}
		})
	}
}

func TestResolveUsesManagedAgentSandboxTagByDefault(t *testing.T) {
	resolution, err := NewProvider(config.E2BConfig{}).Resolve(db.Environment{
		ExternalID:  "env_default_template",
		WorkspaceID: 42,
		Config:      json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`),
	}, nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Template != config.DefaultE2BTemplate {
		t.Fatalf("template = %q, want %q", resolution.Template, config.DefaultE2BTemplate)
	}
}

func TestSandboxVolumeMountsIncludesManagedAgentSkills(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	work := &db.EnvironmentWork{
		Metadata: json.RawMessage(`{"managed_agent_skills_mount":{"mount_path":"/mnt/skills","volume_name":"managed-agent-skills-test","manifest_sha256":"abc123"}}`),
	}

	mounts := provider.sandboxVolumeMounts(work)
	if got := mounts[sandboxUserDataMountPath]; got != sandboxUserDataVolumeName {
		t.Fatalf("mount %s = %v, want %s", sandboxUserDataMountPath, got, sandboxUserDataVolumeName)
	}
	if got := mounts[SandboxSkillsMountPath]; got != "managed-agent-skills-test" {
		t.Fatalf("mount %s = %v, want managed-agent-skills-test", SandboxSkillsMountPath, got)
	}
	if len(mounts) != 2 {
		t.Fatalf("mounts = %#v, want user-data plus skills", mounts)
	}
}

func TestResolveDoesNotProjectLimitedNetworkIntoSessionSandbox(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	resolution, err := provider.Resolve(db.Environment{
		ExternalID:  "env_limited_network",
		WorkspaceID: 42,
		Config:      json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true,"allow_package_managers":true}}`),
	}, &db.EnvironmentWork{Metadata: json.RawMessage(`{"mcp_allowed_hosts":["mcp.example.com"]}`)})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !resolution.AllowInternetAccess {
		t.Fatal("limited Environment was projected into the Session Sandbox")
	}
	if resolution.Network != nil {
		t.Fatalf("Session Sandbox NetworkOpts = %#v, want nil during per-Session provisioning", resolution.Network)
	}
}

func TestSkillMountVolumeNameUsesFullManifestHash(t *testing.T) {
	hash := strings.Repeat("a", 64)
	got := skillMountVolumeName(hash)
	want := skillMountVolumePrefix + hash
	if got != want {
		t.Fatalf("skillMountVolumeName = %q, want %q", got, want)
	}
}

func TestPrepareSkillMountReusesOnlyMatchingReadyMarker(t *testing.T) {
	archive := []byte("skill archive")
	sum := sha256.Sum256(archive)
	sha := fmt.Sprintf("%x", sum[:])
	runtimeSkills := []skillsapi.RuntimeSkill{{
		Source:    "custom",
		SkillID:   "skill_1",
		Version:   "1",
		Directory: "skill-one",
		SHA256:    sha,
		SizeBytes: int64(len(archive)),
		Archive:   archive,
	}}
	_, _, manifestSHA256, err := skillsapi.BuildMountManifest(runtimeSkills)
	if err != nil {
		t.Fatalf("BuildMountManifest: %v", err)
	}
	volumeName := skillMountVolumeName(manifestSHA256)

	t.Run("matching ready marker reuses without writes", func(t *testing.T) {
		var writes []string
		server := newFakeE2BVolumeServer(t, volumeName, manifestSHA256, &writes)
		defer server.Close()

		provider := NewProvider(config.E2BConfig{
			APIKey: "e2b_0000000000000000000000000000000000000000",
			APIURL: server.URL,
		})
		metadataOnly := append([]skillsapi.RuntimeSkill(nil), runtimeSkills...)
		metadataOnly[0].Archive = nil
		mount, err := provider.PrepareSkillMount(context.Background(), metadataOnly)
		if err != nil {
			t.Fatalf("PrepareSkillMount: %v", err)
		}
		if mount.VolumeName != volumeName || mount.ManifestSHA256 != manifestSHA256 {
			t.Fatalf("mount = %#v, want volume=%s manifest=%s", mount, volumeName, manifestSHA256)
		}
		if len(writes) != 0 {
			t.Fatalf("writes = %#v, want none for matching ready marker", writes)
		}
	})

	t.Run("mismatched ready marker rewrites volume", func(t *testing.T) {
		var writes []string
		server := newFakeE2BVolumeServer(t, volumeName, "stale-ready-marker", &writes)
		defer server.Close()

		provider := NewProvider(config.E2BConfig{
			APIKey: "e2b_0000000000000000000000000000000000000000",
			APIURL: server.URL,
		})
		mount, err := provider.PrepareSkillMount(context.Background(), runtimeSkills)
		if err != nil {
			t.Fatalf("PrepareSkillMount: %v", err)
		}
		if mount.VolumeName != volumeName || mount.ManifestSHA256 != manifestSHA256 {
			t.Fatalf("mount = %#v, want volume=%s manifest=%s", mount, volumeName, manifestSHA256)
		}
		if len(writes) != 3 {
			t.Fatalf("writes = %#v, want manifest, archive, ready", writes)
		}
		if writes[len(writes)-1] != skillMountReadyPath+"="+manifestSHA256+"\n" {
			t.Fatalf("ready write = %q, want manifest hash", writes[len(writes)-1])
		}
	})
}

func newFakeE2BVolumeServer(t *testing.T, volumeName string, readyMarker string, writes *[]string) *httptest.Server {
	t.Helper()
	const volumeID = "vol-skills-test"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/volumes":
			_, _ = w.Write([]byte(`[{"volumeID":"` + volumeID + `","name":"` + volumeName + `"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/volumes/"+volumeID:
			_, _ = w.Write([]byte(`{"volumeID":"` + volumeID + `","name":"` + volumeName + `","token":"volume-token"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/volumecontent/"+volumeID+"/path" && r.URL.Query().Get("path") == skillMountReadyPath:
			_, _ = w.Write([]byte(`{"type":"file","name":"` + skillMountReadyPath + `","path":"` + skillMountReadyPath + `","size":1}`))
		case r.Method == http.MethodGet && r.URL.Path == "/volumecontent/"+volumeID+"/file" && r.URL.Query().Get("path") == skillMountReadyPath:
			_, _ = w.Write([]byte(readyMarker))
		case r.Method == http.MethodPut && r.URL.Path == "/volumecontent/"+volumeID+"/file":
			path := r.URL.Query().Get("path")
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read write body: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			*writes = append(*writes, path+"="+string(data))
			_, _ = w.Write([]byte(`{"type":"file","name":"` + path + `","path":"` + path + `","size":1}`))
		default:
			t.Errorf("unexpected E2B request: %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}
