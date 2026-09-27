package tests

import (
	"bytes"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionRejectsInvalidUserContentBlocks(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("invalid-user-content"))
	worker, _ := newPayloadIntegrationSession(t, app)
	for _, block := range []string{
		`{"type":"redacted"}`,
		`{"type":"unknown","text":"hello"}`,
		`{"type":"text","text":""}`,
		`{"type":"image"}`,
		`{"type":"image","source":{"type":"url","url":""}}`,
		`{"type":"document","source":{"type":"mystery"}}`,
	} {
		resp := doSessionRequest(t, app, http.MethodPost, "/v1/sessions/"+worker.SessionExternalID+"/events?beta=true", strings.NewReader(`{"events":[{"type":"user.message","content":[`+block+`]}]}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	}
}

func TestSessionUpdatedEventsContainOnlyChangedFields(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("session-updated-fields"))
	worker, _ := newPayloadIntegrationSession(t, app)
	sessionID := worker.SessionExternalID
	updateSession(t, app, sessionID, `{}`)
	if events := listSessionEvents(t, app, sessionID, "types[]=session.updated", defaultTestKey); len(events.Data) != 0 {
		t.Fatalf("empty update emitted event: %s", events.Data)
	}
	updateSession(t, app, sessionID, `{"title":"changed"}`)
	events := listSessionEvents(t, app, sessionID, "types[]=session.updated", defaultTestKey)
	if len(events.Data) != 1 {
		t.Fatalf("title update events: %s", events.Data)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(events.Data[0], &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload["title"]) != `"changed"` || len(payload["agent"]) != 0 || len(payload["metadata"]) != 0 {
		t.Fatalf("title update included unchanged fields: %s", events.Data[0])
	}
	updateSession(t, app, sessionID, `{"title":"changed"}`)
	if events := listSessionEvents(t, app, sessionID, "types[]=session.updated", defaultTestKey); len(events.Data) != 1 {
		t.Fatalf("same-value update emitted event: %s", events.Data)
	}
	updateSession(t, app, sessionID, `{"metadata":{"priority":"high"}}`)
	events = listSessionEvents(t, app, sessionID, "types[]=session.updated", defaultTestKey)
	if len(events.Data) != 2 {
		t.Fatalf("metadata update events: %s", events.Data)
	}
	payload = nil
	if err := json.Unmarshal(events.Data[1], &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload["metadata"]) == 0 || len(payload["title"]) != 0 || len(payload["agent"]) != 0 {
		t.Fatalf("metadata update included unchanged fields: %s", events.Data[1])
	}
}

func TestArchiveIdleSessionEmitsTermination(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("archive-idle-termination"))
	worker, _ := newPayloadIntegrationSession(t, app)
	archived := archiveSession(t, app, worker.SessionExternalID)
	if archived.Status != "terminated" {
		t.Fatalf("archived idle session status = %s", archived.Status)
	}
	events := listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.status_terminated&types[]=session.thread_status_terminated", defaultTestKey)
	if len(events.Data) != 2 || sessionEventStringField(t, events.Data[0], "type") != "session.thread_status_terminated" || sessionEventStringField(t, events.Data[1], "type") != "session.status_terminated" {
		t.Fatalf("idle archive termination history: %s", events.Data)
	}
	stored, found, err := app.db.GetCodeSession(t.Context(), worker.ExternalID)
	if err != nil || !found || stored.Status != "terminated" {
		t.Fatalf("idle archive left worker active: found=%t status=%s err=%v", found, stored.Status, err)
	}
}

func TestArchiveIdleThreadEmitsTermination(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("archive-idle-thread"))
	worker, _ := newPayloadIntegrationSession(t, app)
	threads := listSessionThreads(t, app, worker.SessionExternalID, defaultTestKey)
	if len(threads.Data) != 1 {
		t.Fatalf("primary thread: %+v", threads.Data)
	}
	threadID := threads.Data[0].ID
	archived := archiveSessionThread(t, app, worker.SessionExternalID, threadID)
	if archived.Status != "terminated" {
		t.Fatalf("archived thread status = %s", archived.Status)
	}
	events := listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.thread_status_terminated", defaultTestKey)
	if len(events.Data) != 1 || sessionEventStringField(t, events.Data[0], "session_thread_id") != threadID {
		t.Fatalf("thread archive missing termination event: %s", events.Data)
	}
	threadEvents := listThreadEvents(t, app, worker.SessionExternalID, threadID, defaultTestKey)
	if len(threadEvents.Data) != 2 || sessionEventStringField(t, threadEvents.Data[0], "type") != "session.thread_status_terminated" {
		t.Fatalf("thread archive missing thread history: %s", threadEvents.Data)
	}
}

func TestSessionPendingToolRulesMatchAcceptance(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("pending-tool-rules"))
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"pending-tool-rules"}`)
	env := createEnvironment(t, app, `{"name":"pending-tool-rules"}`)
	const fields = `"public_event_id":"sevt_tool","request_id":"request","provider_tool_use_id":"tool"`
	for _, tc := range []struct {
		name, metadata string
		primary, all   bool
	}{
		{"unrelated key", `{"other":{` + fields + `}}`, false, false},
		{"wrong key suffix", `{"managed_agent_tool_permission_request:other":{` + fields + `}}`, false, false},
		{"empty key suffix", `{"managed_agent_tool_permission_request:":{` + fields + `}}`, false, false},
		{"cleared", `{"managed_agent_tool_permission_request:sevt_tool": null }`, false, false},
		{"empty request", `{"managed_agent_tool_permission_request:sevt_tool":{}}`, false, false},
		{"missing request id", `{"managed_agent_tool_permission_request:sevt_tool":{"public_event_id":"sevt_tool","provider_tool_use_id":"tool"}}`, false, false},
		{"missing tool id", `{"managed_agent_tool_permission_request:sevt_tool":{"public_event_id":"sevt_tool","request_id":"request"}}`, false, false},
		{"null patch removes keyed entry leaving legacy", `{"managed_agent_tool_permission_request":{` + fields + `},"managed_agent_tool_permission_request:sevt_tool":null}`, true, true},
		{"implicit primary", `{"managed_agent_tool_permission_request:sevt_tool":{` + fields + `}}`, true, true},
		{"empty primary", `{"managed_agent_tool_permission_request:sevt_tool":{` + fields + `,"session_thread_id":""}}`, true, true},
		{"null primary", `{"managed_agent_tool_permission_request:sevt_tool":{` + fields + `,"session_thread_id":null}}`, true, true},
		{"explicit primary", `{"managed_agent_tool_permission_request:sevt_tool":{` + fields + `,"session_thread_id":"PRIMARY"}}`, true, true},
		{"child", `{"managed_agent_tool_permission_request:sevt_tool":{` + fields + `,"session_thread_id":"sthr_child"}}`, false, true},
		{"legacy", `{"managed_agent_tool_permission_request":{` + fields + `}}`, true, true},
		{"duplicate legacy", `{"managed_agent_tool_permission_request":{` + fields + `},"managed_agent_tool_permission_request:sevt_tool":{` + fields + `}}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
			codeID := launchLocalCodeSession(t, app, session.ID)
			epoch := registerCodeSessionWorker(t, app, codeID)
			worker, found, err := app.db.GetCodeSession(t.Context(), codeID)
			if err != nil || !found {
				t.Fatalf("worker: %t %v", found, err)
			}
			primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
			if err != nil || !found {
				t.Fatalf("primary: %t %v", found, err)
			}
			putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
			metadata := strings.ReplaceAll(tc.metadata, "PRIMARY", primary.ExternalID)
			if _, err := app.db.UpdateCodeSessionWorkerState(t.Context(), worker.ExternalID, db.UpdateCodeSessionWorkerStateInput{
				WorkerEpoch: worker.CurrentWorkerEpoch, ExternalMetadataSet: true, ExternalMetadata: json.RawMessage(metadata),
			}); err != nil {
				t.Fatal(err)
			}
			putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
			if tc.all {
				putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"requires_action"}`)
				putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
			}
			for kind, pending := range map[string]bool{"session.status_idle": tc.all, "session.thread_status_idle": tc.primary} {
				events := listSessionEvents(t, app, worker.SessionExternalID, "types[]="+kind+"&order=desc", defaultTestKey)
				if len(events.Data) != 1 {
					t.Fatalf("missing %s", kind)
				}
				var payload struct {
					StopReason struct {
						Type     string   `json:"type"`
						EventIDs []string `json:"event_ids"`
					} `json:"stop_reason"`
				}
				if err := jsonv2.Unmarshal(events.Data[0], &payload); err != nil {
					t.Fatal(err)
				}
				if pending {
					if payload.StopReason.Type != "requires_action" || !slices.Equal(payload.StopReason.EventIDs, []string{"sevt_tool"}) {
						t.Fatalf("%s pending reason: %+v", kind, payload.StopReason)
					}
				} else if payload.StopReason.Type != "end_turn" || len(payload.StopReason.EventIDs) != 0 {
					t.Fatalf("%s unexpected pending reason: %+v", kind, payload.StopReason)
				}
			}
			sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"next"}]}]}`, defaultTestKey)
			queued := sessionInputProcessedAt(t, sent.Data[0]) == ""
			if queued != tc.primary {
				t.Fatalf("queued=%t, primary pending=%t", queued, tc.primary)
			}
			if queued {
				assertQueuedInputPreservesIdle(t, app, worker)
			}
		})
	}
}

func TestSessionIdleReasonChangesAreNotDeduplicated(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("idle-reason-changes"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	for i, metadata := range []string{
		`{"managed_agent_tool_permission_request:sevt_one":{"public_event_id":"sevt_one","request_id":"one","provider_tool_use_id":"one"}}`,
		`{"managed_agent_tool_permission_request:sevt_two":{"public_event_id":"sevt_two","request_id":"two","provider_tool_use_id":"two"}}`,
		`{"managed_agent_tool_permission_request:sevt_one":null,"managed_agent_tool_permission_request:sevt_two":null}`,
	} {
		if _, err := app.db.UpdateCodeSessionWorkerState(t.Context(), worker.ExternalID, db.UpdateCodeSessionWorkerStateInput{
			WorkerEpoch: worker.CurrentWorkerEpoch, ExternalMetadataSet: true, ExternalMetadata: json.RawMessage(metadata),
		}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
		}
		if i == 2 {
			postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_idle","uuid":"stale-tool-wait","stop_reason":{"type":"requires_action","event_ids":["sevt_one"]}}`))
		}
		for _, kind := range []string{"session.status_idle", "session.thread_status_idle"} {
			events := listSessionEvents(t, app, worker.SessionExternalID, "types[]="+kind, defaultTestKey)
			if len(events.Data) != i+1 {
				t.Fatalf("%s count=%d, want %d changed reasons without retries", kind, len(events.Data), i+1)
			}
		}
	}
}

