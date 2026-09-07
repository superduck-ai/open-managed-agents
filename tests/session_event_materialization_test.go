package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestWorkerPublicMappingRollsBackThreadsAndEvents(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "worker-mapping-transaction", `{"model":"claude-opus-4-6","name":"mapping-transaction"}`, `{"name":"mapping-transaction"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSession, err := app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{
		ExternalID: "cse_" + uuid.NewV4().String(), OrganizationUUID: session.OrganizationUUID, WorkspaceUUID: session.WorkspaceUUID,
		SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
		WorkDir: "/workspace", PermissionMode: "default", Model: "claude-opus-4-6", Status: "active", InitialWorkerEpoch: 1, Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
	sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, nil)
	childID := "sthr_" + uuid.NewV4().String()
	payloads := []json.RawMessage{
		json.RawMessage(`{"id":"sevt_create_` + uuid.NewV4().String() + `","type":"session.thread_created","session_thread_id":` + quoteJSON(childID) + `,"agent_name":"child"}`),
		json.RawMessage(`{"id":"sevt_run_` + uuid.NewV4().String() + `","type":"session.thread_status_running","session_thread_id":` + quoteJSON(childID) + `}`),
	}
	before, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSession.ExternalID, 1, append(payloads, json.RawMessage(`{}`)), nil); err == nil {
		t.Fatal("invalid later payload was silently skipped")
	}
	assertNoWorkerMappingEffects(t, app, session, childID, before)
	// An old epoch must be rejected before a new thread can be created.
	if _, err := app.pool.Exec(t.Context(), "UPDATE code_sessions SET current_worker_epoch=2 WHERE workspace_uuid=$1 AND uuid=$2", session.WorkspaceUUID, codeSession.UUID); err != nil {
		t.Fatal(err)
	}
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSession.ExternalID, 1, payloads, nil); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale mapping result: %v", err)
	}
	assertNoWorkerMappingEffects(t, app, session, childID, before)
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSession.ExternalID, 2, payloads, nil); err != nil {
		t.Fatal(err)
	}
	child, err := app.db.GetSessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID, childID)
	if err != nil || child.ParentThreadUUID == nil || child.Status != "running" {
		t.Fatalf("committed child: %+v, %v", child, err)
	}
	if got := mustSessionRecord(t, app, session.ExternalID).Status; got != "running" {
		t.Fatalf("aggregate state = %s", got)
	}
	committed, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSession.ExternalID, 2, payloads, nil); err != nil {
		t.Fatal(err)
	}
	after, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !after.Equal(committed) {
		t.Fatalf("mapping retry advanced clock: %v, %v", after, err)
	}
}

func assertNoWorkerMappingEffects(t *testing.T, app *testApp, session db.Session, childID string, watermark time.Time) {
	t.Helper()
	if _, err := app.db.GetSessionThread(t.Context(), session.WorkspaceUUID, session.ExternalID, childID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("failed mapping left a child: %v", err)
	}
	if got := mustSessionRecord(t, app, session.ExternalID).Status; got != session.Status {
		t.Fatalf("failed mapping changed status: %s", got)
	}
	after, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !after.Equal(watermark) {
		t.Fatalf("failed mapping advanced clock: %v, %v", after, err)
	}
	events, _, err := app.db.ListSessionEventsPage(t.Context(), db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, Limit: 100})
	if err != nil || len(events) != 0 {
		t.Fatalf("failed mapping left public events: %v, %v", events, err)
	}
}

func TestWorkerInternalEventsCommitPublicMaterializationAtomically(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "worker-internal-recovery", `{"model":"claude-opus-4-6","name":"internal-recovery"}`, `{"name":"internal-recovery"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ExternalID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	started := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"payload":{"type":"system","uuid":"started-` + uuid.NewV4().String() + `","subtype":"task_started","task_id":"recover-agent","task_type":"local_agent","description":"recover child","prompt":"child prompt"}}]}`
	removeFailure := rejectPublicSessionEventWrites(t, app, session.UUID, "")
	defer removeFailure()
	failed := doCodeSessionWorkerRequest(t, app, codeSessionID, "events", started)
	assertError(t, failed, http.StatusInternalServerError, "api_error")
	threads, err := app.db.ListSessionThreads(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || len(threads) != 1 {
		t.Fatalf("failed worker event created threads: %v, %v", threads, err)
	}
	removeFailure()
	postCodeSessionWorkerEvents(t, app, codeSessionID, started)
	threads, err = app.db.ListSessionThreads(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || len(threads) != 2 {
		t.Fatalf("worker retry threads: %v, %v", threads, err)
	}
	var childID string
	for _, thread := range threads {
		if thread.ParentThreadUUID != nil {
			childID = thread.ExternalID
		}
	}
	if childID == "" {
		t.Fatal("worker retry created no child")
	}
	before, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	internal := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"agent_id":"recover-agent","payload":{"type":"assistant","uuid":"answer-` + uuid.NewV4().String() + `","agentId":"recover-agent","message":{"role":"assistant","content":[{"type":"text","text":"recover this public answer"}]}}}]}`
	removeMaterializationFailure := rejectPublicSessionEventWrites(t, app, session.UUID, "")
	defer removeMaterializationFailure()
	failed = doCodeSessionWorkerRequest(t, app, codeSessionID, "internal-events", internal)
	assertError(t, failed, http.StatusInternalServerError, "api_error")
	after, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !after.Equal(before) {
		t.Fatalf("failed materialization advanced clock: %v, %v", after, err)
	}
	// Check storage directly: a history GET must not be what repairs this write.
	childQuery := db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, ThreadExternalID: childID, Types: []string{"agent.message"}, Limit: 100}
	childEvents, _, err := app.db.ListSessionEventsPage(t.Context(), childQuery)
	if err != nil || len(childEvents) != 0 {
		t.Fatalf("failed materialization leaked events: %v, %v", childEvents, err)
	}
	var rawCount int
	var internalSequence int64
	if err := app.pool.QueryRow(t.Context(), "SELECT count(*) FROM code_session_internal_events WHERE workspace_uuid=$1 AND code_session_external_id=$2", session.WorkspaceUUID, codeSessionID).Scan(&rawCount); err != nil || rawCount != 0 {
		t.Fatalf("failed materialization left raw transcript: %d, %v", rawCount, err)
	}
	if err := app.pool.QueryRow(t.Context(), "SELECT last_internal_sequence_num FROM code_sessions WHERE workspace_uuid=$1 AND external_id=$2", session.WorkspaceUUID, codeSessionID).Scan(&internalSequence); err != nil || internalSequence != 0 {
		t.Fatalf("failed materialization advanced internal sequence: %d, %v", internalSequence, err)
	}
	removeMaterializationFailure()
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, internal)
	childEvents, _, err = app.db.ListSessionEventsPage(t.Context(), childQuery)
	if err != nil || len(childEvents) != 1 || !strings.Contains(string(childEvents[0].Payload), "recover this public answer") {
		t.Fatalf("identical retry did not recover public event: %v, %v", childEvents, err)
	}
	committed, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, internal)
	afterRetry, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !afterRetry.Equal(committed) {
		t.Fatalf("internal retry advanced public clock: %v, %v", afterRetry, err)
	}
	var internalCount int
	if err := app.pool.QueryRow(t.Context(), "SELECT count(*) FROM code_session_internal_events WHERE workspace_uuid=$1 AND code_session_external_id=$2", session.WorkspaceUUID, codeSessionID).Scan(&internalCount); err != nil || internalCount != 1 {
		t.Fatalf("internal retry duplicated source: %d, %v", internalCount, err)
	}
}

