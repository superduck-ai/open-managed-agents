//go:build integration || e2e || e2b_integration

package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func newBrainToolCallApp(t *testing.T, agentJSON string) (*testApp, db.CodeSession, int64, string) {
	t.Helper()
	app := newPayloadIntegrationApp(t, newFakeStore("brain-tool-calls"))
	worker, _ := newPayloadIntegrationSessionWithAgent(t, app, agentJSON)
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET metadata=jsonb_set(metadata,'{config}',COALESCE(metadata->'config','{}') || '{"worker_transport":"claim"}') WHERE external_id=$1`, worker.ExternalID); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	work, err := app.db.CreateEnvironmentWork(t.Context(), db.EnvironmentWork{UUID: uuid.NewString(), ExternalID: "work_claim_" + uuid.NewString(), OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID, EnvironmentUUID: worker.EnvironmentUUID, EnvironmentExternalID: worker.EnvironmentExternalID, SessionUUID: worker.SessionUUID, SessionExternalID: worker.SessionExternalID, State: "active", Metadata: []byte(`{}`), CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	providerID := "provider-claim-test"
	if _, err := app.db.CreateEnvironmentSandbox(t.Context(), db.EnvironmentSandbox{UUID: uuid.NewString(), ExternalID: "sbx_claim_" + uuid.NewString(), OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID, EnvironmentUUID: worker.EnvironmentUUID, EnvironmentExternalID: worker.EnvironmentExternalID, WorkUUID: &work.UUID, WorkExternalID: &work.ExternalID, Provider: "e2b", Template: "test", ProviderSandboxID: &providerID, State: "running", Metadata: []byte(`{}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}

	service := newCodeSessionService(app, nil, nil).WithSandboxProvisioner(fixedSandboxProvisioner{db: app.db})
	handler := codesessions.NewHandler(app.cfg, service, app.sandboxTimeouts, nil)
	router := chi.NewRouter()
	router.Route("/v1", handler.RegisterV1Routes)
	router.Route("/v2", handler.RegisterV2Routes)
	gateway := httptest.NewServer(router)
	t.Cleanup(gateway.Close)
	app.baseURL = gateway.URL
	claimed, err := registerTaskTestWorker(t, app, worker)
	if err != nil {
		t.Fatal(err)
	}
	return app, worker, claimed.CurrentWorkerEpoch, codeSessionIngressToken(t, app, worker.ExternalID)
}

func TestBrainToolCallSubmissionIdentity(t *testing.T) {
	app, worker, _, token := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"claim-submission"}`)
	endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/threads/root/tool-calls"
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"call_id":"large","tool":"write","input":{"content":"` + strings.Repeat("x", 1<<20) + `"}}`, http.StatusRequestEntityTooLarge},
		{`{"call_id":"trailing","tool":"bash","input":{}} {}`, http.StatusBadRequest},
		{`{"call_id":"call","tool":"bash","input":{"command":"true"}}`, http.StatusAccepted},
		{`{"call_id":"call","tool":"bash","input":{"command":"different"}}`, http.StatusConflict},
		{`{"call_id":"call","tool":"bash","input":{"command":"true"}}`, http.StatusAccepted},
	} {
		response := doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(tc.body), token, false)
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("submission status=%d body=%s", response.StatusCode, body)
		}
	}
	response := doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(`{"call_id":"background","tool":"bash","input":{"command":"sleep 900","timeout":600,"on_timeout":"background"},"timeout_ms":630000}`), token, false)
	var submitted struct {
		DeadlineAt time.Time `json:"deadline_at"`
	}
	decodeJSON(t, response.Body, &submitted)
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted || time.Until(submitted.DeadlineAt) < 629*time.Second {
		t.Fatalf("background deadline truncated: status=%d deadline=%v", response.StatusCode, submitted.DeadlineAt)
	}

}

