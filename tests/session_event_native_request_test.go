package tests

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"uuid"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

// These payloads match the original CLI 2.1.120 capture: stream frames are
// ephemeral, and each assistant contains one block without a block index.
func TestNativeWorkerRequestBlocksAcrossBatches(t *testing.T) {
	p := newNativeWorkerProbe(t)
	scanner := p.openStream("")
	requestID := "msg_native_blocks"
	p.post(nativeStream("", `{"type":"message_start","message":{"id":"`+requestID+`","model":"test"}}`))
	start := assertNextSessionFrameType(t, scanner, "span.model_request_start")
	startID := sessionEventStringField(t, start, "id")
	ids := make(map[string]bool)
	for i, text := range []string{"first complete block", "second complete block"} {
		// A single upload must publish each preview individually, in source order.
		p.post(
			nativeStream("", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, i)),
			nativeStream("", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, i, quoteJSON(text))),
		)
		preview := assertNextSessionFrameType(t, scanner, "event_start")
		var began struct {
			Event struct {
				ID string `json:"id"`
			} `json:"event"`
		}
		if err := json.Unmarshal(preview, &began); err != nil {
			t.Fatal(err)
		}
		delta := assertNextSessionFrameType(t, scanner, "event_delta")
		if sessionEventStringField(t, delta, "event_id") != began.Event.ID || !strings.Contains(string(delta), text) {
			t.Fatalf("block %d preview identity/content: start=%s delta=%s", i, preview, delta)
		}
		assistant := nativeAssistant("", requestID, text)
		p.post(assistant)
		final := assertNextSessionFrameType(t, scanner, "agent.message")
		id := sessionEventStringField(t, final, "id")
		if id != began.Event.ID || ids[id] || !strings.Contains(string(final), text) {
			t.Fatalf("block %d identity/content: preview=%s final=%s", i, preview, final)
		}
		ids[id] = true
		p.post(assistant) // Same source UUID must not create another final.
		p.post(nativeStream("", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, i)))
	}
	p.post(nativeStream("", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`))
	p.post(nativeStream("", `{"type":"message_stop"}`))
	end := assertNextSessionFrameType(t, scanner, "span.model_request_end")
	if sessionEventStringField(t, end, "model_request_start_id") != startID {
		t.Fatalf("end lost start: %s", end)
	}
	p.post(nativeResult(""))
	assertNextSessionFrameType(t, scanner, "session.thread_status_idle")
	assertNextSessionFrameType(t, scanner, "session.status_idle")
	history := listSessionEvents(t, p.app, p.sessionID, "limit=100&order=asc", defaultTestKey)
	counts := make(map[string]int)
	for _, raw := range history.Data {
		counts[sessionEventStringField(t, raw, "type")]++
	}
	if counts["span.model_request_start"] != 1 || counts["span.model_request_end"] != 1 || counts["agent.message"] != 2 {
		t.Fatalf("native boundaries/finals were duplicated: %s", history.Data)
	}
}

func TestNativeWorkerRequestReplayCannotRetagOrCloseNextRequest(t *testing.T) {
	p := newNativeWorkerProbe(t)
	oldStart := nativeStream("", `{"type":"message_start","message":{"id":"msg_old","model":"test"}}`)
	oldBlock := nativeStream("", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
	oldDelta := nativeStream("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"PRIVATE_PREVIEW_ONLY"}}`)
	oldStop, oldResult := nativeStream("", `{"type":"message_stop"}`), nativeResult("")
	p.post(oldStart, oldBlock, oldDelta)
	p.post(oldStop, oldResult)
	putCodeSessionWorkerState(t, p.app, p.workerID, `{"worker_epoch":`+p.epoch+`,"worker_status":"running"}`)
	scanner := p.openStream("")
	p.post(nativeStream("", `{"type":"message_start","message":{"id":"msg_next","model":"test"}}`))
	start := assertNextSessionFrameType(t, scanner, "span.model_request_start")
	p.post(nativeStream("", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`))
	assertNextSessionFrameType(t, scanner, "event_start")
	// The old delta-only HTTP handler may acquire the Session lock after its
	// retry and the next request. It must be ignored before consulting active B.
	p.post(oldDelta)
	p.post(oldStart, oldBlock, oldDelta, oldStop, oldResult)
	p.post(nativeStream("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"fresh next preview"}}`))
	frame := assertNextSessionFrameType(t, scanner, "event_delta")
	if !strings.Contains(string(frame), "fresh next preview") {
		t.Fatalf("replay leaked or closed next request: %s", frame)
	}
	p.post(nativeStream("", `{"type":"message_stop"}`))
	end := assertNextSessionFrameType(t, scanner, "span.model_request_end")
	if sessionEventStringField(t, end, "model_request_start_id") != sessionEventStringField(t, start, "id") {
		t.Fatalf("old stop closed the next request: %s", end)
	}
	p.post(nativeResult(""))
	assertNextSessionFrameType(t, scanner, "session.thread_status_idle")
	assertNextSessionFrameType(t, scanner, "session.status_idle")
	history := listSessionEvents(t, p.app, p.sessionID, "limit=100&order=asc", defaultTestKey)
	if eventPageContains(history, "PRIVATE_PREVIEW_ONLY") || eventPageContains(history, "fresh next preview") {
		t.Fatal("preview body persisted in public history")
	}
	worker, found, err := p.app.db.GetCodeSession(t.Context(), p.workerID)
	if err != nil || !found {
		t.Fatalf("worker lookup: %v", err)
	}
	var count int
	var receipts string
	err = p.app.pool.QueryRow(t.Context(), `SELECT count(*), COALESCE(jsonb_agg(to_jsonb(r))::text, '[]') FROM code_session_worker_event_receipts r WHERE code_session_uuid = $1`, worker.UUID).Scan(&count, &receipts)
	if err != nil || count != 10 || strings.Contains(receipts, "PRIVATE_PREVIEW_ONLY") || strings.Contains(receipts, "fresh next preview") {
		t.Fatalf("receipt dedup/body retention: count=%d err=%v", count, err)
	}
	deleteSession(t, p.app, p.sessionID)
	if err := p.app.pool.QueryRow(t.Context(), `SELECT count(*) FROM code_session_worker_event_receipts WHERE code_session_uuid = $1`, worker.UUID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("Session deletion left receipts: count=%d err=%v", count, err)
	}
}