func TestDelayedThreadMappingMaterializesFullTranscriptAcrossPages(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "delayed-thread-mapping", `{"model":"claude-opus-4-6","name":"delayed-mapping"}`, `{"name":"delayed-mapping"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ExternalID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+quoteJSON(epoch)+`,"events":[
		{"agent_id":"late-agent","payload":{"type":"assistant","uuid":"before-compact","message":{"role":"assistant","content":[{"type":"text","text":"before compaction"}]}}},
		{"agent_id":"late-agent","is_compaction":true,"payload":{"type":"user","uuid":"compact","message":{"role":"user","content":"compaction summary"}}},
		{"agent_id":"late-agent","payload":{"type":"assistant","uuid":"after-compact","message":{"role":"assistant","content":[{"type":"text","text":"after compaction"}]}}}
	]}`)
	resume := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?subagents=true")
	assertInternalEventUUIDs(t, resume.Data, []string{"compact", "after-compact"})

	// Put the target mapping on page two. Both raw materialization and direct
	// public owner inference must find it without changing the primary scope.
	service := codesessions.NewServiceWithCredentials(app.db, app.credentials, nil)
	sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, nil)
	payloads := make([]json.RawMessage, 500)
	for i := range payloads {
		payloads[i] = json.RawMessage(fmt.Sprintf(`{"id":"sevt_%s","type":"session.thread_created","session_thread_id":"sthr_%s","task_id":"other-%d"}`, uuid.NewV4(), uuid.NewV4(), i))
	}
	epochValue, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSessionID, epochValue, payloads, nil); err != nil {
		t.Fatal(err)
	}
	before, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	started := `{"worker_epoch":` + quoteJSON(epoch) + `,"events":[{"payload":{"type":"system","uuid":"late-start","subtype":"task_started","task_id":"late-agent","task_type":"local_agent","description":"late child"}}]}`
	removeFailure := rejectPublicSessionEventWrites(t, app, session.UUID, "agent.message")
	defer removeFailure()
	assertError(t, doCodeSessionWorkerRequest(t, app, codeSessionID, "events", started), http.StatusInternalServerError, "api_error")
	threads, err := app.db.ListSessionThreads(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || len(threads) != 501 {
		t.Fatalf("failed late materialization left a new thread: %d, %v", len(threads), err)
	}
	afterFailure, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil || !afterFailure.Equal(before) {
		t.Fatalf("failed late materialization advanced public clock: %v, %v", afterFailure, err)
	}
	if got := mustSessionRecord(t, app, session.ExternalID).Status; got != session.Status {
		t.Fatalf("failed late materialization changed session state: %s", got)
	}
	removeFailure()
	postCodeSessionWorkerEvents(t, app, codeSessionID, started)
	var childID string
	if err := app.pool.QueryRow(t.Context(), `SELECT payload->>'session_thread_id' FROM session_events WHERE workspace_uuid=$1 AND session_uuid=$2 AND event_type='session.thread_created' AND payload->>'task_id'='late-agent'`, session.WorkspaceUUID, session.UUID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	childQuery := db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, ThreadExternalID: childID, Limit: 100}
	child, _, err := app.db.ListSessionEventsPage(t.Context(), childQuery)
	if err != nil || len(child) != 3 {
		t.Fatalf("delayed transcript length = %d, %v", len(child), err)
	}
	for i, want := range []string{"before compaction", "compaction summary", "after compaction"} {
		if !strings.Contains(string(child[i].Payload), want) {
			t.Fatalf("delayed transcript event %d missing %q: %s", i, want, child[i].Payload)
		}
	}
	message := json.RawMessage(`{"id":"sevt_` + uuid.NewV4().String() + `","type":"agent.message","agent_id":"late-agent","content":[{"type":"text","text":"direct owner after first page"}]}`)
	if err := service.CommitWorkerSessionEvents(t.Context(), codeSessionID, epochValue, []json.RawMessage{message}, nil); err != nil {
		t.Fatal(err)
	}
	child, _, err = app.db.ListSessionEventsPage(t.Context(), childQuery)
	if err != nil || len(child) != 4 || !strings.Contains(string(child[3].Payload), "direct owner after first page") {
		t.Fatalf("direct public output did not find page-two owner: %v, %v", child, err)
	}
	primary := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message&limit=100", defaultTestKey)
	if len(primary.Data) != 0 {
		t.Fatalf("late child messages leaked to primary: %v", primary.Data)
	}
	resume = getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?subagents=true")
	assertInternalEventUUIDs(t, resume.Data, []string{"compact", "after-compact"})
}

func rejectPublicSessionEventWrites(t *testing.T, app *testApp, sessionUUID, eventType string) func() {
	t.Helper()
	name := `"test_public_event_failure_` + uuid.NewV4().String() + `"`
	_, err := app.pool.Exec(t.Context(), `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.session_uuid::text = TG_ARGV[0] AND (TG_ARGV[1] = '' OR NEW.event_type = TG_ARGV[1]) THEN RAISE EXCEPTION 'test rejected public event'; END IF; RETURN NEW; END $$`)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	cleanup := func() {
		if removed {
			return
		}
		removed = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := app.pool.Exec(ctx, `DROP TRIGGER IF EXISTS `+name+` ON session_events`); err != nil {
			t.Error(err)
		}
		if _, err := app.pool.Exec(ctx, `DROP FUNCTION `+name+`() `); err != nil {
			t.Error(err)
		}
	}
	if _, err := app.pool.Exec(t.Context(), `CREATE TRIGGER `+name+` BEFORE INSERT ON session_events FOR EACH ROW EXECUTE FUNCTION `+name+`('`+strings.ReplaceAll(sessionUUID, "'", "''")+`','`+strings.ReplaceAll(eventType, "'", "''")+`')`); err != nil {
		cleanup()
		t.Fatal(err)
	}
	return cleanup
}

func TestWorkerBatchMaterializesTranscriptBeforeThreadCompletion(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "batch-materialization-order", `{"model":"claude-opus-4-6","name":"batch-materialization-order"}`, `{"name":"batch-materialization-order"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSessionID := launchLocalCodeSession(t, app, response.ID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[{"agent_id":"batch-late-agent","payload":{"type":"assistant","uuid":"batch-late-message","message":{"role":"assistant","content":[{"type":"text","text":"body arrived before mapping"}]}}}]}`)
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[
		{"payload":{"type":"system","uuid":"batch-late-start","subtype":"task_started","task_id":"batch-late-agent","description":"late child"}},
		{"payload":{"type":"system","uuid":"batch-late-end","subtype":"task_notification","task_id":"batch-late-agent","status":"completed"}}
	]}`)
	events, _, err := app.db.ListSessionEventsPage(t.Context(), db.ListSessionEventsPageParams{WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, Limit: 100, Order: "asc"})
	if err != nil {
		t.Fatal(err)
	}
	messageIndex, idleIndex := -1, -1
	for i, event := range events {
		if event.EventType == "agent.message" && strings.Contains(string(event.Payload), "body arrived before mapping") {
			messageIndex = i
		}
		if event.EventType == "session.thread_status_idle" {
			idleIndex = i
		}
	}
	if messageIndex < 0 || idleIndex < 0 || messageIndex >= idleIndex {
		t.Fatalf("late materialization order: message=%d idle=%d", messageIndex, idleIndex)
	}
}