func TestBrainToolCallEncodedIdentity(t *testing.T) {
	app, worker, _, token := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"claim-encoded-identity"}`)
	endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/threads/root/tool-calls"
	for _, id := range []string{"toolu_functions.Bash:0", "call/tool", "call%2Fvalue", "call +?&"} {
		body, err := json.Marshal(map[string]any{"call_id": id, "tool": "bash", "input": map[string]string{"command": "true"}})
		if err != nil {
			t.Fatal(err)
		}
		response := doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(string(body)), token, false)
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("submit %q status=%d", id, response.StatusCode)
		}
		path := endpoint + "/" + strings.ReplaceAll(url.QueryEscape(id), "+", "%20")
		for _, step := range []struct{ method, suffix, state string }{
			{http.MethodGet, "", "pending"},
			{http.MethodPost, "/cancel", "cancelled"},
			{http.MethodGet, "", "cancelled"},
		} {
			response := doSessionRequest(t, app, step.method, path+step.suffix, nil, token, false)
			var call struct {
				State string `json:"state"`
			}
			decodeJSON(t, response.Body, &call)
			response.Body.Close()
			if response.StatusCode != http.StatusOK || call.State != step.state {
				t.Fatalf("%s %q status=%d state=%q", step.method, id, response.StatusCode, call.State)
			}
		}
	}
}

func TestBrainPendingToolCallPreventsIdleReclaim(t *testing.T) {
	app, worker, epoch, _ := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"claim-idle-guard"}`)
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_status='idle',idle_since=$1 WHERE uuid=$2`, time.Now().Add(-time.Hour), worker.UUID); err != nil {
		t.Fatal(err)
	}
	scope := db.SandboxToolCallScope{OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID, CodeSessionID: worker.ExternalID, WorkerEpoch: epoch, ThreadID: "root", CallID: "pending-blocks-idle"}
	if _, err := app.db.SubmitSandboxToolCall(t.Context(), db.SandboxToolCallSubmission{Scope: scope, Tool: "bash", Input: []byte(`{"command":"true"}`), Deadline: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	candidates, err := app.db.ListSandboxReclaimCandidates(t.Context(), time.Now(), "", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatal("pending tool call allowed idle reclamation")
	}
	if _, err := app.db.CancelSandboxToolCall(t.Context(), scope); err != nil {
		t.Fatal(err)
	}
	candidates, err = app.db.ListSandboxReclaimCandidates(t.Context(), time.Now(), "", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("terminal call blocked idle reclamation: %d candidates", len(candidates))
	}
}

func registerTaskTestWorker(t *testing.T, app *testApp, worker db.CodeSession) (db.CodeSession, error) {
	t.Helper()
	_, _, err := app.db.RegisterCodeSessionWorkerAtEpoch(t.Context(), worker.ExternalID, worker.CurrentWorkerEpoch,
		db.CodeSessionWorkerBinding{TokenSessionID: worker.ExternalID}, time.Minute)
	return worker, err
}

func fixedWorkerLease(ttl, grace time.Duration) db.WorkerLeasePolicy {
	return func(string) db.WorkerLeaseTimings { return db.WorkerLeaseTimings{TTL: ttl, Grace: grace} }
}

func TestBrainToolCallResultRoundTrip(t *testing.T) {
	app, worker, _, workerToken := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"claim-result"}`)
	sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(t.Context(), worker.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	sandboxToken, err := newCodeSessionService(app, nil, nil).SandboxIngressToken(t.Context(), db.Session{OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID}, worker.ExternalID, sandbox.UUID)
	if err != nil {
		t.Fatal(err)
	}
	for index, text := range []string{strings.Repeat("output", 8000), "before\x00after"} {
		id := "result_" + strconv.Itoa(index)
		submission := `{"call_id":"` + id + `","tool":"write","input":{"file_path":"/tmp/a","content":"a\u0000b"}}`
		endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/threads/root/tool-calls"
		response := doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(submission), workerToken, false)
		body := readAll(t, response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("submit status=%d body=%s", response.StatusCode, body)
		}
		response = doSessionRequest(t, app, http.MethodPost, "/v1/code/sandbox/tool-calls/claims?max=0", nil, sandboxToken, false)
		var empty struct {
			Calls []json.RawMessage `json:"calls"`
		}
		decodeJSON(t, response.Body, &empty)
		response.Body.Close()
		if response.StatusCode != 200 || len(empty.Calls) != 0 {
			t.Fatal("zero slots received a call")
		}
		response = doSessionRequest(t, app, http.MethodPost, "/v1/code/sandbox/tool-calls/claims?max=1", nil, sandboxToken, false)
		var claimed struct {
			Calls []struct {
				UUID    string `json:"call_uuid"`
				Attempt int    `json:"attempt"`
			} `json:"calls"`
		}
		decodeJSON(t, response.Body, &claimed)
		response.Body.Close()
		if response.StatusCode != 200 || len(claimed.Calls) != 1 {
			t.Fatalf("claims status=%d calls=%+v", response.StatusCode, claimed)
		}
		call := claimed.Calls[0]
		payload, err := json.Marshal(struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}{Content: []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{{Type: "text", Text: text}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, report := range []struct {
			attempt int
			status  string
			body    []byte
			want    int
		}{
			{call.Attempt, "stopped", []byte("{"), 400},
			{call.Attempt + 1, "succeeded", payload, 409},
			{call.Attempt, "succeeded", payload, 200},
			{call.Attempt, "succeeded", payload, 200},
		} {
			digest := sha256.Sum256(report.body)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, app.baseURL+"/v1/code/sandbox/tool-calls/"+call.UUID+"/result", strings.NewReader(string(report.body)))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+sandboxToken)
			request.Header.Set("Oma-Tool-Call-Attempt", strconv.Itoa(report.attempt))
			request.Header.Set("Oma-Result-Status", report.status)
			request.Header.Set("Oma-Result-Sha256", hex.EncodeToString(digest[:]))
			response, err := app.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			body := readAll(t, response.Body)
			response.Body.Close()
			if response.StatusCode != report.want {
				t.Fatalf("result status=%d want=%d body=%s", response.StatusCode, report.want, body)
			}
		}
		response = doSessionRequest(t, app, http.MethodGet, endpoint+"/"+id, nil, workerToken, false)
		var waited struct {
			State  string `json:"state"`
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		decodeJSON(t, response.Body, &waited)
		response.Body.Close()
		if response.StatusCode != 200 || waited.State != "succeeded" || len(waited.Result.Content) != 1 || waited.Result.Content[0].Text != text {
			t.Fatalf("round trip mismatch: state=%s status=%d", waited.State, response.StatusCode)
		}
	}
}

