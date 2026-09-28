package tests

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

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

func TestSessionHistoryCreationFiltersAndDefaultOrder(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("history-creation-filter"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	sent := sendSessionEvents(t, app, worker.SessionExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":"first"}]},{"type":"user.message","content":[{"type":"text","text":"queued"}]}]}`, defaultTestKey)
	queuedID := sessionEventStringField(t, sent.Data[1], "id")
	// An old creation time and a current processing time must not satisfy this range.
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, `{"type":"system.message","id":"sevt_old_creation","uuid":"old-creation","created_at":"2000-01-01T00:00:00Z","message":"old"}`))
	events := listSessionEvents(t, app, worker.SessionExternalID, "created_at[gte]=2020-01-01T00:00:00Z", defaultTestKey)
	if len(events.Data) != 4 || sessionEventStringField(t, events.Data[0], "id") != queuedID {
		t.Fatalf("creation filter must include queued input first in default desc: %s", events.Data)
	}
	old := listSessionEvents(t, app, worker.SessionExternalID, "created_at[lt]=2020-01-01T00:00:00Z", defaultTestKey)
	if len(old.Data) != 1 || sessionEventStringField(t, old.Data[0], "id") != "sevt_old_creation" {
		t.Fatalf("creation filter used processing time: %s", old.Data)
	}
}
