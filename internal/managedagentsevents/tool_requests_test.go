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

func TestResolvePendingToolEventID(t *testing.T) {
	const childEventID = "sevt_e6def445dfa02dbc80525f1e5d2f5b6d"
	const request = `{"public_event_id":"mirror","request_id":"request","provider_tool_use_id":"provider-child","session_thread_id":"child"}`
	for _, test := range []struct {
		name, metadata, threadID, eventID, want string
	}{
		{"unknown ID", `{"managed_agent_tool_permission_request:mirror":` + request + `}`, "child", "unknown", ""},
		{"wrong thread", `{"managed_agent_tool_permission_request:mirror":` + request + `}`, "other", childEventID, ""},
		{"implicit primary", `{"managed_agent_tool_permission_request:mirror":` + request + `}`, "primary", childEventID, ""},
		{"completed legacy request", `{"managed_agent_tool_permission_request":` + request + `,"managed_agent_tool_permission_request:mirror":null}`, "child", childEventID, ""},
		{"wrong metadata key", `{"managed_agent_tool_permission_request:wrong":` + request + `}`, "child", childEventID, ""},
		{"mirror", `{"managed_agent_tool_permission_request:mirror":` + request + `}`, "child", "mirror", "mirror"},
		{"child history", `{"managed_agent_tool_permission_request:mirror":` + request + `}`, "child", childEventID, "mirror"},
		{"legacy child history", `{"managed_agent_tool_permission_request":` + request + `}`, "child", childEventID, "mirror"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolvePendingToolEventID([]byte(test.metadata), "cse_test", "primary", test.threadID, test.eventID)
			if err != nil || got != test.want {
				t.Fatalf("resolved event ID=%q want=%q error=%v", got, test.want, err)
			}
		})
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
