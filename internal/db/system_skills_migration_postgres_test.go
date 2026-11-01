//go:build integration

package db

import (
	"os"
	"testing"
)

func TestSystemSkillsMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 TEST_MIGRATION_DATABASE_URL 运行迁移验收；跳过不能视为通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 113); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO builtin_skill_versions (external_id, skill_id, skill_external_id, version, name, directory, s3_bucket, s3_key, size_bytes, sha256)
VALUES ('orphan', 999999, 'orphan', '1', 'orphan', 'orphan', 'bucket', 'archive', 1, 'hash');`); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 114); err == nil {
		t.Fatal("孤立引用必须阻止迁移")
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM builtin_skill_versions WHERE external_id = 'orphan';
INSERT INTO builtin_skills (external_id, display_title, latest_version) VALUES ('migration-skill', 'Migration skill', '1');
INSERT INTO builtin_skill_versions (id, external_id, skill_id, skill_external_id, version, name, directory, s3_bucket, s3_key, size_bytes, sha256)
OVERRIDING SYSTEM VALUE SELECT 100, 'skver_legacy', id, external_id, '1', 'migration-skill', 'migration-skill', 'bucket', 'archive-1', 100, 'hash-1' FROM builtin_skills;`); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		if err := database.QueryRowContext(ctx, `SELECT jsonb_build_object(
'skills', (SELECT jsonb_agg(to_jsonb(s) - 'listed' - 'revision') FROM builtin_skills s),
'versions', (SELECT jsonb_agg(to_jsonb(v) - 'state' - 'skill_id' - 'skill_uuid' ORDER BY version) FROM builtin_skill_versions v))::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := snapshot()
	for range 2 {
		if _, err := provider.UpTo(ctx, 114); err != nil {
			t.Fatal(err)
		}
		var columns string
		if err := database.QueryRowContext(ctx, `SELECT string_agg(column_name, ',' ORDER BY ordinal_position) FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'builtin_skill_versions'`).Scan(&columns); err != nil {
			t.Fatal(err)
		}
		if columns != "id,uuid,external_id,skill_uuid,skill_external_id,version,state,name,description,directory,s3_bucket,s3_key,size_bytes,sha256,created_at,deleted_at" || snapshot() != before {
			t.Fatal("最终表结构不正确或迁移改变了版本数据")
		}
		var references int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM builtin_skill_versions v JOIN builtin_skills s ON v.skill_uuid = s.uuid WHERE v.state = 'published'`).Scan(&references); err != nil || references != 1 {
			t.Fatalf("UUID 回填：%d %v", references, err)
		}
		var nextID int64
		if err := database.QueryRowContext(ctx, `SELECT nextval(pg_get_serial_sequence('builtin_skill_versions', 'id'))`).Scan(&nextID); err != nil || nextID <= 100 {
			t.Fatalf("identity 未续接：%d %v", nextID, err)
		}
		if _, err := provider.DownTo(ctx, 113); err != nil {
			t.Fatal(err)
		}
		if snapshot() != before {
			t.Fatal("回滚改变了原始数据")
		}
		var missing int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM builtin_skill_versions v LEFT JOIN builtin_skills s ON v.skill_id = s.id WHERE s.id IS NULL`).Scan(&missing); err != nil || missing != 0 {
			t.Fatalf("旧关联未恢复：%d %v", missing, err)
		}
	}
}
