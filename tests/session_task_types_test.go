package tests

import (
	"encoding/json"
	"fmt"
	"testing"

	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestSessionNonAgentTasksDoNotCreateThreads(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("task-types"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	for i, taskType := range []string{"local_bash", "local_workflow", "monitor_mcp", "dream", "unknown", ""} {
		for _, status := range []string{"completed", "failed", "stopped"} {
			id := fmt.Sprintf("task_%d_%s", i, status)
			postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
				fmt.Sprintf(`{"type":"system","uuid":"start_%s","subtype":"task_started","task_type":%q,"task_id":%q,"tool_use_id":%q,"description":"non-agent"}`, id, taskType, id, id),
				fmt.Sprintf(`{"type":"system","uuid":"end_%s","subtype":"task_notification","task_id":%q,"tool_use_id":%q,"status":%q}`, id, id, id, status)))
		}
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"system","uuid":"orphan","subtype":"task_notification","task_id":"orphan","status":"completed"}`,
		`{"type":"assistant","uuid":"bash-call","message":{"content":[{"type":"tool_use","id":"bash-tool","name":"Bash","input":{"command":"echo done"}}]}}`,
		`{"type":"user","uuid":"bash-result","message":{"content":[{"type":"tool_result","tool_use_id":"bash-tool","content":"bash output preserved"}]}}`))
	threads := listSessionThreads(t, app, worker.SessionExternalID, defaultTestKey)
	if len(threads.Data) != 1 {
		t.Fatalf("non-agent tasks created threads: %+v", threads.Data)
	}
	events := listSessionEvents(t, app, worker.SessionExternalID, "", defaultTestKey)
	if eventPageContains(events, `"type":"session.thread_created"`) || !eventPageContains(events, "bash output preserved") {
		t.Fatalf("unexpected task events or missing bash result: %+v", events.Data)
	}
}

func TestSessionLegacyBashNotificationDoesNotUpdateThread(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("legacy-bash-task"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	service := newCodeSessionService(app, nil, nil)
	sink := sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil)
	if err := sink.PublishCodeSessionEvents(t.Context(), worker, []json.RawMessage{
		json.RawMessage(`{"type":"session.thread_created","id":"legacy_bash","session_thread_id":"sthr_legacy_bash","subtype":"task_started","task_type":"local_bash","task_id":"legacy_bash","tool_use_id":"legacy_tool"}`),
	}); err != nil {
		t.Fatal(err)
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"system","uuid":"legacy_end","subtype":"task_notification","task_id":"legacy_bash","tool_use_id":"legacy_tool","status":"failed"}`))
	threads := listSessionThreads(t, app, worker.SessionExternalID, defaultTestKey)
	if len(threads.Data) != 2 {
		t.Fatalf("notification created another thread: %+v", threads.Data)
	}
	events := listSessionEvents(t, app, worker.SessionExternalID, "types[]=session.thread_status_terminated", defaultTestKey)
	if len(events.Data) != 0 {
		t.Fatalf("legacy bash notification updated thread: %+v", events.Data)
	}
}

func TestSessionAgentTaskTypesCompleteRegisteredThreads(t *testing.T) {
	for _, taskType := range []string{"local_agent", "remote_agent", "in_process_teammate"} {
		t.Run(taskType, func(t *testing.T) {
			app := newPayloadIntegrationApp(t, newFakeStore("agent-task-types"))
			worker, epoch := newPayloadIntegrationSession(t, app)
			for _, status := range []string{"failed", "stopped", "completed"} {
				toolID := "tool_" + status
				notification := fmt.Sprintf(`{"type":"system","uuid":"end_%s","subtype":"task_notification","task_id":%q,"status":%q}`, status, status, status)
				postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
					fmt.Sprintf(`{"type":"system","uuid":"start_%s","subtype":"task_started","task_type":%q,"task_id":%q,"tool_use_id":%q,"description":"child"}`, status, taskType, status, toolID),
					fmt.Sprintf(`{"type":"assistant","uuid":"message_%s","parent_tool_use_id":%q,"message":{"content":[{"type":"text","text":"child output"}]}}`, status, toolID),
					notification))
				postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch, notification))
				threadID := maevents.ClaudeTaskThreadID(worker.ExternalID, toolID)
				thread, err := app.db.GetSessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID, threadID)
				want := "terminated"
				if status == "completed" {
					want = "idle"
				}
				if err != nil || thread.Status != want {
					t.Fatalf("child status=%s, want=%s, err=%v", thread.Status, want, err)
				}
				if events := listThreadEvents(t, app, worker.SessionExternalID, threadID, defaultTestKey); !eventPageContains(events, "child output") {
					t.Fatalf("missing child output: %+v", events.Data)
				}
			}
			if threads := listSessionThreads(t, app, worker.SessionExternalID, defaultTestKey); len(threads.Data) != 4 {
				t.Fatalf("replay created extra threads: %+v", threads.Data)
			}
		})
	}
}
