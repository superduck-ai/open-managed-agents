package codesessions

import (
	"encoding/json"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestBrainPersistenceLogProjectsNoPublicPayloads(t *testing.T) {
	agentID := "sthr_child"
	event := db.CodeSessionInternalEvent{
		SequenceNum: 1,
		AgentID:     &agentID,
		Payload:     json.RawMessage(`{"type":"dsh_log","uuid":"dsh_log:cse_1:sthr_child:1","data":{"type":"assistant/message","data":{"text":"hi"}}}`),
	}
	payloads, err := publicPayloadsFromInternalSubagentEvent("cse_1", event, agentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 0 {
		t.Fatalf("dsh_log projection = %d payloads, want none", len(payloads))
	}
}

func TestBrainClaimSessionsSkipSubagentInternalProjection(t *testing.T) {
	service := newTestService(t, nil)
	sink := &brainModelEventSink{}
	service.SetPublicEventSink(sink)
	codeSession := db.CodeSession{ExternalID: "cse_brain", Metadata: json.RawMessage(`{"config":{"worker_transport":"claim"}}`)}
	if err := service.PublishSubagentInternalEvents(t.Context(), codeSession); err != nil {
		t.Fatal(err)
	}
	if sink.batches != 0 {
		t.Fatalf("published %d batches for a brain session, want 0", sink.batches)
	}
}
