//go:build integration

package tests

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	brainClaimTestToken = "brain-claim-test-token-0123456789abcdef"
	brainClaimIdleBody  = `{"max_tasks":1,"max_sessions":8,"busy_sessions":0,"resident_sessions":0,"admission_failures":0}`
)

type brainClaimTestResponse struct {
	SessionID      string `json:"session_id"`
	WorkerEpoch    int64  `json:"worker_epoch"`
	IngressToken   string `json:"ingress_token"`
	ModelToken     string `json:"model_token"`
	ClaimExpiresAt string `json:"claim_expires_at"`
}

type brainClaimTestBatch struct {
	Tasks []brainClaimTestResponse `json:"tasks"`
}

func newBrainClaimApp(t *testing.T) *testApp {
	t.Helper()
	tokenFile := filepath.Join(t.TempDir(), "brain-claim-token")
	if err := os.WriteFile(tokenFile, []byte(brainClaimTestToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return newPayloadIntegrationAppWithConfig(t, newFakeStore("brain-claim"), func(cfg *config.Config) {
		cfg.CodeSession.BrainClaimTokenFile = tokenFile
	})
}

func prepareBrainClaimSession(t *testing.T, app *testApp) (db.CodeSession, db.Session) {
	t.Helper()
	code, _ := newPayloadIntegrationSession(t, app)
	return code, useBrainClaimTransport(t, app, code)
}

func prepareNamedBrainClaimSession(t *testing.T, app *testApp, name string) (db.CodeSession, db.Session) {
	t.Helper()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":`+quoteJSON(name)+`}`)
	env := createEnvironment(t, app, `{"name":`+quoteJSON(name)+`}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	codeID := launchLocalCodeSession(t, app, session.ID)
	registerCodeSessionWorker(t, app, codeID)
	code, found, err := app.db.GetCodeSession(t.Context(), codeID)
	if err != nil || !found {
		t.Fatalf("load code session: %v %t", err, found)
	}
	return code, useBrainClaimTransport(t, app, code)
}

func useBrainClaimTransport(t *testing.T, app *testApp, code db.CodeSession) db.Session {
	t.Helper()
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions
		SET metadata = jsonb_set(metadata, '{config}', COALESCE(metadata->'config', '{}'::jsonb) || '{"worker_transport":"claim"}'),
		    worker_lease_expires_at = NULL
		WHERE uuid = $1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	setBrainClaimSessionStatus(t, app, code, "idle")
	public, _, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil {
		t.Fatal(err)
	}
	return public
}

func setBrainClaimSessionStatus(t *testing.T, app *testApp, code db.CodeSession, status string) {
	t.Helper()
	if _, err := app.pool.Exec(t.Context(), `UPDATE sessions SET status = $1 WHERE uuid = $2`, status, code.SessionUUID); err != nil {
		t.Fatal(err)
	}
}

func expireBrainClaim(t *testing.T, app *testApp, code db.CodeSession) {
	t.Helper()
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_claim_expires_at = now() - interval '1 second' WHERE uuid = $1`, code.UUID); err != nil {
		t.Fatal(err)
	}
}

func doBrainClaimRequest(t *testing.T, app *testApp, method, path, token, workerID, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, app.baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if workerID != "" {
		request.Header.Set("X-Worker-ID", workerID)
	}
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func requestBrainClaims(t *testing.T, app *testApp, workerID string, blockMS int, body string) []brainClaimTestResponse {
	t.Helper()
	response := doBrainClaimRequest(t, app, http.MethodPost, "/v1/code/brain/claims?block_ms="+strconv.Itoa(blockMS), brainClaimTestToken, workerID, body)
	defer response.Body.Close()
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("claim Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d: %s", response.StatusCode, readAll(t, response.Body))
	}
	var batch brainClaimTestBatch
	if err := json.NewDecoder(response.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Tasks) == 0 {
		t.Fatal("claim returned an empty task list")
	}
	return batch.Tasks
}

func ackBrainClaim(t *testing.T, app *testApp, claim brainClaimTestResponse, workerID string) int {
	t.Helper()
	body := `{"worker_epoch":` + strconv.FormatInt(claim.WorkerEpoch, 10) + `}`
	response := doBrainClaimRequest(t, app, http.MethodPost, "/v1/code/brain/claims/"+claim.SessionID+"/ack", brainClaimTestToken, workerID, body)
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func claimBrainWorker(t *testing.T, app *testApp, workerID string, blockMS int) (brainClaimTestResponse, bool) {
	t.Helper()
	claims := requestBrainClaims(t, app, workerID, blockMS, brainClaimIdleBody)
	if len(claims) == 0 {
		return brainClaimTestResponse{}, false
	}
	if status := ackBrainClaim(t, app, claims[0], workerID); status != http.StatusOK {
		t.Fatalf("ack status = %d", status)
	}
	return claims[0], true
}

func releaseBrainClaim(t *testing.T, app *testApp, claim brainClaimTestResponse, workerID, reason string) int {
	t.Helper()
	body := `{"worker_epoch":` + strconv.FormatInt(claim.WorkerEpoch, 10) + `,"reason":` + quoteJSON(reason) + `}`
	response := doBrainClaimRequest(t, app, http.MethodPost, "/v1/code/brain/claims/"+claim.SessionID+"/release", brainClaimTestToken, workerID, body)
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func registerBrainWorker(t *testing.T, app *testApp, claim brainClaimTestResponse) int {
	t.Helper()
	response := doSessionBearerRequest(t, app, http.MethodPost, "/v1/code/sessions/"+claim.SessionID+"/worker/register", strings.NewReader(`{}`), claim.IngressToken, false)
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func heartbeatBrainWorker(t *testing.T, app *testApp, claim brainClaimTestResponse) (*http.Response, string) {
	t.Helper()
	body := `{"session_id":` + quoteJSON(claim.SessionID) + `,"worker_epoch":` + strconv.FormatInt(claim.WorkerEpoch, 10) + `}`
	response := doSessionBearerRequest(t, app, http.MethodPost, "/v1/code/sessions/"+claim.SessionID+"/worker/heartbeat", strings.NewReader(body), claim.IngressToken, false)
	defer response.Body.Close()
	return response, string(readAll(t, response.Body))
}

func assertBrainClaimFailurePublished(t *testing.T, app *testApp, code db.CodeSession, public db.Session) {
	t.Helper()
	page := listSessionEvents(t, app, public.ExternalID, "", defaultTestKey)
	var sawError, sawIdle bool
	for _, raw := range page.Data {
		var event struct {
			Type  string `json:"type"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			StopReason struct {
				Type string `json:"type"`
			} `json:"stop_reason"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		sawError = sawError || (event.Type == "session.error" && strings.Contains(event.Error.Message, "repeated attempts"))
		sawIdle = sawIdle || (event.Type == "session.status_idle" && event.StopReason.Type == "retries_exhausted")
	}
	if !sawError || !sawIdle {
		t.Fatalf("exhaustion events missing: error=%t idle=%t", sawError, sawIdle)
	}
	stored, _, err := app.db.GetSession(t.Context(), code.WorkspaceUUID, code.SessionExternalID)
	if err != nil || stored.Status != "idle" {
		t.Fatalf("session status after exhaustion = %q err=%v", stored.Status, err)
	}
}

func TestBrainClaimRejectsInvalidRequests(t *testing.T) {
	app := newBrainClaimApp(t)
	code, _ := prepareBrainClaimSession(t, app)
	for _, tc := range []struct {
		name, path, token, workerID, body string
		status                            int
	}{
		{"missing token", "/v1/code/brain/claims", "", "worker", "", http.StatusUnauthorized},
		{"wrong token", "/v1/code/brain/claims", "wrong-token-0123456789abcdef0123456789", "worker", "", http.StatusUnauthorized},
		{"missing worker", "/v1/code/brain/claims", brainClaimTestToken, "", "", http.StatusBadRequest},
		{"block too long", "/v1/code/brain/claims?block_ms=30001", brainClaimTestToken, "worker", brainClaimIdleBody, http.StatusBadRequest},
		{"missing load report", "/v1/code/brain/claims", brainClaimTestToken, "worker", `{}`, http.StatusBadRequest},
		{"batch too large", "/v1/code/brain/claims", brainClaimTestToken, "worker", `{"max_tasks":33,"max_sessions":8}`, http.StatusBadRequest},
		{"invalid ack epoch", "/v1/code/brain/claims/" + code.ExternalID + "/ack", brainClaimTestToken, "worker", `{"worker_epoch":0}`, http.StatusBadRequest},
		{"ack not held", "/v1/code/brain/claims/" + code.ExternalID + "/ack", brainClaimTestToken, "worker", `{"worker_epoch":1}`, http.StatusConflict},
		{"invalid release reason", "/v1/code/brain/claims/" + code.ExternalID + "/release", brainClaimTestToken, "worker", `{"worker_epoch":1,"reason":"capacity"}`, http.StatusBadRequest},
		{"release not held", "/v1/code/brain/claims/" + code.ExternalID + "/release", brainClaimTestToken, "worker", `{"worker_epoch":1,"reason":"shutdown"}`, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := doBrainClaimRequest(t, app, http.MethodPost, tc.path, tc.token, tc.workerID, tc.body)
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status = %d, want %d: %s", response.StatusCode, tc.status, readAll(t, response.Body))
			}
		})
	}
}

func TestBrainClaimLongPollWakesOnDispatchAndUsesShortLease(t *testing.T) {
	app := newBrainClaimApp(t)
	code, public := prepareBrainClaimSession(t, app)
	if _, found := claimBrainWorker(t, app, "worker-a", 0); found {
		t.Fatal("idle undispatched session claimed")
	}
	type claimResult struct {
		response *http.Response
		err      error
	}
	results := make(chan claimResult, 1)
	go func() {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v1/code/brain/claims?block_ms=10000", strings.NewReader(brainClaimIdleBody))
		if err != nil {
			results <- claimResult{err: err}
			return
		}
		request.Header.Set("Authorization", "Bearer "+brainClaimTestToken)
		request.Header.Set("X-Worker-ID", "worker-a")
		response, err := app.client.Do(request)
		results <- claimResult{response, err}
	}()
	time.Sleep(200 * time.Millisecond)
	dispatchedAt := time.Now()
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	result := <-results
	if result.err != nil {
		t.Fatal(result.err)
	}
	defer result.response.Body.Close()
	if result.response.StatusCode != http.StatusOK {
		t.Fatalf("long-poll status = %d: %s", result.response.StatusCode, readAll(t, result.response.Body))
	}
	var batch brainClaimTestBatch
	if err := json.NewDecoder(result.response.Body).Decode(&batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Tasks) != 1 {
		t.Fatalf("long-poll batch = %+v", batch)
	}
	claim := batch.Tasks[0]
	if status := ackBrainClaim(t, app, claim, "worker-a"); status != http.StatusOK {
		t.Fatalf("ack status = %d", status)
	}
	if claim.SessionID != code.ExternalID || claim.WorkerEpoch != code.CurrentWorkerEpoch+1 || claim.ModelToken == "" {
		t.Fatalf("long-poll claim = %+v", claim)
	}
	if elapsed := time.Since(dispatchedAt); elapsed > 3*time.Second {
		t.Fatalf("long poll returned %s after dispatch", elapsed)
	}
	if _, found := claimBrainWorker(t, app, "worker-b", 0); found {
		t.Fatal("claimed session handed to a second worker")
	}
	if status := registerBrainWorker(t, app, claim); status != http.StatusOK {
		t.Fatalf("register status = %d", status)
	}
	response, body := heartbeatBrainWorker(t, app, claim)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat status = %d: %s", response.StatusCode, body)
	}
	var heartbeat struct {
		WorkerLeaseExpiresAt time.Time `json:"worker_lease_expires_at"`
	}
	if err := json.Unmarshal([]byte(body), &heartbeat); err != nil {
		t.Fatal(err)
	}
	if lease := time.Until(heartbeat.WorkerLeaseExpiresAt); lease <= 15*time.Second || lease > 21*time.Second {
		t.Fatalf("brain worker lease = %s, want about 20s", lease)
	}
}

func TestBrainClaimTakeoverFencesStaleWorker(t *testing.T) {
	app := newBrainClaimApp(t)
	code, public := prepareBrainClaimSession(t, app)
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	first, found := claimBrainWorker(t, app, "worker-a", 0)
	if !found || registerBrainWorker(t, app, first) != http.StatusOK {
		t.Fatalf("first claim = %+v found=%t", first, found)
	}
	setBrainClaimSessionStatus(t, app, code, "running")
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_lease_expires_at = now() - interval '5 seconds' WHERE uuid = $1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	if _, found := claimBrainWorker(t, app, "worker-b", 0); found {
		t.Fatal("session taken over during lease grace")
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_lease_expires_at = now() - interval '11 seconds' WHERE uuid = $1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	second, found := claimBrainWorker(t, app, "worker-b", 0)
	if !found || second.WorkerEpoch != first.WorkerEpoch+1 {
		t.Fatalf("takeover claim = %+v found=%t", second, found)
	}
	if status := registerBrainWorker(t, app, first); status == http.StatusOK {
		t.Fatal("stale worker re-registered after takeover")
	}
	if response, body := heartbeatBrainWorker(t, app, first); response.StatusCode == http.StatusOK {
		t.Fatalf("stale worker heartbeat accepted: %s", body)
	}
	if status := registerBrainWorker(t, app, second); status != http.StatusOK {
		t.Fatalf("takeover register status = %d", status)
	}
}

func TestBrainClaimExhaustionPublishesSessionError(t *testing.T) {
	app := newBrainClaimApp(t)
	code, public := prepareBrainClaimSession(t, app)
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	setBrainClaimSessionStatus(t, app, code, "running")
	first, found := claimBrainWorker(t, app, "worker-a", 0)
	if !found {
		t.Fatal("dispatched session not claimable")
	}
	if status := releaseBrainClaim(t, app, first, "worker-b", "admission_failed"); status != http.StatusConflict {
		t.Fatalf("release by another worker = %d", status)
	}
	if status := releaseBrainClaim(t, app, first, "worker-a", "admission_failed"); status != http.StatusOK {
		t.Fatalf("release = %d", status)
	}
	if _, found := claimBrainWorker(t, app, "worker-a", 0); found {
		t.Fatal("session claimed during failure backoff")
	}
	last := first
	for attempt := 2; attempt <= 5; attempt++ {
		expireBrainClaim(t, app, code)
		if last, found = claimBrainWorker(t, app, "worker-a", 0); !found {
			t.Fatalf("attempt %d not claimable", attempt)
		}
	}
	expireBrainClaim(t, app, code)
	if _, found := claimBrainWorker(t, app, "worker-a", 0); found {
		t.Fatal("exhausted session handed out")
	}
	assertBrainClaimFailurePublished(t, app, code, public)
	if status := registerBrainWorker(t, app, last); status == http.StatusOK {
		t.Fatal("stale worker registered after exhaustion")
	}
	if _, found := claimBrainWorker(t, app, "worker-a", 0); found {
		t.Fatal("failed idle session reclaimed")
	}
}

func TestBrainClaimIgnoresDeletedPublicSession(t *testing.T) {
	app := newBrainClaimApp(t)
	code, public := prepareBrainClaimSession(t, app)
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.pool.Exec(t.Context(), `UPDATE sessions SET deleted_at = now() WHERE uuid = $1`, code.SessionUUID); err != nil {
		t.Fatal(err)
	}
	if claim, found := claimBrainWorker(t, app, "worker-a", 0); found {
		t.Fatalf("deleted session handed out: %+v", claim)
	}
	var attempts int
	var epoch int64
	if err := app.pool.QueryRow(t.Context(), `SELECT worker_claim_attempts, current_worker_epoch FROM code_sessions WHERE uuid = $1`, code.UUID).Scan(&attempts, &epoch); err != nil || attempts != 0 || epoch != code.CurrentWorkerEpoch {
		t.Fatalf("deleted public session consumed a claim: attempts=%d epoch=%d err=%v", attempts, epoch, err)
	}
}

func TestBrainClaimPermanentReleasePublishesSessionErrorImmediately(t *testing.T) {
	app := newBrainClaimApp(t)
	code, public := prepareBrainClaimSession(t, app)
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	setBrainClaimSessionStatus(t, app, code, "running")
	claim, found := claimBrainWorker(t, app, "worker-a", 0)
	if !found {
		t.Fatal("dispatched session not claimable")
	}
	if status := releaseBrainClaim(t, app, claim, "worker-b", "permanent"); status != http.StatusConflict {
		t.Fatalf("permanent release by another worker = %d", status)
	}
	if status := releaseBrainClaim(t, app, claim, "worker-a", "permanent"); status != http.StatusOK {
		t.Fatalf("permanent release = %d", status)
	}
	assertBrainClaimFailurePublished(t, app, code, public)
	if status := registerBrainWorker(t, app, claim); status == http.StatusOK {
		t.Fatal("stale worker registered after permanent failure")
	}
	if _, found := claimBrainWorker(t, app, "worker-a", 0); found {
		t.Fatal("permanently failed session reclaimed")
	}
	setBrainClaimSessionStatus(t, app, code, "running")
	if _, err := app.pool.Exec(t.Context(), `UPDATE code_sessions SET worker_claim_expires_at = NULL WHERE uuid = $1`, code.UUID); err != nil {
		t.Fatal(err)
	}
	if retry, found := claimBrainWorker(t, app, "worker-a", 0); !found || retry.WorkerEpoch != claim.WorkerEpoch+2 {
		t.Fatalf("new input after permanent failure = %+v found=%t", retry, found)
	}
}

func TestBrainClaimIdleReleaseAllowsImmediateReclaim(t *testing.T) {
	app := newBrainClaimApp(t)
	code, public := prepareBrainClaimSession(t, app)
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	claim, found := claimBrainWorker(t, app, "worker-a", 0)
	if !found || registerBrainWorker(t, app, claim) != http.StatusOK {
		t.Fatalf("claim = %+v found=%t", claim, found)
	}
	if status := releaseBrainClaim(t, app, claim, "worker-a", "idle"); status != http.StatusOK {
		t.Fatalf("idle release = %d", status)
	}
	if _, found := claimBrainWorker(t, app, "worker-b", 0); found {
		t.Fatal("idle released session claimed without new input")
	}
	setBrainClaimSessionStatus(t, app, code, "running")
	next, found := claimBrainWorker(t, app, "worker-b", 0)
	if !found || next.WorkerEpoch != claim.WorkerEpoch+1 {
		t.Fatalf("claim after idle release = %+v found=%t", next, found)
	}
}

func brainClaimAttempts(t *testing.T, app *testApp, code db.CodeSession) int {
	t.Helper()
	var attempts int
	if err := app.pool.QueryRow(t.Context(), `SELECT worker_claim_attempts FROM code_sessions WHERE uuid = $1`, code.UUID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	return attempts
}

func dispatchBrainClaimSession(t *testing.T, app *testApp, name string) db.CodeSession {
	t.Helper()
	code, public := prepareNamedBrainClaimSession(t, app, name)
	if err := newCodeSessionService(app, nil, nil).DispatchWorker(t.Context(), public, code.ExternalID); err != nil {
		t.Fatal(err)
	}
	return code
}

func TestBrainClaimUndeliveredClaimDoesNotConsumeAttempts(t *testing.T) {
	app := newBrainClaimApp(t)
	code := dispatchBrainClaimSession(t, app, "brain-claim-1")
	lost := requestBrainClaims(t, app, "worker-a", 0, brainClaimIdleBody)
	if len(lost) != 1 {
		t.Fatalf("first claim = %+v", lost)
	}
	if expiresAt, err := time.Parse(time.RFC3339Nano, lost[0].ClaimExpiresAt); err != nil || time.Until(expiresAt) > 11*time.Second {
		t.Fatalf("delivery deadline = %q err=%v, want about 10s", lost[0].ClaimExpiresAt, err)
	}
	if _, found := claimBrainWorker(t, app, "worker-b", 0); found {
		t.Fatal("undelivered claim handed out before its delivery deadline")
	}
	expireBrainClaim(t, app, code)
	delivered, found := claimBrainWorker(t, app, "worker-b", 0)
	if !found || delivered.WorkerEpoch != lost[0].WorkerEpoch+1 {
		t.Fatalf("reclaim after lost delivery = %+v found=%t", delivered, found)
	}
	if status := ackBrainClaim(t, app, lost[0], "worker-a"); status != http.StatusConflict {
		t.Fatalf("superseded ack status = %d", status)
	}
	if attempts := brainClaimAttempts(t, app, code); attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestBrainClaimReturnsBatchesBoundedByMaxTasks(t *testing.T) {
	app := newBrainClaimApp(t)
	dispatchBrainClaimSession(t, app, "brain-claim-2")
	dispatchBrainClaimSession(t, app, "brain-claim-3")
	dispatchBrainClaimSession(t, app, "brain-claim-4")
	body := `{"max_tasks":2,"max_sessions":8,"busy_sessions":0,"resident_sessions":0,"admission_failures":0}`
	first := requestBrainClaims(t, app, "worker-a", 0, body)
	if len(first) != 2 || first[0].SessionID == first[1].SessionID {
		t.Fatalf("first batch = %+v", first)
	}
	if rest := requestBrainClaims(t, app, "worker-a", 0, body); len(rest) != 1 {
		t.Fatalf("second batch = %+v", rest)
	}
}

func TestBrainClaimCircuitOpensForFailingWorker(t *testing.T) {
	app := newBrainClaimApp(t)
	dispatchBrainClaimSession(t, app, "brain-claim-5")
	failing := `{"max_tasks":4,"max_sessions":8,"busy_sessions":0,"resident_sessions":0,"admission_failures":3}`
	if claims := requestBrainClaims(t, app, "worker-a", 0, failing); len(claims) != 0 {
		t.Fatalf("failing worker claimed %+v", claims)
	}
	var openUntil time.Time
	if err := app.pool.QueryRow(t.Context(), `SELECT circuit_open_until FROM brain_workers WHERE external_id = 'worker-a'`).Scan(&openUntil); err != nil {
		t.Fatal(err)
	}
	if hold := time.Until(openUntil); hold <= 10*time.Second || hold > 16*time.Second {
		t.Fatalf("circuit hold = %s, want about 15s", hold)
	}
	if _, found := claimBrainWorker(t, app, "worker-b", 0); !found {
		t.Fatal("healthy worker could not claim while another circuit was open")
	}
	dispatchBrainClaimSession(t, app, "brain-claim-6")
	dispatchBrainClaimSession(t, app, "brain-claim-7")
	if _, err := app.pool.Exec(t.Context(), `UPDATE brain_workers SET circuit_open_until = now() - interval '1 second' WHERE external_id = 'worker-a'`); err != nil {
		t.Fatal(err)
	}
	if probe := requestBrainClaims(t, app, "worker-a", 0, failing); len(probe) != 1 {
		t.Fatalf("half-open probe = %+v, want one task", probe)
	}
	recovered := `{"max_tasks":4,"max_sessions":8,"busy_sessions":1,"resident_sessions":1,"admission_failures":0}`
	if claims := requestBrainClaims(t, app, "worker-a", 0, recovered); len(claims) != 1 {
		t.Fatalf("recovered worker claims = %+v", claims)
	}
}

func TestBrainClaimDelaysMoreLoadedWorkers(t *testing.T) {
	app := newBrainClaimApp(t)
	if claims := requestBrainClaims(t, app, "worker-idle", 0, brainClaimIdleBody); len(claims) != 0 {
		t.Fatalf("idle worker claimed %+v", claims)
	}
	dispatchBrainClaimSession(t, app, "brain-claim-8")
	full := `{"max_tasks":1,"max_sessions":8,"busy_sessions":8,"resident_sessions":8,"admission_failures":0}`
	startedAt := time.Now()
	claims := requestBrainClaims(t, app, "worker-full", 0, full)
	if len(claims) != 1 {
		t.Fatalf("loaded worker claims = %+v", claims)
	}
	if elapsed := time.Since(startedAt); elapsed < 450*time.Millisecond {
		t.Fatalf("loaded worker claimed after %s, want the load delay", elapsed)
	}
}
