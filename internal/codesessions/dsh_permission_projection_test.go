package codesessions

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

func TestDSRPermissionProjectionOnlyForBlockingChildTools(t *testing.T) {
	for _, tc := range []struct {
		name       string
		permission resolvedToolPermission
		identity   toolIdentity
		waiting    bool
		count      int
	}{
		{"allow builtin", resolvedToolPermissionAllow, toolIdentity{Kind: "agent_toolset", ToolName: "Bash"}, false, 1},
		{"deny builtin", resolvedToolPermissionDeny, toolIdentity{Kind: "agent_toolset", ToolName: "Read"}, false, 1},
		{"allow MCP", resolvedToolPermissionAllow, toolIdentity{Kind: "mcp", ServerName: "peer", ToolName: "read"}, false, 1},
		{"deny MCP", resolvedToolPermissionDeny, toolIdentity{Kind: "mcp", ServerName: "peer", ToolName: "read"}, false, 1},
		{"ask builtin", resolvedToolPermissionAsk, toolIdentity{Kind: "agent_toolset", ToolName: "Bash"}, false, 2},
		{"ask MCP", resolvedToolPermissionAsk, toolIdentity{Kind: "mcp", ServerName: "peer", ToolName: "read"}, false, 2},
		{"custom", resolvedToolPermissionAllow, toolIdentity{Kind: "agent_toolset", ToolName: "ask_user_question"}, true, 2},
		{"self hosted waiting", resolvedToolPermissionAllow, toolIdentity{Kind: "agent_toolset", ToolName: "Bash"}, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, raw, err := dshToolCallPublicPayload("code", toolPermissionRequest{ToolUseID: "native", ToolName: tc.identity.ToolName, SessionThreadID: "child"}, json.RawMessage(`{"value":9007199254740993}`), tc.identity, tc.permission, time.Now().UTC(), tc.waiting)
			if err != nil {
				t.Fatal(err)
			}
			events, err := dshPermissionEvents(db.DSHScope{CodeSessionID: "code"}, request, raw)
			if err != nil || len(events) != tc.count {
				t.Fatal("projection", len(events), err)
			}
			original := maevents.ToolUseEventID("code", "native", "child")
			if events[0].ExternalID != original || events[0].ThreadExternalID == nil || *events[0].ThreadExternalID != "child" {
				t.Fatal("child original changed")
			}
			var source struct {
				Primary  string `json:"primary_event_id"`
				Original string `json:"original_event_id"`
			}
			if json.Unmarshal(events[0].Payload, &source) != nil || source.Original != "" {
				t.Fatal("original metadata", source)
			}
			if tc.count == 1 && source.Primary != "" || tc.count == 2 && source.Primary != request.PublicEventID {
				t.Fatal("original reverse mirror link", source.Primary, request.PublicEventID)
			}
			if tc.count == 1 {
				if request.PublicEventID != original {
					t.Fatal("permission refers to absent root alias")
				}
			} else {
				var mirror struct {
					ID       string `json:"id"`
					Original string `json:"original_event_id"`
					Thread   string `json:"session_thread_id"`
				}
				if json.Unmarshal(events[1].Payload, &mirror) != nil || mirror.ID != request.PublicEventID || mirror.Original != original || mirror.Thread != "child" || events[1].ThreadExternalID != nil {
					t.Fatal("blocking mirror identity", mirror)
				}
			}
		})
	}
}
