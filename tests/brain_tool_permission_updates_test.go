//go:build integration

package tests

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"uuid"
)

func TestBrainToolPermissionsFollowSessionUpdates(t *testing.T) {
	for _, policy := range []string{"deny", "ask"} {
		t.Run(policy, func(t *testing.T) {
			app, worker, _, token := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"policy-update"}`)
			calls := 0
			call := func() *http.Response {
				calls++
				response := doSessionRequest(t, app, http.MethodPost, "/v2/ccr-sessions/"+worker.ExternalID+"/threads/root/tool-calls",
					strings.NewReader(fmt.Sprintf(`{"call_id":"call_%d","tool":"bash","input":{"command":"echo ok"}}`, calls)), token, false)
				response.Body.Close()
				return response
			}
			readPolicy := func() string {
				response := doSessionRequest(t, app, http.MethodGet, "/v2/sessions/"+worker.ExternalID, nil, token, false)
				defer response.Body.Close()
				if response.StatusCode != http.StatusOK {
					t.Fatalf("session context status=%d: %s", response.StatusCode, readAll(t, response.Body))
				}
				var context struct {
					SessionContext struct {
						Permissions map[string]string `json:"runtime_tool_permissions"`
					} `json:"session_context"`
				}
				decodeJSON(t, response.Body, &context)
				return context.SessionContext.Permissions["bash"]
			}
			if initial := readPolicy(); initial != "allow" {
				t.Fatalf("initial policy=%q", initial)
			}
			if response := call(); response.StatusCode != http.StatusAccepted {
				t.Fatalf("initial call status=%d forwarded=%d", response.StatusCode, calls)
			}
			if _, err := app.db.SetSessionStatus(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, "idle"); err != nil {
				t.Fatal(err)
			}
			publicApp := *app
			publicApp.baseURL = app.server.URL
			toolConfig := `"enabled":false`
			if policy == "ask" {
				toolConfig = `"permission_policy":{"type":"always_ask"}`
			}
			updateSession(t, &publicApp, worker.SessionExternalID,
				`{"agent":{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"bash",`+toolConfig+`}]}]}}`)
			if current := readPolicy(); current != policy {
				t.Fatalf("updated policy=%q want=%q", current, policy)
			}
			if policy == "deny" {
				renewals := len(app.sandboxTimeouts.snapshotCalls())
				response := call()
				if response.StatusCode != http.StatusForbidden {
					t.Fatalf("denied call status=%d marker=%q", response.StatusCode, response.Header.Get("X-Oma-Sandbox-Not-Forwarded"))
				}
				if len(app.sandboxTimeouts.snapshotCalls()) != renewals {
					t.Fatal("updated denial forwarded or renewed the sandbox")
				}
			}
		})
	}
}

func TestBrainOutputWriteRespectsChildPolicy(t *testing.T) {
	app, worker, _, token := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"output-policy"}`)
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("primary: %v", err)
	}
	child := primary
	child.UUID = uuid.NewV4().String()
	child.ExternalID = "sthr_output_denied"
	child.ParentThreadUUID = &primary.UUID
	child.ParentThreadExternalID = &primary.ExternalID
	child.AgentSnapshot = []byte(`{"tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","enabled":false}]}]}`)
	if _, err := app.db.CreateSessionThreadIfAbsent(t.Context(), child); err != nil {
		t.Fatal(err)
	}
	for _, thread := range []string{child.ExternalID, "root"} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v2/ccr-sessions/"+worker.ExternalID+"/threads/"+thread+"/tool-calls", strings.NewReader(`{"call_id":"output","tool":"write","input":{"file_path":"/tmp/tool-outputs/test.txt","content":"web output"}}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		if thread != "root" {
			request.Header.Set("X-OMA-Session-Thread-ID", thread)
		}
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusAccepted
		if thread != "root" {
			want = http.StatusForbidden
		}
		if response.StatusCode != want {
			t.Fatalf("thread=%q status=%d want=%d", thread, response.StatusCode, want)
		}
	}
}
