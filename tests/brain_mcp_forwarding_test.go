//go:build integration || e2e || e2b_integration

package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBrainMCPFailureDoesNotBlockCCRv2(t *testing.T) {
	for _, mode := range []string{"upstream error", "lost response", "oversized response"} {
		t.Run(mode, func(t *testing.T) {
			app, worker, epoch, token := newBrainMCPProxyApp(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				switch mode {
				case "lost response":
					connection, _, err := http.NewResponseController(w).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
				case "oversized response":
					_, _ = io.WriteString(w, strings.Repeat("x", (8<<20)+1))
				default:
					http.Error(w, "sandbox unavailable", http.StatusServiceUnavailable)
				}
			}))
			response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp",
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{"file_path":"/tmp/test"}}}`), token, false)
			response.Body.Close()
			if response.StatusCode < 500 {
				t.Fatalf("failed upstream status=%d", response.StatusCode)
			}
			if _, err := app.db.RecordCodeSessionWorkerHeartbeat(t.Context(), worker.ExternalID, epoch, time.Minute, 0); err != nil {
				t.Fatalf("MCP failure blocked heartbeat: %v", err)
			}
			_, err := app.db.AppendCodeSessionInternalEvents(t.Context(), worker.ExternalID, epoch, []db.AppendCodeSessionInternalEventInput{{
				ExternalID: "csie_recovery", EventType: "system", PayloadUUID: "recovery", IsCompaction: true, IdempotencyKey: "recovery",
				Payload: json.RawMessage(`{"type":"system","uuid":"recovery","subtype":"dsh_brain_state","schema_version":1,"data":{"phase":"blocked"}}`),
			}})
			if err != nil {
				t.Fatalf("MCP failure blocked Brain recovery checkpoint: %v", err)
			}
		})
	}
}

func TestBrainMCPRejectsInvalidIngress(t *testing.T) {
	app, worker, _, token := newBrainMCPProxyApp(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid ingress reached the sandbox")
	}))
	foreign := codeSessionIngressTokenWithScope(t, app, worker.ExternalID, worker.OrganizationUUID, uuid.NewString())
	reject := func(credential string) {
		t.Helper()
		response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{"file_path":"/tmp/test"}}}`), credential, false)
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("invalid ingress status=%d", response.StatusCode)
		}
	}
	reject("")
	reject(foreign)
	public, _, err := app.db.GetSession(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.db.RotateManagedAgentCodeSessionCredentials(t.Context(), public, worker.ExternalID, "replacement"); err != nil {
		t.Fatal(err)
	}
	reject(token)
}

func TestBrainMCPForwardingDoesNotHoldSessionLock(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	app, worker, epoch, token := newBrainMCPProxyApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	results := make(chan error, 2)
	forward := func() {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{"file_path":"/tmp/test"}}}`))
		if err == nil {
			request.Header.Set("X-Api-Key", token)
			request.Header.Set("Content-Type", "application/json")
			var response *http.Response
			response, err = app.client.Do(request)
			if err == nil {
				_, err = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Errorf("MCP response status=%d", response.StatusCode)
				}
			}
		}
		results <- err
	}
	waitStarted := func() {
		t.Helper()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("MCP forwarding was serialized behind another call")
		}
	}
	go forward()
	waitStarted()
	heartbeatCtx, heartbeatCancel := context.WithTimeout(ctx, 3*time.Second)
	defer heartbeatCancel()
	if _, err := app.db.RecordCodeSessionWorkerHeartbeat(heartbeatCtx, worker.ExternalID, epoch, time.Minute, 0); err != nil {
		t.Fatalf("MCP forwarding held the session lock: %v", err)
	}
	go forward()
	waitStarted()
	unblock()
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

func TestBrainMCPForwardsModernHeaders(t *testing.T) {
	app, worker, _, token := newBrainMCPProxyApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, key := range []string{"X-OMA-MCP-Client-ID", "X-OMA-MCP-Scope", "Mcp-Session-Id"} {
			if got := r.Header.Get(key); got != "" {
				t.Errorf("obsolete %s forwarded as %q", key, got)
			}
		}
		for key, want := range map[string]string{
			"Mcp-Protocol-Version": "2026-07-28",
			"Mcp-Method":           "tools/call",
			"Mcp-Name":             "bash",
			"Mcp-Param-Region":     "us-east-1",
		} {
			if got := r.Header.Get(key); got != want {
				t.Errorf("%s = %q, want %q", key, got, want)
			}
		}
		w.Header().Set("Mcp-Session-Id", "obsolete-session")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`)
	}))
	request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bash","arguments":{"region":"us-east-1"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
	request.Header.Set("Mcp-Method", "tools/call")
	request.Header.Set("Mcp-Name", "bash")
	request.Header.Set("Mcp-Param-Region", "us-east-1")
	request.Header.Set("X-OMA-MCP-Client-ID", "obsolete-client")
	request.Header.Set("Mcp-Session-Id", "obsolete-session")
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if got := response.Header.Get("Mcp-Session-Id"); got != "" {
		t.Fatalf("obsolete session header returned as %q", got)
	}
}

