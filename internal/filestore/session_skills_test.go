package filestore

import (
	"context"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"testing"
)

func TestReadSessionSkillRejectsTraversalBeforeDatabaseAccess(t *testing.T) {
	service := &Service{}
	for _, directory := range []string{"", ".", "..", "../secret", "a/b", "a\\b", "a\x00b"} {
		if _, err := service.ReadSessionSkill(t.Context(), "workspace", "session", directory); err == nil {
			t.Fatalf("accepted %q", directory)
		}
	}
}

func TestReadSessionSkillUsesArchiveSnapshot(t *testing.T) {
	data := buildSkillArchiveTestZip(t, map[string]string{"demo/SKILL.md": "# Demo"})
	service, _ := skillArchiveTestService(data, skillArchiveTestEntry(data))
	database := service.db.(*fakeServiceDatabase)
	database.getSessionFilesystemFn = func(_ context.Context, workspaceUUID, sessionID string) (db.FilestoreFilesystem, error) {
		if workspaceUUID != serviceTestPrincipal().WorkspaceUUID || sessionID != "session" {
			return db.FilestoreFilesystem{}, db.ErrNotFound
		}
		return serviceTestFilesystem(), nil
	}
	if _, err := service.ReadSessionSkill(t.Context(), "other", "session", "demo"); err == nil {
		t.Fatal("cross-workspace read accepted")
	}
	result, err := service.ReadSessionSkill(t.Context(), serviceTestPrincipal().WorkspaceUUID, "session", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != "# Demo" {
		t.Fatalf("document = %q", result)
	}
}
