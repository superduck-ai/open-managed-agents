//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type provisioningCodeSessions struct {
	*codesessions.Service
	dispatches atomic.Int32
}

func (s *provisioningCodeSessions) DispatchWorker(ctx context.Context, credentials codesessions.ManagedAgentCreateResult) error {
	s.dispatches.Add(1)
	return s.Service.DispatchWorker(ctx, credentials)
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
		name                 string
		prewarm, fail, vault bool
	}{
		{"failed prewarm does not dispatch", true, true, false}, {"lazy", false, false, false}, {"prewarm", true, false, false},
		{"lazy vault placeholders", false, false, true}, {"prewarm vault placeholders", true, false, true},
	} {
		prewarm := scenario.prewarm
		t.Run(scenario.name, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("sandbox-provisioning"))
			cfg := app.cfg
			cfg.EnvironmentRunner.AgentMode = "brain"
			cfg.EnvironmentRunner.BrainSandboxPrewarm = prewarm
			cfg.CodeSession.UpstreamProxyMITMEnabled = scenario.vault
			cfg.CodeSession.SandboxAPIBaseURL = "http://sandbox.example.test"
			agent := createAgent(t, app, `{"name":"lazy","model":"claude-opus-4-6"}`)
			env := createEnvironment(t, app, `{"name":"lazy","config":{"type":"cloud","networking":{"type":"unrestricted"}}}`)
			vaultBinding, expectedPlaceholder := "", ""
			if scenario.vault {
				vault := createVault(t, app, `{"display_name":"brain env mount"}`)
				credential := createVaultCredential(t, app, vault.ID, `{"auth":{"type":"environment_variable","secret_name":"VAULT_TEST","secret_value":"synthetic-mount-canary","networking":{"type":"unrestricted"}}}`)
				var auth struct {
					Placeholder string `json:"placeholder"`
				}
				if err := json.Unmarshal(credential.Auth, &auth); err != nil {
					t.Fatal(err)
				}
				expectedPlaceholder = auth.Placeholder
				vaultBinding = `,"vault_ids":[` + quoteJSON(vault.ID) + `]`
			}
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+vaultBinding+`}`)
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
			broker := &brainTaskRecorder{MemoryBroker: workerevents.NewMemory()}
			service := &provisioningCodeSessions{Service: codesessions.NewServiceWithCredentials(app.db, app.credentials, slog.Default()).WithWorkerEventBroker(broker)}
			var creates atomic.Int32
			providers := make([]*provisioningTestProvider, 2)
			runners := make([]*environments.Runner, 2)
			for i := range 2 {
				providers[i] = &provisioningTestProvider{recordingRunnerProvider: &recordingRunnerProvider{sandboxID: "sandbox-provisioning", beforeCreate: func() {
					creates.Add(1)
					wantTasks := 1
					if prewarm {
						wantTasks = 0
					}
					if len(broker.tasks) != wantTasks {
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
				if err == nil || !processed || len(broker.tasks) != 0 || service.dispatches.Load() != 0 {
					t.Fatalf("failed prewarm dispatched worker: processed=%t err=%v tasks=%d", processed, err, len(broker.tasks))
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
				calls.Go(func() { urls[i], failures[i] = runners[i].EnsureSessionSandbox(ctx, code) })
			}
			calls.Wait()
			if failures[0] != nil || failures[1] != nil || urls[0] != endpoint.URL || urls[1] != endpoint.URL || creates.Load() != 1 {
				t.Fatalf("concurrent sandbox preparation: %v %v creates=%d", urls, failures, creates.Load())
			}
			if len(providers[0].launches)+len(providers[1].launches) != 1 {
				t.Fatal("MCP started more than once")
			}
			if scenario.vault {
				for _, provider := range providers {
					for _, launch := range provider.launches {
						var payload struct {
							Startup struct {
								Environment map[string]string `json:"environment_variables"`
							} `json:"startup_context"`
						}
						if err := json.Unmarshal(launch.stdin, &payload); err != nil {
							t.Fatal(err)
						}
						if expectedPlaceholder == "" || payload.Startup.Environment["VAULT_TEST"] != expectedPlaceholder {
							t.Fatal("Vault placeholder missing from Brain sandbox startup")
						}
						if strings.Contains(string(launch.stdin), "synthetic-mount-canary") {
							t.Fatal("Vault secret entered sandbox startup")
						}
					}
				}
			}
			_, err = runners[1].EnsureSessionSandbox(ctx, code)
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
			if _, err := runners[0].EnsureSessionSandbox(ctx, code); err == nil {
				t.Fatal("deleted session provisioned sandbox")
			}
		})
	}
}