func TestNativeWorkerRequestConflictRollsBackWholeBatch(t *testing.T) {
	p := newNativeWorkerProbe(t)
	accepted := nativeStream("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"accepted preview"}}`)
	p.post(accepted)
	before := listSessionEvents(t, p.app, p.sessionID, "limit=100&order=asc", defaultTestKey)
	worker, found, err := p.app.db.GetCodeSession(t.Context(), p.workerID)
	if err != nil || !found {
		t.Fatalf("worker lookup: %v", err)
	}
	scanner := p.openStream("")
	start := nativeStream("", `{"type":"message_start","message":{"id":"msg_rolled_back","model":"test"}}`)
	block := nativeStream("", `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`)
	delta := nativeStream("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"rolled back preview"}}`)
	conflicting := strings.Replace(accepted, "accepted preview", "changed preview", 1)
	payloads := []string{start, block, delta, conflicting}
	wrapped := make([]string, len(payloads))
	for i, payload := range payloads {
		wrapped[i] = `{"ephemeral":true,"payload":` + payload + `}`
	}
	response := doCodeSessionWorkerRequest(t, p.app, p.workerID, "events", `{"worker_epoch":`+p.epoch+`,"events":[`+strings.Join(wrapped, ",")+`]}`)
	assertError(t, response, http.StatusConflict, "conflict_error")
	after := listSessionEvents(t, p.app, p.sessionID, "limit=100&order=asc", defaultTestKey)
	if !reflect.DeepEqual(before.Data, after.Data) {
		t.Fatalf("conflict persisted preceding boundaries: %s", after.Data)
	}
	var count int
	if err := p.app.pool.QueryRow(t.Context(), `SELECT count(*) FROM code_session_worker_event_receipts WHERE code_session_uuid = $1`, worker.UUID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("conflict persisted preceding receipts: count=%d err=%v", count, err)
	}
	p.post(nativeStream("", `{"type":"message_start","message":{"id":"msg_after_conflict","model":"test"}}`))
	frame := assertNextSessionFrameType(t, scanner, "span.model_request_start")
	if sessionEventStringField(t, frame, "id") != maevents.ModelRequestEventID(p.workerID, "msg_after_conflict", "start") {
		t.Fatalf("conflict published a rolled-back boundary/preview: %s", frame)
	}
}

func TestNativeWorkerRequestParentScopesStayIndependent(t *testing.T) {
	p := newNativeWorkerProbe(t)
	parent := "tool_native_child"
	child := maevents.ClaudeTaskThreadID(p.workerID, parent)
	p.post(`{"uuid":` + quoteJSON(uuid.NewV4().String()) + `,"type":"session.thread_created","session_thread_id":` + quoteJSON(child) + `,"agent_name":"native child"}`)
	p.post(`{"uuid":` + quoteJSON(uuid.NewV4().String()) + `,"type":"session.thread_status_running","session_thread_id":` + quoteJSON(child) + `}`)
	primaryScanner, childScanner := p.openStream(""), p.openStream(child)
	p.post(nativeStream("", `{"type":"message_start","message":{"id":"msg_primary","model":"test"}}`))
	primaryStart := assertNextSessionFrameType(t, primaryScanner, "span.model_request_start")
	p.post(nativeStream(parent, `{"type":"message_start","message":{"id":"msg_child","model":"test"}}`))
	childStart := assertNextSessionFrameType(t, childScanner, "span.model_request_start")
	for _, scope := range []struct {
		parent  string
		scanner *bufio.Scanner
	}{{"", primaryScanner}, {parent, childScanner}} {
		p.post(nativeStream(scope.parent, `{"type":"content_block_start","index":0,"content_block":{"type":"text"}}`))
		assertNextSessionFrameType(t, scope.scanner, "event_start")
	}
	p.post(nativeAssistant(parent, "msg_child", "child complete"))
	childFinal := assertNextSessionFrameType(t, childScanner, "agent.message")
	if sessionEventStringField(t, childFinal, "session_thread_id") != child {
		t.Fatalf("child assistant moved to primary: %s", childFinal)
	}
	p.post(nativeStream(parent, `{"type":"message_stop"}`))
	childEnd := assertNextSessionFrameType(t, childScanner, "span.model_request_end")
	if sessionEventStringField(t, childEnd, "model_request_start_id") != sessionEventStringField(t, childStart, "id") {
		t.Fatalf("child end lost scope: %s", childEnd)
	}
	p.post(nativeResult(parent))
	childIdle := assertNextSessionFrameType(t, primaryScanner, "session.thread_status_idle")
	if sessionEventStringField(t, childIdle, "session_thread_id") != child {
		t.Fatalf("child result idled primary: %s", childIdle)
	}
	if current := mustSessionRecord(t, p.app, p.sessionID); current.Status != "running" {
		t.Fatalf("child result changed aggregate Session status: %s", current.Status)
	}
	p.post(nativeStream("", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"primary continues"}}`))
	frame := assertNextSessionFrameType(t, primaryScanner, "event_delta")
	if !strings.Contains(string(frame), "primary continues") {
		t.Fatalf("child close interrupted primary: %s", frame)
	}
	p.post(nativeAssistant("", "msg_primary", "primary complete"))
	assertNextSessionFrameType(t, primaryScanner, "agent.message")
	p.post(nativeStream("", `{"type":"message_stop"}`))
	primaryEnd := assertNextSessionFrameType(t, primaryScanner, "span.model_request_end")
	if sessionEventStringField(t, primaryEnd, "model_request_start_id") != sessionEventStringField(t, primaryStart, "id") {
		t.Fatalf("primary end lost scope: %s", primaryEnd)
	}
	primaryHistory := listSessionEvents(t, p.app, p.sessionID, "limit=100", defaultTestKey)
	childHistory := listThreadEvents(t, p.app, p.sessionID, child, defaultTestKey)
	if eventPageContains(primaryHistory, "child complete") || eventPageContains(childHistory, "primary complete") {
		t.Fatal("native scopes mixed histories")
	}
}

type nativeWorkerProbe struct {
	t                          *testing.T
	app                        *testApp
	sessionID, workerID, epoch string
}

func newNativeWorkerProbe(t *testing.T) nativeWorkerProbe {
	t.Helper()
	app, agent, env := newSessionEventTestApp(t, "native-worker-request", `{"model":"claude-opus-4-6","name":"native-worker-request"}`, `{"name":"native-worker-request"}`)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	worker := launchLocalCodeSession(t, app, session.ID)
	epoch := registerCodeSessionWorker(t, app, worker)
	putCodeSessionWorkerState(t, app, worker, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	return nativeWorkerProbe{t: t, app: app, sessionID: session.ID, workerID: worker, epoch: epoch}
}

func (p nativeWorkerProbe) post(payloads ...string) {
	p.t.Helper()
	events := make([]string, len(payloads))
	for i, raw := range payloads {
		var source struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(raw), &source); err != nil {
			p.t.Fatal(err)
		}
		events[i] = fmt.Sprintf(`{"ephemeral":%t,"payload":%s}`, source.Type == "stream_event", raw)
	}
	response := doCodeSessionWorkerRequest(p.t, p.app, p.workerID, "events", `{"worker_epoch":`+p.epoch+`,"events":[`+strings.Join(events, ",")+`]}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		p.t.Fatalf("native upload status=%d: %s", response.StatusCode, readAll(p.t, response.Body))
	}
}

func (p nativeWorkerProbe) openStream(thread string) *bufio.Scanner {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(p.t.Context(), 20*time.Second)
	p.t.Cleanup(cancel)
	path := "/v1/sessions/" + p.sessionID + "/events/stream"
	if thread != "" {
		path = "/v1/sessions/" + p.sessionID + "/threads/" + thread + "/stream"
	}
	response := openSessionEventStream(p.t, p.app, ctx, path+"?beta=true&event_deltas=agent.message")
	p.t.Cleanup(func() { response.Body.Close() })
	return bufio.NewScanner(response.Body)
}

func nativeStream(parent, event string) string {
	return `{"type":"stream_event","uuid":` + quoteJSON(uuid.NewV4().String()) + `,"session_id":"native-cli","parent_tool_use_id":` + nativeParent(parent) + `,"event":` + event + `}`
}

func nativeAssistant(parent, request, text string) string {
	return `{"type":"assistant","uuid":` + quoteJSON(uuid.NewV4().String()) + `,"parent_tool_use_id":` + nativeParent(parent) + `,"message":{"id":` + quoteJSON(request) + `,"role":"assistant","content":[{"type":"text","text":` + quoteJSON(text) + `}]}}`
}

func nativeResult(parent string) string {
	return `{"type":"result","uuid":` + quoteJSON(uuid.NewV4().String()) + `,"parent_tool_use_id":` + nativeParent(parent) + `,"subtype":"success","is_error":false,"stop_reason":"end_turn","result":"done"}`
}

func nativeParent(parent string) string {
	if parent == "" {
		return "null"
	}
	return quoteJSON(parent)
}
