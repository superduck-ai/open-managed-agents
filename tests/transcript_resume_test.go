package tests

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/environments"
)

func transcriptHTTPBytes(t *testing.T, app *testApp, id, suffix string) []byte {
	t.Helper()
	response := doCodeSessionWorkerRequestWithMethod(t, app, http.MethodGet, id, suffix, "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("transcript status: %d", response.StatusCode)
	}
	return []byte(readAll(t, response.Body))
}

func TestTranscriptArchivePreservesIdleReclaimResume(t *testing.T) {
	f := newSandboxLifecycleFixture(t)
	seedArchiveEvents(t, f.app, f.code, make([]db.AppendCodeSessionInternalEventInput, 3))
	before := transcriptHTTPBytes(t, f.app, f.code.ExternalID, "internal-events")
	killer := newLifecycleProvider(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	lifecycle := environments.NewSandboxLifecycle(f.app.db, killer, config.SandboxLifecycleConfig{Enabled: true, IdleTimeout: 24 * time.Hour}, nil)
	if err := lifecycle.Reclaim(t.Context(), f.target); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := f.app.pool.QueryRow(t.Context(), "select stop_reason from environment_sandboxes where uuid=$1", f.target.SandboxUUID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "idle_timeout" {
		t.Fatalf("stop reason: %s", reason)
	}
	service := newTranscriptRetentionService(t, f.app, f.app.store, transcriptPolicy())
	if err := service.Archive(t.Context(), transcriptScope(f.code), true); err != nil {
		t.Fatal(err)
	}
	after := transcriptHTTPBytes(t, f.app, f.code.ExternalID, "internal-events")
	if !bytes.Equal(before, after) {
		t.Fatal("idle reclamation transcript changed")
	}
	sendSessionEvents(t, f.app, f.session.ExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"resume archived-check"}]}]}`, defaultTestKey)
	provider := &recordingRunnerProvider{sandboxID: f.target.ProviderSandboxID + "-resumed"}
	cfg := f.app.cfg
	cfg.CodeSession.SandboxAPIBaseURL = "http://sandbox-api.example.test"
	runUntilSandboxCreated(t, t.Context(), newManagedAgentRunner(t, f.app, provider, cfg), provider)
	resumed := transcriptHTTPBytes(t, f.app, f.code.ExternalID, "internal-events")
	if !bytes.Equal(before, resumed) {
		t.Fatal("resumed worker history changed")
	}
}

func TestTranscriptArchiveConcurrentCompactionAndEpoch(t *testing.T) {
	objects := &payloadFaultStore{fakeStore: newFakeStore("archive-resume")}
	app := newPayloadIntegrationApp(t, objects)
	session, _ := newPayloadIntegrationSession(t, app)
	a, b := "agent_a", "agent_b"
	seedArchiveEvents(t, app, session, []db.AppendCodeSessionInternalEventInput{{}, {AgentID: &a}, {AgentID: &b}, {IsCompaction: true}, {AgentID: &a, IsCompaction: true}, {AgentID: &b, IsCompaction: true}, {}, {AgentID: &a}, {AgentID: &b}})
	var expected [2][]byte
	calls := 0
	objects.afterUpload = func(string) error {
		if calls > 0 {
			return nil
		}
		calls++
		oldEpoch := session.CurrentWorkerEpoch
		epoch := registerCodeSessionWorker(t, app, session.ExternalID)
		assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, session.ExternalID, "internal-events", `{"worker_epoch":"`+epoch+`","events":[{"is_compaction":true,"payload":{"type":"assistant","uuid":"during-archive","message":{"role":"assistant","content":[{"type":"text","text":"new boundary"}]}}}]}`, http.StatusOK, "")
		assertCodeSessionWorkerWriteStatus(t, app, http.MethodPost, session.ExternalID, "internal-events", `{"worker_epoch":"`+strconv.FormatInt(oldEpoch, 10)+`","events":[]}`, http.StatusConflict, "conflict_error")
		expected[0] = transcriptHTTPBytes(t, app, session.ExternalID, "internal-events")
		expected[1] = transcriptHTTPBytes(t, app, session.ExternalID, "internal-events?subagents=true")
		return nil
	}
	service := newTranscriptRetentionService(t, app, objects, transcriptPolicy())
	if err := service.Archive(t.Context(), transcriptScope(session), false); err != nil {
		t.Fatal(err)
	}
	for i, suffix := range []string{"internal-events", "internal-events?subagents=true"} {
		if !bytes.Equal(expected[i], transcriptHTTPBytes(t, app, session.ExternalID, suffix)) {
			t.Fatal("HTTP resume bytes changed")
		}
	}
}
