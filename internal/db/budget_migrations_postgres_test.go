//go:build integration

package db

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
)

func TestBudgetMigrationsUpgradeFromMainPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("未设置 TEST_MIGRATION_DATABASE_URL")
	}
	for _, baselineVersion := range []int64{95, 99} {
		t.Run(fmt.Sprint(baselineVersion), func(t *testing.T) {
			ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
			baseline := fstest.MapFS{}
			entries, err := fs.ReadDir(embeddedMigrations, "migrations")
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				_, name, _ := strings.Cut(entry.Name(), "_")
				if name == "remove_model_price_catalog.sql" || name == "provider_cost.sql" {
					continue
				}
				if baselineVersion == 95 {
					switch name {
					case "session_budget.sql", "usage_records.sql", "llm_model_prices.sql", "model_price_catalog.sql":
						continue
					}
				}
				data, err := fs.ReadFile(embeddedMigrations, "migrations/"+entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				baseline[entry.Name()] = &fstest.MapFile{Data: data}
			}
			provider, err := goose.NewProvider(goose.DialectPostgres, database, baseline, goose.WithDisableGlobalRegistry(true), goose.WithLogger(goose.NopLogger()))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Up(ctx); err != nil {
				t.Fatalf("初始化 main 数据库：%v", err)
			}
			var version, historyCount int64
			if err := database.QueryRowContext(ctx, `SELECT max(version_id), count(*) FROM goose_db_version WHERE is_applied`).Scan(&version, &historyCount); err != nil || version != baselineVersion {
				t.Fatalf("main 迁移版本=%d，预期 %d：%v", version, baselineVersion, err)
			}
			expectedBudgetColumns := 0
			if baselineVersion == 99 {
				expectedBudgetColumns = 1
			}
			var budgetColumns int
			if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='sessions' AND column_name='budget'`).Scan(&budgetColumns); err != nil || budgetColumns != expectedBudgetColumns {
				t.Fatalf("main 基线预算字段不符：%d/%v", budgetColumns, err)
			}
			assertMigrationColumnNullable(t, ctx, database, "llm_providers", "models", "NO")
			assertMigrationColumnNullable(t, ctx, database, "llm_providers", "workspace_uuid", "YES")
			seedEnvironmentWorkMigrationSessions(t, ctx, database)
			for range 2 {
				if err := runGooseMigrations(ctx, database); err != nil {
					t.Fatalf("从 main 升级或重复启动失败：%v", err)
				}
			}
			var finalVersion, finalCount int64
			if err := database.QueryRowContext(ctx, `SELECT max(version_id), count(*) FROM goose_db_version WHERE is_applied`).Scan(&finalVersion, &finalCount); err != nil || finalVersion != 101 || finalCount != historyCount+101-baselineVersion {
				t.Fatalf("升级历史=%d/%d，预期 101/%d：%v", finalVersion, finalCount, historyCount+101-baselineVersion, err)
			}
			for _, column := range []string{"budget", "budget_reached_at", "budget_removed_at"} {
				assertMigrationColumnNullable(t, ctx, database, "sessions", column, "YES")
			}
			assertMigrationColumnNullable(t, ctx, database, "deployments", "budget", "YES")
			assertMigrationColumnNullable(t, ctx, database, "code_sessions", "worker_claim_requested_at", "YES")
			assertMigrationColumnNullable(t, ctx, database, "llm_providers", "api_backend", "NO")
			assertMigrationColumnNullable(t, ctx, database, "llm_providers", "slug", "NO")
			assertMigrationColumnNullable(t, ctx, database, "llm_providers", "models", "NO")
			assertMigrationColumnNullable(t, ctx, database, "llm_providers", "workspace_uuid", "YES")
			assertMigrationColumnExists(t, ctx, database, "llm_providers", "model_ids", false)
			for _, table := range []string{"usage_records", "llm_model_prices"} {
				var exists bool
				if err := database.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil || !exists {
					t.Fatalf("升级后缺少表 %s：%v", table, err)
				}
			}
			var catalogExists bool
			if err := database.QueryRowContext(ctx, `SELECT to_regclass('model_price_catalog') IS NOT NULL`).Scan(&catalogExists); err != nil || catalogExists {
				t.Fatalf("标价目录表未移除: %v", err)
			}
			var sessions int
			if err := database.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE external_id IN ('sesn_migration_work_active','sesn_migration_work_deleted') AND budget IS NULL AND budget_reached_at IS NULL AND budget_removed_at IS NULL`).Scan(&sessions); err != nil || sessions != 2 {
				t.Fatalf("升级未保留原会话：%d/%v", sessions, err)
			}
		})
	}
}
