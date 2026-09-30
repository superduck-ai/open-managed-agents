package managedagentsevents

import (
	"encoding/json"
	"testing"
)

func TestPendingToolEventIDsRejectsMalformedMetadata(t *testing.T) {
	for _, raw := range []string{
		`{`, `[]`,
		`{"managed_agent_tool_permission_request:tool":true}`,
		`{"managed_agent_tool_permission_request:tool":{"public_event_id":123}}`,
	} {
		if _, err := PendingToolEventIDs([]byte(raw), "primary", "primary"); err == nil {
			t.Fatalf("accepted malformed metadata: %s", raw)
		}
	}
}

func TestClearPendingToolRequestsRejectsMalformedMetadata(t *testing.T) {
	if _, err := ClearPendingToolRequests([]byte(`{"managed_agent_tool_permission_request:tool":true}`), "primary", "primary"); err == nil {
		t.Fatal("accepted malformed tool request")
	}
}

func TestClearPendingToolRequestsPreservesOtherThreads(t *testing.T) {
	raw := []byte(`{"task_summary":"keep","managed_agent_tool_permission_request":{"public_event_id":"main","request_id":"r1","provider_tool_use_id":"t1"},"managed_agent_tool_permission_request:main":{"public_event_id":"main","request_id":"r1","provider_tool_use_id":"t1"},"managed_agent_tool_permission_request:child":{"public_event_id":"child","request_id":"r2","provider_tool_use_id":"t2","session_thread_id":"child-thread"}}`)
	cleared, err := ClearPendingToolRequests(raw, "primary", "primary")
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]json.RawMessage
	if err := json.Unmarshal(cleared, &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 2 || string(metadata["task_summary"]) != `"keep"` {
		t.Fatalf("metadata lost unrelated keys: %s", cleared)
	}
	ids, err := PendingToolEventIDs(cleared, "primary", "")
	if err != nil || len(ids) != 1 || ids[0] != "child" {
		t.Fatalf("pending child request = %v, error=%v", ids, err)
	}
}
