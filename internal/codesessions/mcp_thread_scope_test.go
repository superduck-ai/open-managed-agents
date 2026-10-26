package codesessions

import (
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"net/http"
	"testing"
	"time"
)

func TestMCPThreadPinnedScopeAndIsolation(t *testing.T) {
	parent := "root"
	record := db.CodeSession{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "ses"}
	thread := db.SessionThread{OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "ses", ParentThreadExternalID: &parent, Status: "idle", AgentSnapshot: json.RawMessage(`{"tools":[],"mcp_servers":[{"name":"peer","type":"url","url":"https://expert.example/mcp"}]}`)}
	raw, err := validatedMCPThreadSnapshot(record, thread)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := networkpolicy.ParseMCPProxyPolicy(json.RawMessage(`{"type":"cloud","networking":{"type":"unrestricted"}}`), raw)
	if err != nil {
		t.Fatal(err)
	}
	if target, ok := policy.MCPServerURL("peer"); !ok || target != "https://expert.example/mcp" {
		t.Fatal("wrong pinned target", target)
	}
	if policy.AuthorizeMCPURL("https://parent.example/mcp").Allow {
		t.Fatal("parent MCP URL leaked into child policy")
	}
	for name, change := range map[string]func(*db.SessionThread){
		"organization": func(s *db.SessionThread) { s.OrganizationUUID = "foreign" },
		"workspace":    func(s *db.SessionThread) { s.WorkspaceUUID = "foreign" },
		"session":      func(s *db.SessionThread) { s.SessionExternalID = "foreign" },
		"root":         func(s *db.SessionThread) { s.ParentThreadExternalID = nil },
		"terminated":   func(s *db.SessionThread) { s.Status = "terminated" },
		"archived":     func(s *db.SessionThread) { now := time.Now(); s.ArchivedAt = &now },
		"deleted":      func(s *db.SessionThread) { now := time.Now(); s.DeletedAt = &now },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := thread
			change(&candidate)
			if _, err := validatedMCPThreadSnapshot(record, candidate); !errors.Is(err, db.ErrNotFound) {
				t.Fatalf("accepted inaccessible scope: %v", err)
			}
		})
	}
	for _, raw := range []string{"null", "[]", "{"} {
		candidate := thread
		candidate.AgentSnapshot = json.RawMessage(raw)
		if _, err := validatedMCPThreadSnapshot(record, candidate); err == nil {
			t.Fatal("accepted invalid snapshot", raw)
		}
	}
	headers := make(http.Header)
	headers.Set(runtimeThreadHeader, "private")
	headers.Set("Mcp-Session-Id", "upstream")
	clean := sanitizedMCPProxyHeaders(headers)
	if clean.Get(runtimeThreadHeader) != "" || clean.Get("Mcp-Session-Id") != "upstream" || headers.Get(runtimeThreadHeader) != "private" {
		t.Fatal("thread routing header leaked upstream or source changed")
	}
}