func TestBrainToolCallCancellationConfirmation(t *testing.T) {
	app, worker, _, workerToken := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"claim-cancel"}`)
	sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(t.Context(), worker.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := newCodeSessionService(app, nil, nil).SandboxIngressToken(t.Context(), db.Session{OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID}, worker.ExternalID, sandbox.UUID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/threads/root/tool-calls"
	response := doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(`{"call_id":"stop","tool":"bash","input":{"command":"sleep 30"}}`), workerToken, false)
	response.Body.Close()
	if response.StatusCode != 202 {
		t.Fatalf("submit status=%d", response.StatusCode)
	}
	response = doSessionRequest(t, app, http.MethodPost, "/v1/code/sandbox/tool-calls/claims?max=1", nil, token, false)
	var claimed struct {
		Calls []struct {
			UUID    string `json:"call_uuid"`
			Attempt int    `json:"attempt"`
		} `json:"calls"`
	}
	decodeJSON(t, response.Body, &claimed)
	response.Body.Close()
	if len(claimed.Calls) != 1 {
		t.Fatalf("claims=%+v", claimed)
	}
	response = doSessionRequest(t, app, http.MethodPost, "/v1/code/sandbox/tool-calls/"+claimed.Calls[0].UUID+"/events", strings.NewReader(`{"attempt":`+strconv.Itoa(claimed.Calls[0].Attempt)+`,"event":"started"}`), token, false)
	var state struct {
		State string `json:"state"`
	}
	decodeJSON(t, response.Body, &state)
	response.Body.Close()
	if state.State != "running" {
		t.Fatalf("started state=%s", state.State)
	}
	response = doSessionRequest(t, app, http.MethodPost, endpoint+"/stop/cancel", strings.NewReader(`{}`), workerToken, false)
	decodeJSON(t, response.Body, &state)
	response.Body.Close()
	if state.State != "cancel_requested" {
		t.Fatalf("cancel state=%s", state.State)
	}
	payload := `{"content":[{"type":"text","text":"partial output"}],"isError":true}`
	hash := sha256.Sum256([]byte(payload))
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, app.baseURL+"/v1/code/sandbox/tool-calls/"+claimed.Calls[0].UUID+"/result", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Oma-Tool-Call-Attempt", strconv.Itoa(claimed.Calls[0].Attempt))
	request.Header.Set("Oma-Result-Status", "interrupted")
	request.Header.Set("Oma-Result-Reason", "stop_requested")
	request.Header.Set("Oma-Result-Sha256", hex.EncodeToString(hash[:]))
	response, err = app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !strings.Contains(string(body), `"state":"cancelled"`) || !strings.Contains(string(body), "partial output") {
		t.Fatalf("stop status=%d body=%s", response.StatusCode, body)
	}
}

func TestBrainToolCallCancellationBeforeCommitNeverRuns(t *testing.T) {
	app, worker, _, workerToken := newBrainToolCallApp(t, `{"model":"claude-opus-4-6","name":"claim-precommit"}`)
	sandbox, err := app.db.GetResumableEnvironmentSandboxForCodeSession(t.Context(), worker.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := newCodeSessionService(app, nil, nil).SandboxIngressToken(t.Context(), db.Session{OrganizationUUID: worker.OrganizationUUID, WorkspaceUUID: worker.WorkspaceUUID}, worker.ExternalID, sandbox.UUID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "/v2/ccr-sessions/" + worker.ExternalID + "/threads/root/tool-calls"
	response := doSessionRequest(t, app, http.MethodPost, endpoint, strings.NewReader(`{"call_id":"precommit","tool":"bash","input":{"command":"sleep 30"}}`), workerToken, false)
	response.Body.Close()
	response = doSessionRequest(t, app, http.MethodPost, "/v1/code/sandbox/tool-calls/claims?max=1", nil, token, false)
	var claimed struct {
		Calls []struct {
			UUID    string `json:"call_uuid"`
			Attempt int    `json:"attempt"`
		} `json:"calls"`
	}
	decodeJSON(t, response.Body, &claimed)
	response.Body.Close()
	if len(claimed.Calls) != 1 {
		t.Fatalf("claims=%+v", claimed)
	}
	var state struct {
		State string `json:"state"`
	}
	response = doSessionRequest(t, app, http.MethodPost, endpoint+"/precommit/cancel", strings.NewReader(`{}`), workerToken, false)
	decodeJSON(t, response.Body, &state)
	response.Body.Close()
	if state.State != "cancelled" {
		t.Fatalf("cancel before started state=%s", state.State)
	}
	response = doSessionRequest(t, app, http.MethodPost, "/v1/code/sandbox/tool-calls/"+claimed.Calls[0].UUID+"/events", strings.NewReader(`{"attempt":`+strconv.Itoa(claimed.Calls[0].Attempt)+`,"event":"started"}`), token, false)
	decodeJSON(t, response.Body, &state)
	response.Body.Close()
	if state.State != "cancelled" {
		t.Fatalf("late started state=%s", state.State)
	}
}
