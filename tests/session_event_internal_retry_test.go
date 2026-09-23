package tests

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestWorkerInternalRetryConflictsRollBackBatch(t *testing.T) {
	app, agent, env := newSessionEventTestApp(t, "internal-event-retry", `{"model":"claude-opus-4-6","name":"internal-event-retry"}`, `{"name":"internal-event-retry"}`)
	response := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	session := mustSessionRecord(t, app, response.ID)
	codeSessionID := launchLocalCodeSession(t, app, session.ExternalID)
	epoch := registerCodeSessionWorker(t, app, codeSessionID)
	postCodeSessionWorkerEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[{"payload":{"type":"system","uuid":"retry-start","subtype":"task_started","task_id":"retry-agent","task_type":"local_agent","description":"retry child"}}]}`)
	threads, err := app.db.ListSessionThreads(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	childID := ""
	for _, thread := range threads {
		if thread.ParentThreadUUID != nil {
			childID = thread.ExternalID
		}
	}
	if childID == "" {
		t.Fatal("missing child thread")
	}
	original := `{"agent_id":"retry-agent","is_compaction":false,"event_metadata":{"batch":1},"payload":{"type":"assistant","uuid":"retry-source","message":{"role":"assistant","content":[{"type":"text","text":"original answer"}]},"value":9007199254740993}}`
	postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[`+original+`]}`)
	workerBefore, err := getCodeSession(app, t.Context(), codeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	rawBefore := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?subagents=true")
	publicBefore := listThreadEvents(t, app, session.ExternalID, childID, defaultTestKey)
	watermark, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		worker, err := getCodeSession(app, t.Context(), codeSessionID)
		if err != nil || worker.LastInternalSequenceNum != workerBefore.LastInternalSequenceNum {
			t.Fatalf("retry changed internal sequence: %v", err)
		}
		if raw := getCodeSessionWorkerInternalEvents(t, app, codeSessionID, "internal-events?subagents=true"); !reflect.DeepEqual(raw, rawBefore) {
			t.Fatal("retry changed stored transcript, ownership, or compaction")
		}
		if public := listThreadEvents(t, app, session.ExternalID, childID, defaultTestKey); !reflect.DeepEqual(public.Data, publicBefore.Data) {
			t.Fatal("retry changed public history")
		}
		progress, err := app.db.SessionEventWatermark(t.Context(), session.WorkspaceUUID, session.ExternalID)
		if err != nil || !progress.Equal(watermark) {
			t.Fatalf("retry advanced public clock: %v", err)
		}
	}
	for _, change := range []struct{ name, from, to string }{
		{"payload", "original answer", "changed answer"},
		{"number precision", "9007199254740993", "9007199254740992"},
		{"type", `"type":"assistant"`, `"type":"user"`},
		{"owner", `"agent_id":"retry-agent"`, `"agent_id":"other-agent"`},
		{"compaction", `"is_compaction":false`, `"is_compaction":true`},
		{"metadata", `"batch":1`, `"batch":2`},
		{"missing metadata", `"event_metadata":{"batch":1},`, ""},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := strings.Replace(original, change.from, change.to, 1)
			// This fresh event precedes the conflict; it must also roll back.
			fresh := strings.ReplaceAll(original, "retry-source", "before-conflict")
			body := `{"worker_epoch":` + epoch + `,"events":[` + fresh + `,` + changed + `]}`
			assertError(t, doCodeSessionWorkerRequest(t, app, codeSessionID, "internal-events", body), http.StatusConflict, "conflict_error")
			assertUnchanged(t)
		})
	}
	// JSONB equality accepts alternate spellings without overwriting the
	// original JSON, internal event ID, timestamp, metadata or sequence.
	equivalent := `{"payload":{"value":9007199254740993.0,"uuid":"retry-source","type":"assistant","message":{"content":[{"text":"original answer","type":"text"}],"role":"assistant"}},"event_metadata":{"batch":1.0},"agent_id":"retry-agent"}`
	for _, retry := range []string{original, equivalent} {
		postCodeSessionWorkerInternalEvents(t, app, codeSessionID, `{"worker_epoch":`+epoch+`,"events":[`+retry+`]}`)
		assertUnchanged(t)
	}
	// The same source UUID in another Code Session is a separate event.
	other := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	otherID := launchLocalCodeSession(t, app, other.ID)
	otherEpoch := registerCodeSessionWorker(t, app, otherID)
	postCodeSessionWorkerInternalEvents(t, app, otherID, `{"worker_epoch":`+otherEpoch+`,"events":[`+original+`]}`)
	assertInternalEventUUIDs(t, getCodeSessionWorkerInternalEvents(t, app, otherID, "internal-events?subagents=true").Data, []string{"retry-source"})
}
