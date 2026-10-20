package db

import (
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	"testing"
)

func TestRegisteredExternalToolResultDoesNotWaitForWorkerStatusReport(t *testing.T) {
	primary := SessionThread{ExternalID: "primary", Status: "idle"}
	worker := codeSessionInputStateRow{ExternalID: "cse_test", Status: "active", WorkerStatus: "running", WorkerExternalMetadata: []byte(`{"managed_agent_tool_permission_request:public":{"public_event_id":"public","request_id":"request","provider_tool_use_id":"provider","session_thread_id":"primary"}}`)}
	reference := "public"
	owner := "primary"
	event := SessionEvent{EventType: "user.tool_result", ToolUseID: &reference, ThreadExternalID: &owner}
	if accepted, err := sessionToolResultInputAccepted(primary, worker, event); err != nil || !accepted {
		t.Fatal("valid registered external result raced worker status", accepted, err)
	}
	for _, name := range []string{"foreign tool", "foreign thread", "no reference", "no pending", "terminated", "retired worker"} {
		t.Run(name, func(t *testing.T) {
			p, w, e := primary, worker, event
			switch name {
			case "foreign tool":
				v := "foreign"
				e.ToolUseID = &v
			case "foreign thread":
				v := "foreign"
				e.ThreadExternalID = &v
			case "no reference":
				e.ToolUseID = nil
			case "no pending":
				w.WorkerExternalMetadata = []byte(`{}`)
			case "terminated":
				p.Status = "terminated"
			case "retired worker":
				w.Status = "terminated"
			}
			if accepted, err := sessionToolResultInputAccepted(p, w, e); err != nil || accepted {
				t.Fatal("unregistered/foreign result accepted", accepted, err)
			}
		})
	}
	id, err := maevents.ResolvePendingToolEventID(worker.WorkerExternalMetadata, worker.ExternalID, primary.ExternalID, owner, reference)
	if err != nil || id != reference {
		t.Fatal("fixture must be a real valid pending registry", err)
	}
}
