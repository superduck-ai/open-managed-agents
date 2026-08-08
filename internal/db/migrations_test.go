package db

import (
	"io/fs"
	"strings"
	"testing"
)

func TestScheduledDeploymentsMigrationDefinesOccurrenceInvariant(t *testing.T) {
	migration, err := fs.ReadFile(embeddedMigrations, "migrations/00050_schedule_deployments_with_river.sql")
	if err != nil {
		t.Fatalf("read scheduled deployments migration: %v", err)
	}
	text := string(migration)
	for _, fragment := range []string{
		"add column schedule_revision bigint not null default 0",
		"add column next_scheduled_at timestamptz",
		"set scheduled_at = (trigger_context ->> 'scheduled_at')::timestamptz",
		"deployment_runs_scheduled_at_check",
		"deployment_runs_schedule_occurrence_idx",
		"where trigger_type = 'schedule'",
		"drop column trigger_context",
		"jsonb_build_object('type', 'schedule', 'scheduled_at', scheduled_at)",
	} {
		if !strings.Contains(text, fragment) {
			t.Errorf("migration does not contain %q", fragment)
		}
	}
}

func TestSnapshotSessionSkillsMigrationSkipsExistingFiles(t *testing.T) {
	migration, err := fs.ReadFile(embeddedMigrations, "migrations/00048_snapshot_session_skills.sql")
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

func TestSessionResourceMigrationsPreserveMainHistory(t *testing.T) {
	expected := []string{
		"00036_use_uuid_workspace_organization_reference.sql",
		"00037_drop_organization_external_id.sql",
		"00038_use_uuid_external_key_organization_reference.sql",
		"00039_use_uuid_admin_resource_references.sql",
		"00040_use_uuid_external_key_pagination.sql",
		"00041_use_uuid_resource_references.sql",
		"00042_use_uuid_orchestration_references.sql",
		"00043_use_uuid_runtime_references.sql",
		"00044_use_uuid_compatibility_references.sql",
		"00045_use_uuid_tunnel_references.sql",
		"00046_name_compatibility_workspace_display_ids.sql",
		"00047_unify_session_resources_and_files.sql",
		"00048_snapshot_session_skills.sql",
	}

	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	actual := make([]string, 0, len(expected))
	for _, entry := range entries {
		name := entry.Name()
		if len(name) >= 5 && name[:5] >= "00036" && name[:5] <= "00048" {
			actual = append(actual, name)
		}
	}
	if strings.Join(actual, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("migration history 00036-00048 = %v, want %v", actual, expected)
	}
}
