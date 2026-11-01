//go:build integration

package db

import (
	"os"
	"testing"
)

func TestSimplifySystemSkillsMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 TEST_MIGRATION_DATABASE_URL 运行迁移验收；跳过不能视为通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 114); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO builtin_skills (external_id, display_title, latest_version, publisher, source_url, license, revision)
VALUES ('migration-skill', 'Migration skill', '2', 'OMA', 'source', 'license', 5);
INSERT INTO builtin_skill_versions (external_id, skill_id, skill_uuid, skill_external_id, version, name, directory, s3_bucket, s3_key, size_bytes, sha256, notes)
SELECT 'skver_legacy', id, uuid, external_id, '1', 'migration-skill', 'migration-skill', 'bucket', 'archive-1', 100, 'hash-1', 'notes' FROM builtin_skills;
INSERT INTO builtin_skill_versions (external_id, skill_uuid, skill_external_id, version, name, directory, s3_bucket, s3_key, size_bytes, sha256)
SELECT 'skver_current', uuid, external_id, '2', 'migration-skill', 'migration-skill', 'bucket', 'archive-2', 100, 'hash-2' FROM builtin_skills;
INSERT INTO system_skill_events (skill_uuid, actor_uuid, action, version)
SELECT uuid, gen_random_uuid(), 'publish', '2' FROM builtin_skills;
`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := database.QueryRowContext(ctx, `SELECT jsonb_build_object(
'skills', (SELECT jsonb_agg(to_jsonb(s) - 'publisher' - 'source_url' - 'license') FROM builtin_skills s),
'versions', (SELECT jsonb_agg(to_jsonb(v) - 'notes' - 'skill_id' ORDER BY version) FROM builtin_skill_versions v),
'events', (SELECT jsonb_agg(to_jsonb(e)) FROM system_skill_events e))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	for range 2 {
		if _, err := provider.UpTo(ctx, 115); err != nil {
			t.Fatal(err)
		}
		var removedColumns int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
WHERE table_schema = current_schema() AND
((table_name = 'builtin_skills' AND column_name IN ('publisher', 'source_url', 'license')) OR
(table_name = 'builtin_skill_versions' AND column_name IN ('notes', 'skill_id')))`).Scan(&removedColumns); err != nil {
			t.Fatal(err)
		}
		if removedColumns != 0 || snapshot() != before {
			t.Fatal("清理迁移未移除冗余字段，或改变了技能、版本、包引用与操作记录")
		}
		if _, err := provider.DownTo(ctx, 114); err != nil {
			t.Fatal(err)
		}
		var missingIDs int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM builtin_skill_versions WHERE skill_id IS NULL`).Scan(&missingIDs); err != nil || missingIDs != 0 {
			t.Fatalf("旧关联未恢复：missing=%d error=%v", missingIDs, err)
		}
	}
}