func TestBrainMCPStreamsSSEWithoutWaitingForCompletion(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	app, worker, _, token := newBrainMCPProxyApp(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"bash"}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("status = %d, buffering = %q", response.StatusCode, response.Header.Get("X-Accel-Buffering"))
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || line != "data: first\n" {
		t.Fatalf("first SSE line = %q, error = %v", line, err)
	}
}

func TestBrainMCPRustServerInterop(t *testing.T) {
	endpoint := os.Getenv("OMA_RUST_MCP_URL")
	if endpoint == "" {
		t.Skip("OMA_RUST_MCP_URL is not set")
	}
	app, worker, _, token := newBrainMCPProxyAppWithEndpoint(t, endpoint)
	post := func(body, method, name string) (int, string) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/sandbox/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		request.Header.Set("Mcp-Protocol-Version", "2026-07-28")
		request.Header.Set("Mcp-Method", method)
		if name != "" {
			request.Header.Set("Mcp-Name", name)
		}
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, string(responseBody)
	}
	status, _ := post(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, "tools/list", "")
	if status != http.StatusBadRequest {
		t.Fatalf("missing protocol metadata status = %d", status)
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`
	status, body := post(`{"jsonrpc":"2.0","id":2,"method":"server/discover","params":{`+meta+`}}`, "server/discover", "")
	if status != http.StatusOK || !strings.Contains(body, `"supportedVersions":["2026-07-28"]`) {
		t.Fatalf("discovery status = %d, body = %q", status, body)
	}
	status, body = post(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"bash","arguments":{"command":"printf real-proxy-ok"},`+meta+`}}`, "tools/call", "bash")
	if status != http.StatusOK || !strings.Contains(body, `"stdout":"real-proxy-ok"`) {
		t.Fatalf("tool status = %d, body = %q", status, body)
	}
}

func newBrainMCPProxyApp(t *testing.T, handler http.Handler) (*testApp, db.CodeSession, int64, string) {
	t.Helper()
	sandbox := httptest.NewServer(handler)
	t.Cleanup(sandbox.Close)
	return newBrainMCPProxyAppWithEndpoint(t, sandbox.URL)
}

func newBrainMCPProxyAppWithEndpoint(t *testing.T, endpoint string) (*testApp, db.CodeSession, int64, string) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("brain-mcp-proxy"))
	worker, _ := newPayloadIntegrationSession(t, app)
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil).WithSandboxProvisioner(fixedSandboxProvisioner(endpoint))
	proxyHandler := codesessions.NewHandler(app.cfg, service, app.sandboxTimeouts, nil)
	router := chi.NewRouter()
	router.Route("/v2", proxyHandler.RegisterV2Routes)
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	app.baseURL = gateway.URL
	claimed, err := registerTaskTestWorker(t, app, worker)
	if err != nil {
		t.Fatal(err)
	}
	return app, worker, claimed.CurrentWorkerEpoch, codeSessionIngressToken(t, app, worker.ExternalID)
}

func registerTaskTestWorker(t *testing.T, app *testApp, worker db.CodeSession) (db.CodeSession, error) {
	t.Helper()
	_, _, err := app.db.RegisterCodeSessionWorkerAtEpoch(t.Context(), worker.ExternalID, worker.CurrentWorkerEpoch,
		db.CodeSessionWorkerBinding{TokenSessionID: worker.ExternalID}, time.Minute)
	return worker, err
}
