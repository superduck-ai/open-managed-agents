package e2bruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestSetTimeoutUsesConfiguredConnectTimeoutAndRequestedRenewal(t *testing.T) {
	var paths []string
	var timeouts []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		var body struct {
			Timeout int `json:"timeout"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode %s request: %v", r.URL.Path, err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		timeouts = append(timeouts, body.Timeout)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/sandboxes/sbx_timeout/connect" {
			_, _ = fmt.Fprint(w, `{"sandboxID":"sbx_timeout","envdURL":"http://127.0.0.1:1"}`)
			return
		}
		if r.URL.Path == "/sandboxes/sbx_timeout/timeout" {
			_, _ = fmt.Fprint(w, `{}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	provider := NewProvider(config.E2BConfig{
		APIKey:         "e2b_0000000000000000000000000000000000000000",
		APIURL:         server.URL,
		SandboxURL:     server.URL,
		RequestTimeout: time.Second,
		SandboxTimeout: 7 * time.Minute,
	})
	if err := provider.SetTimeout(context.Background(), "sbx_timeout", 3*time.Minute); err != nil {
		t.Fatalf("SetTimeout() error = %v", err)
	}
	wantPaths := []string{"/sandboxes/sbx_timeout/connect", "/sandboxes/sbx_timeout/timeout"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("request paths = %#v, want %#v", paths, wantPaths)
	}
	if want := []int{420, 180}; !reflect.DeepEqual(timeouts, want) {
		t.Fatalf("request timeouts = %#v, want %#v", timeouts, want)
	}
}

func TestExecuteCommandTransport(t *testing.T) {
	stdin := []byte(`{"version":1}`)
	results := []struct {
		name   string
		result CommandResult
	}{
		{"success", CommandResult{Stdout: []byte(`{"status":"succeeded"}`)}},
		{"nonzero exit", CommandResult{ExitCode: 10, Stderr: []byte("diagnostic")}},
	}
	for _, tt := range results {
		t.Run(tt.name, func(t *testing.T) {
			process := newRecordingCommandProcess()
			process.result = tt.result
			got, err := executeCommand(context.Background(), CommandRequest{Command: "provision --stdin", Stdin: stdin, Timeout: time.Second}, process.start)
			if err != nil || !reflect.DeepEqual(got, tt.result) {
				t.Fatalf("executeCommand() = (%#v, %v), want (%#v, nil)", got, err, tt.result)
			}
			if !reflect.DeepEqual(process.stdin, stdin) || process.sendOrder > process.waitOrder || process.closeOrder > process.waitOrder {
				t.Fatalf("stdin/order = (%q, %d, %d, %d), want send and close before wait", process.stdin, process.sendOrder, process.closeOrder, process.waitOrder)
			}
		})
	}

	for _, tt := range []struct {
		name, want string
		configure  func(*recordingCommandProcess)
	}{
		{"send failure", "send sandbox command stdin", func(p *recordingCommandProcess) { p.sendErr = errors.New("send") }},
		{"close failure", "close sandbox command stdin", func(p *recordingCommandProcess) { p.closeErr = errors.New("close") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			process := newRecordingCommandProcess()
			tt.configure(process)
			_, err := executeCommand(context.Background(), CommandRequest{Command: "provision --stdin", Stdin: stdin, Timeout: time.Second}, process.start)
			if err == nil || !strings.Contains(err.Error(), tt.want) || process.kills != 1 {
				t.Fatalf("error/kills = (%v, %d), want %q and one kill", err, process.kills, tt.want)
			}
		})
	}

	t.Run("timeout returns and attempts kill", func(t *testing.T) {
		process := newRecordingCommandProcess()
		process.blockWait = true
		started := time.Now()
		_, err := executeCommand(context.Background(), CommandRequest{Command: "provision --stdin", Stdin: stdin, Timeout: 10 * time.Millisecond}, process.start)
		if err == nil || !strings.Contains(err.Error(), "deadline exceeded") || process.kills != 1 {
			t.Fatalf("error/kills = (%v, %d), want deadline and one kill", err, process.kills)
		}
		if time.Since(started) > 500*time.Millisecond {
			t.Fatal("executeCommand blocked after timeout")
		}
	})
}

func TestRunConnectedCommandAppliesDeadlineBeforeConnect(t *testing.T) {
	started := time.Now()
	_, err := runConnectedCommand(
		context.Background(),
		CommandRequest{Command: "provision", Timeout: 10 * time.Millisecond},
		func(ctx context.Context) (commandStarter, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runConnectedCommand() error = %v, want deadline exceeded", err)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("runConnectedCommand did not apply timeout to connect")
	}
}

type recordingCommandProcess struct {
	mu                                                sync.Mutex
	sequence, sendOrder, closeOrder, waitOrder, kills int
	stdin                                             []byte
	result                                            CommandResult
	sendErr, closeErr                                 error
	blockWait                                         bool
	waitDone                                          chan struct{}
}

func newRecordingCommandProcess() *recordingCommandProcess {
	return &recordingCommandProcess{waitDone: make(chan struct{})}
}

func (p *recordingCommandProcess) start(_ context.Context, _ CommandRequest) (commandProcess, error) {
	return p, nil
}

func (p *recordingCommandProcess) SendStdin(_ context.Context, stdin []byte) error {
	p.sendOrder = p.record(false)
	p.stdin = append([]byte(nil), stdin...)
	return p.sendErr
}

func (p *recordingCommandProcess) CloseStdin(_ context.Context) error {
	p.closeOrder = p.record(false)
	return p.closeErr
}

func (p *recordingCommandProcess) Wait() (CommandResult, error) {
	p.waitOrder = p.record(false)
	if p.blockWait {
		<-p.waitDone
	}
	return p.result, nil
}

func (p *recordingCommandProcess) Kill(context.Context) error {
	p.record(true)
	return nil
}

func (p *recordingCommandProcess) Disconnect() {
	p.releaseWait()
}

func (p *recordingCommandProcess) releaseWait() {
	select {
	case <-p.waitDone:
	default:
		close(p.waitDone)
	}
}

func (p *recordingCommandProcess) record(kill bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sequence++
	if kill {
		p.kills++
	}
	return p.sequence
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
		t.Fatalf("limited network should disable unrestricted internet")
	}
	if resolution.Network == nil {
		t.Fatalf("expected network options")
	}
	want := []string{"api.example.com", "mcp.notion.com", "api.githubcopilot.com"}
	if !reflect.DeepEqual(resolution.Network.AllowOut, want) {
		t.Fatalf("AllowOut = %#v, want %#v", resolution.Network.AllowOut, want)
	}
}
