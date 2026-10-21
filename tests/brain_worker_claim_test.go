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

const brainClaimTestToken = "brain-claim-test-token-0123456789abcdef"

type brainClaimTestResponse struct {
	SessionID      string `json:"session_id"`
	WorkerEpoch    int64  `json:"worker_epoch"`
	IngressToken   string `json:"ingress_token"`
	ModelToken     string `json:"model_token"`
	ClaimExpiresAt string `json:"claim_expires_at"`
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
	return code, public
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

func claimBrainWorker(t *testing.T, app *testApp, workerID string, blockMS int) (brainClaimTestResponse, bool) {
	t.Helper()
	response := doBrainClaimRequest(t, app, http.MethodPost, "/v1/code/brain/claims?block_ms="+strconv.Itoa(blockMS), brainClaimTestToken, workerID, "")
	defer response.Body.Close()
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("claim Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	if response.StatusCode == http.StatusNoContent {
		return brainClaimTestResponse{}, false
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("claim status = %d: %s", response.StatusCode, readAll(t, response.Body))
	}
	var claim brainClaimTestResponse
	if err := json.NewDecoder(response.Body).Decode(&claim); err != nil {
		t.Fatal(err)
	}
	return claim, true
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
		{"block too long", "/v1/code/brain/claims?block_ms=30001", brainClaimTestToken, "worker", "", http.StatusBadRequest},
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
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, app.baseURL+"/v1/code/brain/claims?block_ms=10000", nil)
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
	var claim brainClaimTestResponse
	if err := json.NewDecoder(result.response.Body).Decode(&claim); err != nil {
		t.Fatal(err)
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
