package codesessions

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
	"time"
)

func TestWorkerSubagentStateIsSessionScopedAndRetainsArchive(t *testing.T) {
	parent := "primary"
	record := db.CodeSession{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionExternalID: "session"}
	thread := db.SessionThread{ExternalID: "child", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionExternalID: "session", ParentThreadExternalID: &parent, Status: "idle"}
	state, err := scopedSubagentThreadState(record, thread)
	if err != nil || state.ID != "child" || state.Status != "idle" || state.Archived {
		t.Fatal(state, err)
	}
	now := time.Now()
	thread.ArchivedAt = &now
	thread.Status = "terminated"
	state, err = scopedSubagentThreadState(record, thread)
	if err != nil || !state.Archived || state.Status != "terminated" {
		t.Fatal(state, err)
	}
	for _, bad := range []string{"workspace", "session", "org", "primary", "deleted"} {
		copy := thread
		switch bad {
		case "workspace":
			copy.WorkspaceUUID = "foreign"
		case "session":
			copy.SessionExternalID = "foreign"
		case "org":
			copy.OrganizationUUID = "foreign"
		case "primary":
			copy.ParentThreadExternalID = nil
		case "deleted":
			copy.DeletedAt = &now
		}
		if _, err := scopedSubagentThreadState(record, copy); !errors.Is(err, db.ErrNotFound) {
			t.Fatal("foreign/deleted thread disclosed", bad, err)
		}
	}
}
