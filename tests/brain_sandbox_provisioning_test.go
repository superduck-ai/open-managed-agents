package tests

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type provisioningCodeSessions struct {
	*codesessions.Service
	dispatches atomic.Int32
}

func (s *provisioningCodeSessions) DispatchWorker(ctx context.Context, session db.Session, codeSessionID string) error {
	s.dispatches.Add(1)
	return s.Service.DispatchWorker(ctx, session, codeSessionID)
}

type provisioningTestProvider struct {
	*recordingRunnerProvider
	endpoint string
}

func (p *provisioningTestProvider) ServiceEndpoint(context.Context, string, int, string) (string, error) {
	return p.endpoint, nil
}

func TestBrainSandboxUsesEnvironmentLifecycle(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		prewarm, fail bool
	}{
		{"failed prewarm does not dispatch", true, true}, {"lazy", false, false}, {"prewarm", true, false},
	} {
		prewarm := scenario.prewarm
		t.Run(scenario.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("sandbox-provisioning"))
			cfg := app.cfg
			cfg.EnvironmentRunner.AgentMode = "brain"
			cfg.EnvironmentRunner.BrainSandboxPrewarm = prewarm
			cfg.CodeSession.UpstreamProxyMITMEnabled = false
			cfg.CodeSession.SandboxAPIBaseURL = "http://sandbox.example.test"
			agent := createAgent(t, app, `{"name":"lazy","model":"claude-opus-4-6"}`)
			env := createEnvironment(t, app, `{"name":"lazy","config":{"type":"cloud","networking":{"type":"unrestricted"}}}`)
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			sendSessionEvents(t, app, session.ID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"hello"}]}]}`, defaultTestKey)
			var discoveries atomic.Int32
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Mcp-Method") != "server/discover" {
					t.Errorf("unexpected MCP method: %q", r.Header.Get("Mcp-Method"))
				}
				discoveries.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"resultType\":\"complete\",\"supportedVersions\":[\"2026-07-28\"],\"capabilities\":{\"tools\":{}}}}\n\n"))
			}))
			defer endpoint.Close()
			service := &provisioningCodeSessions{Service: codesessions.NewServiceWithCredentials(app.db, app.credentials, slog.Default()).WithWorkerEventBroker(workerevents.NewMemory())}
			var creates atomic.Int32
			providers := make([]*provisioningTestProvider, 2)
			runners := make([]*environments.Runner, 2)
			for i := range 2 {
				providers[i] = &provisioningTestProvider{recordingRunnerProvider: &recordingRunnerProvider{sandboxID: "sandbox-provisioning", beforeCreate: func() {
					creates.Add(1)
					wantDispatches := int32(1)
					if prewarm {
						wantDispatches = 0
					}
					if service.dispatches.Load() != wantDispatches {
						t.Error("unexpected worker dispatch order")
					}
				}}, endpoint: endpoint.URL}
				if scenario.fail {
					providers[i].resolveErr = errors.New("prewarm unavailable")
				}
				var err error
				runners[i], err = environments.NewRunner(environments.RunnerDependencies{DB: app.db, Provider: providers[i], Config: cfg, CodeSessions: service, Skills: skillsapi.NewRuntimeResolver(app.db), FilestoreTokens: app.filestoreCredentials, Logger: slog.Default()})
				if err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			processed, err := runners[0].RunOnce(ctx, "deferred")
			if scenario.fail {
				if err == nil || !processed || service.dispatches.Load() != 0 {
					t.Fatalf("failed prewarm dispatched worker: processed=%t err=%v dispatches=%d", processed, err, service.dispatches.Load())
				}
				return
			}
			if err != nil || !processed {
				t.Fatalf("deferred launch: %t %v", processed, err)
			}
			if service.dispatches.Load() != 1 {
				t.Fatal("queued Work starved worker recovery dispatch")
			}
			wantCreates := int32(0)
			if prewarm {
				wantCreates = 1
			}
			if creates.Load() != wantCreates {
				t.Fatalf("initial creations: %d", creates.Load())
			}
			key, err := app.db.GetAPIKey(ctx, auth.HashAPIKey(defaultTestKey))
			if err != nil {
				t.Fatal(err)
			}
			code, err := app.db.GetCodeSessionBySessionExternalID(ctx, key.WorkspaceUUID.String(), session.ID)
			if err != nil {
				t.Fatal(err)
			}
			urls := make([]string, 2)
			failures := make([]error, 2)
			var calls sync.WaitGroup
			for i := range 2 {
				calls.Go(func() { urls[i], _, failures[i] = runners[i].EnsureSessionSandbox(ctx, code) })
			}
			calls.Wait()
			if failures[0] != nil || failures[1] != nil || urls[0] != endpoint.URL || urls[1] != endpoint.URL || creates.Load() != 1 {
				t.Fatalf("concurrent sandbox preparation: %v %v creates=%d", urls, failures, creates.Load())
			}
			if len(providers[0].launches)+len(providers[1].launches) != 1 {
				t.Fatal("MCP started more than once")
			}
			_, _, err = runners[1].EnsureSessionSandbox(ctx, code)
			if err != nil || creates.Load() != 1 {
				t.Fatalf("warm reuse: %v creates=%d", err, creates.Load())
			}
			if discoveries.Load() != 1 {
				t.Fatalf("MCP discovery calls = %d, want one startup check", discoveries.Load())
			}
			response := doSessionRequest(t, app, http.MethodDelete, "/v1/sessions/"+session.ID, nil, defaultTestKey, true)
			response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("delete session: %d", response.StatusCode)
			}
			if _, _, err := runners[0].EnsureSessionSandbox(ctx, code); err == nil {
				t.Fatal("deleted session provisioned sandbox")
			}
		})
	}
}