func TestSessionStatusRespectsOtherThreads(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("status-other-threads"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"running"}`)
	childID := "sthr_child_" + worker.SessionExternalID
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_running","id":"sevt_child_running","uuid":"child-running","session_thread_id":`+quoteJSON(childID)+`}`))
	putCodeSessionWorkerState(t, app, worker.ExternalID, `{"worker_epoch":`+epoch+`,"worker_status":"idle"}`)
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("primary idle ended active child: %s", status)
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.thread_status_terminated","id":"sevt_child_terminated","uuid":"child-terminated","session_thread_id":`+quoteJSON(childID)+`}`))
	if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("child termination should leave idle primary available: %s", status)
	}
	for _, tc := range []struct{ event, status string }{{"rescheduled", "rescheduling"}, {"terminated", "terminated"}} {
		postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"session.status_`+tc.event+`","id":"sevt_session_`+tc.event+`","uuid":"session-`+tc.event+`"}`))
		primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
		if err != nil || !found || primary.Status != tc.status {
			t.Fatalf("primary status=%s, want %s: %v", primary.Status, tc.status, err)
		}
		if status := retrieveSession(t, app, worker.SessionExternalID, defaultTestKey).Status; status != tc.status {
			t.Fatalf("session status=%s, want %s", status, tc.status)
		}
	}
}

func TestSessionHistoryFiltersByProcessingTimeAndDefaultsToChronologicalOrder(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("history-creation-filter"))
	worker, _ := newPayloadIntegrationSession(t, app)
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"first"}]},{"type":"user.message","content":[{"type":"text","text":"queued"}]}]}`, defaultTestKey)
	if bytes.Contains(sent.Data[0], []byte(`"created_at"`)) {
		t.Fatalf("session event response exposed created_at: %s", sent.Data[0])
	}
	queuedID := sessionEventStringField(t, sent.Data[1], "id")
	stored, err := app.db.GetSessionEvent(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, queuedID)
	if err != nil {
		t.Fatalf("load queued event: %v", err)
	}
	if bytes.Contains(stored.Payload, []byte(`"created_at"`)) || stored.CreatedAt.IsZero() {
		t.Fatal("queued event should keep creation time only in its record")
	}
	// Claude's created_at query filters compare against processed_at, despite the parameter name.
	cutoff := time.Now().UTC().Add(time.Hour)
	if _, changed, err := app.db.AcknowledgeSessionInput(t.Context(), worker, queuedID, cutoff); err != nil || !changed {
		t.Fatalf("process queued input: changed=%t err=%v", changed, err)
	}
	events := listSessionEvents(t, app, worker.SessionExternalID, "created_at[gte]="+cutoff.Format(time.RFC3339Nano), defaultTestKey)
	if len(events.Data) != 1 || sessionEventStringField(t, events.Data[0], "id") != queuedID {
		t.Fatalf("processing filter excluded a later-processed input: %s", events.Data)
	}
	newer := listSessionEvents(t, app, worker.SessionExternalID, "created_at[gt]="+cutoff.Format(time.RFC3339Nano), defaultTestKey)
	if len(newer.Data) != 0 {
		t.Fatalf("exclusive processing filter included the boundary event: %s", newer.Data)
	}
	old := listSessionEvents(t, app, worker.SessionExternalID, "created_at[lt]="+cutoff.Format(time.RFC3339Nano), defaultTestKey)
	if len(old.Data) != 3 {
		t.Fatalf("processing filter included later-processed input: %s", old.Data)
	}
	older := listSessionEvents(t, app, worker.SessionExternalID, "created_at[lte]="+cutoff.Format(time.RFC3339Nano), defaultTestKey)
	if len(older.Data) != 4 || sessionEventStringField(t, older.Data[len(older.Data)-1], "id") != queuedID {
		t.Fatalf("inclusive processing filter excluded the boundary event: %s", older.Data)
	}
	all := listSessionEvents(t, app, worker.SessionExternalID, "", defaultTestKey)
	if len(all.Data) != 4 || sessionEventStringField(t, all.Data[len(all.Data)-1], "id") != queuedID {
		t.Fatalf("default history order is not processed_at ascending: %s", all.Data)
	}
}
