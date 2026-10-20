//go:build integration || e2e || e2b_integration

package tests

import (
	"testing"
)

func TestSessionThreadMessagesHaveSurfaceRelativeDirections(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("thread-message-directions"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	child := "sthr_message_child_" + worker.SessionExternalID
	input := internalPayloadRequest(epoch,
		`{"type":"session.thread_created","uuid":"direction-child","session_thread_id":`+quoteJSON(child)+`,"agent_name":"worker"}`,
		`{"type":"agent.thread_message_sent","uuid":"direction-task","id":"sevt_direction_task","to_session_thread_id":`+quoteJSON(child)+`,"to_agent_name":"worker","content":[{"type":"text","text":"return literal"}]}`,
		`{"type":"agent.thread_message_received","uuid":"direction-report","id":"sevt_direction_report","from_session_thread_id":`+quoteJSON(child)+`,"from_agent_name":"worker","content":[{"type":"text","text":"literal"}]}`)
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, input)
	primary, found, err := app.db.GetPrimarySessionThread(t.Context(), worker.WorkspaceUUID, worker.SessionExternalID)
	if err != nil || !found {
		t.Fatalf("primary thread: %v", err)
	}
	events := listThreadEvents(t, app, worker.SessionExternalID, child, defaultTestKey)
	if len(events.Data) != 2 {
		t.Fatalf("child needs task and report, got %s", events.Data)
	}
	received := sessionEventObjectByType(t, events, "agent.thread_message_received")
	sent := sessionEventObjectByType(t, events, "agent.thread_message_sent")
	if received["from_session_thread_id"] != primary.ExternalID || received["original_event_id"] != "sevt_direction_task" || sent["to_session_thread_id"] != primary.ExternalID || sent["original_event_id"] != "sevt_direction_report" {
		t.Fatalf("wrong directions received=%v sent=%v", received, sent)
	}
	main := listSessionEvents(t, app, worker.SessionExternalID, "order=asc", defaultTestKey)
	task := sessionEventObjectByType(t, main, "agent.thread_message_sent")
	report := sessionEventObjectByType(t, main, "agent.thread_message_received")
	if task["to_session_thread_id"] != child || report["from_session_thread_id"] != child {
		t.Fatalf("parent direction changed: %v %v", task, report)
	}
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, input)
	replay := listThreadEvents(t, app, worker.SessionExternalID, child, defaultTestKey)
	if len(replay.Data) != 2 {
		t.Fatalf("replay duplicated paired messages: %s", replay.Data)
	}
}

func TestSessionOwnedThreadMessageDoesNotMirrorBack(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("owned-thread-message"))
	worker, epoch := newPayloadIntegrationSession(t, app)
	child := "sthr_owned_message_" + worker.SessionExternalID
	postCodeSessionWorkerEvents(t, app, worker.ExternalID, internalPayloadRequest(epoch,
		`{"type":"session.thread_created","uuid":"owned-direction-child","session_thread_id":`+quoteJSON(child)+`,"agent_name":"worker"}`,
		`{"type":"agent.thread_message_received","uuid":"owned-direction-task","owner_session_thread_id":`+quoteJSON(child)+`,"content":[{"type":"text","text":"owned task"}]}`))
	primary := listSessionEvents(t, app, worker.SessionExternalID, "types[]=agent.thread_message_received", defaultTestKey)
	if len(primary.Data) != 0 {
		t.Fatalf("explicit child message leaked to primary: %s", primary.Data)
	}
	events := listThreadEvents(t, app, worker.SessionExternalID, child, defaultTestKey)
	if len(events.Data) != 1 {
		t.Fatalf("explicit ownership duplicated: %s", events.Data)
	}
}
