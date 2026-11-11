//go:build integration

package db

import (
	"os"
	"testing"
)

func TestEvaluationTaskMigrationKeepsDatasetsAndRubrics(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 TEST_MIGRATION_DATABASE_URL 运行迁移验收；跳过不能视为通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 120); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO evaluation_datasets (external_id, organization_uuid, workspace_uuid, name)
VALUES ('evalds_keep', gen_random_uuid(), gen_random_uuid(), '保留的评测集');
INSERT INTO evaluation_cases (external_id, organization_uuid, workspace_uuid, dataset_uuid, version_uuid, position, query)
SELECT 'evalcase_keep', organization_uuid, workspace_uuid, uuid, gen_random_uuid(), 1, '汇总销售额' FROM evaluation_datasets;
INSERT INTO evaluation_rubrics (external_id, organization_uuid, workspace_uuid, name)
VALUES ('evalrb_keep', gen_random_uuid(), gen_random_uuid(), '保留的评分标准');`); err != nil {
		t.Fatal(err)
	}
	count := func(query string) int {
		t.Helper()
		var value int
		if err := database.QueryRowContext(ctx, query).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	kept := count(`SELECT (SELECT count(*) FROM evaluation_datasets) + (SELECT count(*) FROM evaluation_cases) + (SELECT count(*) FROM evaluation_rubrics)`)
	if kept != 3 {
		t.Fatalf("kept rows = %d", kept)
	}
	if indexes := count(`SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname = 'evaluation_cases_version_position_list_idx'`); indexes != 0 {
		t.Fatalf("duplicate case index count = %d", indexes)
	}
	if constraints := count(`SELECT count(*) FROM pg_constraint WHERE conrelid = 'evaluation_cases'::regclass AND conname = 'evaluation_cases_has_input'`); constraints != 1 {
		t.Fatalf("case input constraint count = %d", constraints)
	}
	if _, err := provider.DownTo(ctx, 116); err != nil {
		t.Fatal(err)
	}
	if tables := count(`SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name LIKE 'evaluation_%'`); tables != 0 {
		t.Fatalf("evaluation tables after rollback = %d", tables)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
}
