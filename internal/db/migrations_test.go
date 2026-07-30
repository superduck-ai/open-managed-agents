package db

import (
	"io/fs"
	"strings"
	"testing"
)

func TestSnapshotSessionSkillsMigrationSkipsExistingFiles(t *testing.T) {
	migration, err := fs.ReadFile(embeddedMigrations, "migrations/00037_snapshot_session_skills.sql")
	if err != nil {
		t.Fatalf("read Session Skill snapshot migration: %v", err)
	}

	guard := `and not exists (
		select 1
		from files existing
		where existing.uuid = resource.file_uuid
	)`
	if count := strings.Count(strings.ReplaceAll(string(migration), "\r\n", "\n"), guard); count != 2 {
		t.Fatalf("existing File guard count = %d, want one per Skill source", count)
	}
}
