package db

import (
	"os"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestGlobalLoginIdentityIndexPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不代表验收通过")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
	if _, err := provider.UpTo(ctx, 83); err != nil {
		t.Fatal(err)
	}
	var mainlineReady, platformAbsent bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='code_sessions' AND column_name='worker_claimed_by'), to_regclass('platform_admins') IS NULL`).Scan(&mainlineReady, &platformAbsent); err != nil || !mainlineReady || !platformAbsent {
		t.Fatalf("主线迁移 83 与平台迁移边界不一致：claims=%t platformAbsent=%t %v", mainlineReady, platformAbsent, err)
	}
	if _, err := provider.UpTo(ctx, 87); err != nil {
		t.Fatal(err)
	}
	store := yourbatis.NewDB(database, yourbatis.DialectPostgres)
	execMapperFixtureSQL(t, ctx, store, `INSERT INTO organizations (uuid, name) VALUES ('a0000000-0000-0000-0000-000000000001', '索引验收'), ('a0000000-0000-0000-0000-000000000002', '旧身份来源')`)
	execMapperFixtureSQL(t, ctx, store, `INSERT INTO users (uuid, external_id, organization_uuid, email, role)
		SELECT gen_random_uuid(), 'index_' || n, 'a0000000-0000-0000-0000-000000000001', 'index-' || n || '@example.com', 'user' FROM generate_series(1, 2000) n`)
	const accountUUID = "b0000000-0000-0000-0000-000000000001"
	execMapperFixtureSQL(t, ctx, store, `INSERT INTO users (uuid, external_id, organization_uuid, email, role, registration_identity, added_at)
		VALUES ($1, 'current_identity', 'a0000000-0000-0000-0000-000000000001', 'Case@example.com', 'user', true, now()),
		('b0000000-0000-0000-0000-000000000002', 'old_identity', 'a0000000-0000-0000-0000-000000000002', 'case@example.com', 'admin', false, now()-interval '1 day')`, accountUUID)
	execMapperFixtureSQL(t, ctx, store, `CREATE INDEX users_login_identity_idx ON users (uuid)`)
	for _, stage := range []string{"首次升级并替换残留索引", "回滚后重新升级"} {
		t.Run(stage, func(t *testing.T) {
			if _, err := provider.UpTo(ctx, 88); err != nil {
				t.Fatal(err)
			}
			var definition string
			var valid bool
			if err := database.QueryRowContext(ctx, `SELECT pg_get_indexdef(indexrelid), indisvalid FROM pg_index WHERE indexrelid = 'users_login_identity_idx'::regclass`).Scan(&definition, &valid); err != nil {
				t.Fatal(err)
			}
			if !valid || !strings.Contains(definition, "(lower(email), registration_identity DESC, added_at, uuid)") {
				t.Fatalf("全局登录身份索引无效：%s，valid=%t", definition, valid)
			}
			execMapperFixtureSQL(t, ctx, store, `ANALYZE users`)
			bound := buildPlatformAdminMapperFindAccount(yourbatis.DialectPostgres, accountUUID)
			var plan string
			if err := database.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+bound.SQL, bound.Values()...).Scan(&plan); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(plan, `"Index Name": "users_login_identity_idx"`) {
				t.Fatalf("实际账号查询未使用全局邮箱索引：%s", plan)
			}
			account, err := NewPlatformAdminMapper(store).FindAccount(ctx, accountUUID)
			if err != nil || account.UUID != accountUUID {
				t.Fatalf("索引改变登录身份选择：%+v %v", account, err)
			}
			if _, err := provider.Down(ctx); err != nil {
				t.Fatal(err)
			}
			var removed bool
			if err := database.QueryRowContext(ctx, "SELECT to_regclass('users_login_identity_idx') IS NULL").Scan(&removed); err != nil || !removed {
				t.Fatalf("回滚未移除索引：removed=%t %v", removed, err)
			}
		})
	}
}
