//go:build integration

package tests

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

func TestEgressConnectorSessionHeader(t *testing.T) {
	binary := os.Getenv("TEST_TUNNEL_CLIENT_BINARY")
	if binary == "" {
		t.Skip("TEST_TUNNEL_CLIENT_BINARY is not configured")
	}
	started := time.Now()
	runEgressTunnelSessionHeader(t, binary)
	if !t.Failed() {
		t.Logf("BE_PROOF {\"stage\":\"connector_headers_received\",\"elapsed_ms\":%d}", time.Since(started).Milliseconds())
	}
}

func startEgressConnectorReceiver(t *testing.T, app *testApp, binary, tunnelID, token string) func(*testing.T, *testApp, *http.Request, string, string) egressTunnelCommand {
	t.Helper()
	var mu sync.Mutex
	var received []egressTunnelCommand
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		kind := tunnels.CommandTypeJSONRPC
		switch {
		case strings.HasPrefix(r.URL.Path, "/.well-known/"):
			kind = tunnels.CommandTypeOAuthDiscovery
		case r.Method == http.MethodDelete:
			kind = tunnels.CommandTypeSessionTermination
		}
		mu.Lock()
		received = append(received, egressTunnelCommand{CommandType: kind, Headers: r.Header.Clone(), JSONRPC: json.RawMessage(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if kind == tunnels.CommandTypeOAuthDiscovery {
			_, _ = io.WriteString(w, `{"resource":"http://`+r.Host+`/mcp","scopes_supported":["read"]}`)
		} else if r.Method == http.MethodGet || r.Method == http.MethodHead || len(body) == 0 && kind != tunnels.CommandTypeSessionTermination {
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="http://`+r.Host+`/.well-known/oauth-protected-resource/mcp"`)
			w.WriteHeader(http.StatusUnauthorized)
		} else if kind == tunnels.CommandTypeSessionTermination {
			w.WriteHeader(http.StatusOK)
		} else {
			var message struct {
				ID json.RawMessage `json:"id"`
			}
			if err := jsonv2.Unmarshal(body, &message); err != nil {
				t.Error(err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if len(message.ID) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(message.ID)+`,"result":{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"header-proof","version":"1"}}}`)
		}
	}))
	t.Cleanup(private.Close)
	logPath := filepath.Join(t.TempDir(), "connector.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "--profile-dir", t.TempDir(),
		"--control-plane.base-url", app.baseURL, "--control-plane.url-path", "/connector",
		"--control-plane.tunnel-id", tunnelID, "--control-plane.api-key", "env:OMA_TEST_CONNECTOR_TOKEN",
		"--control-plane.poll-timeout", "1s", "--health.listen-addr", "127.0.0.1:0",
		"--log.level", "warn", "--mcp.server-url", "url="+private.URL+"/mcp")
	cmd.Env = append(os.Environ(), "OMA_TEST_CONNECTOR_TOKEN="+token)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = logFile.Close()
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("connector log: %s", data)
		}
	})
	return func(t *testing.T, app *testApp, request *http.Request, _, _ string) egressTunnelCommand {
		t.Helper()
		mu.Lock()
		received = nil
		mu.Unlock()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		response, err := app.client.Do(request.Clone(ctx))
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("ingress status=%d body=%s error=%v", response.StatusCode, body, err)
		}
		kind := tunnels.CommandTypeJSONRPC
		if request.Method == http.MethodGet {
			kind = tunnels.CommandTypeOAuthDiscovery
		}
		if request.Method == http.MethodDelete {
			kind = tunnels.CommandTypeSessionTermination
		}
		mu.Lock()
		defer mu.Unlock()
		for i := len(received) - 1; i >= 0; i-- {
			if received[i].CommandType == kind {
				t.Logf("private MCP received %s session=%q", kind, received[i].Headers.Get("X-OMA-Session-ID"))
				return received[i]
			}
		}
		t.Fatal("private MCP did not receive the request")
		return egressTunnelCommand{}
	}
}
