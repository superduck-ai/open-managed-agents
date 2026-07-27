package e2bruntime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
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

	t.Run("timeout returns when kill does not release wait", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.blockWait = true
		process.killDoesNotUnblockWait = true
		startedAt := time.Now()
		_, err := executeCommand(context.Background(), CommandRequest{
			Command: "environment-manager provision-packages --protocol v1 --stdin",
			Stdin:   []byte(`{"version":1}`),
			Timeout: 10 * time.Millisecond,
		}, process.start)
		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
			t.Fatalf("executeCommand() error = %v, want deadline", err)
		}
		if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
			t.Fatalf("executeCommand() blocked for %s after timeout", elapsed)
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

	t.Run("command without stdin starts and waits without opening stdin", func(t *testing.T) {
		process := newRecordingCommandProcess()
		got, err := executeCommand(context.Background(), CommandRequest{
			Command: "chmod 0600 /tmp/rclone-mount-config.json",
			Timeout: time.Second,
		}, process.start)
		if err != nil || !reflect.DeepEqual(got, process.result) {
			t.Fatalf("executeCommand() = (%#v, %v), want (%#v, nil)", got, err, process.result)
		}
		assertCommandOperations(t, process.recordedOperations(), "start", "wait", "disconnect")
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
	mu                     sync.Mutex
	operations             []string
	stdin                  []byte
	result                 CommandResult
	sendErr                error
	closeErr               error
	waitErr                error
	blockWait              bool
	killDoesNotUnblockWait bool
	waitDone               chan struct{}
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
	if p.killDoesNotUnblockWait {
		return nil
	}
	p.releaseWait()
	return nil
}

func (p *recordingCommandProcess) Disconnect() {
	p.record("disconnect")
	p.releaseWait()
}

func (p *recordingCommandProcess) releaseWait() {
	select {
	case <-p.waitDone:
	default:
		close(p.waitDone)
	}
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

func TestResolveLimitedNetworkFailsClosedOnInvalidAllowedHost(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	_, err := provider.Resolve(db.Environment{
		ExternalID:       "env_invalid_network",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["bad/path","api.example.com"]}}`),
		ResolvedTemplate: "template_test",
	}, nil)
	if err == nil {
		t.Fatal("invalid allowed_hosts policy must fail closed")
	}
}

func TestResolveLimitedNetworkFailsClosedOnMalformedMCPMetadata(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	_, err := provider.Resolve(db.Environment{
		ExternalID:       "env_invalid_mcp_metadata",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":true}}`),
		ResolvedTemplate: "template_test",
	}, &db.EnvironmentWork{
		ExternalID: "work_invalid_mcp_metadata",
		Metadata:   json.RawMessage(`{"mcp_allowed_hosts":["mcp.example.com",42]}`),
	})
	if err == nil {
		t.Fatal("malformed mcp_allowed_hosts metadata must fail closed")
	}
}

func TestResolveLimitedNetworkCanonicalizesExplicitAllowedHosts(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	resolution, err := provider.Resolve(db.Environment{
		ExternalID:  "env_canonical_network",
		WorkspaceID: 42,
		Config: json.RawMessage(`{
			"type":"cloud",
			"networking":{
				"type":"limited",
				"allowed_hosts":["例子.测试","API.Example.COM.","::ffff:192.0.2.1","*.例子.测试","[2606:4700:4700::1111]:443","Example.com:8443"]
			}
		}`),
	}, nil)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	want := []string{
		"xn--fsqu00a.xn--0zwm56d",
		"api.example.com",
		"192.0.2.1",
		"*.xn--fsqu00a.xn--0zwm56d",
		"[2606:4700:4700::1111]:443",
		"example.com:8443",
	}
	if resolution.Network == nil || !reflect.DeepEqual(resolution.Network.AllowOut, want) {
		t.Fatalf("AllowOut = %#v, want %#v", resolution.Network, want)
	}
}

func TestResolveLimitedNetworkIncludesMCPHostsWhenAllowed(t *testing.T) {
	provider := NewProvider(config.E2BConfig{})
	env := db.Environment{
		ExternalID:       "env_test",
		WorkspaceID:      42,
		Config:           json.RawMessage(`{"type":"cloud","networking":{"type":"limited","allowed_hosts":["api.example.com"],"allow_mcp_servers":true}}`),
		ResolvedTemplate: "template_test",
	}
	work := &db.EnvironmentWork{
		ExternalID: "work_test",
		Metadata:   json.RawMessage(`{"mcp_allowed_hosts":["mcp.notion.com","api.githubcopilot.com","mcp.notion.com"]}`),
	}

	resolution, err := provider.Resolve(env, work)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.AllowInternetAccess {
		t.Fatal("limited network should disable unrestricted internet")
	}
	if resolution.Network == nil {
		t.Fatal("expected network options")
	}
	want := []string{"api.example.com", "mcp.notion.com", "api.githubcopilot.com"}
	if !reflect.DeepEqual(resolution.Network.AllowOut, want) {
		t.Fatalf("AllowOut = %#v, want %#v", resolution.Network.AllowOut, want)
	}
}
