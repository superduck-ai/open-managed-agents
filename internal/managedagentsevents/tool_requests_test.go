package managedagentsevents

import "testing"

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
