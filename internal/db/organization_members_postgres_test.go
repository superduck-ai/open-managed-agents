package db

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func TestOrganizationMemberChangesPostgreSQL(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("load database config: %v", err)
		}
		databaseURL = cfg.Database.URL
	}
	ctx, sqlDB, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	database := &DB{mapperDB: yourbatis.NewDB(sqlDB, yourbatis.DialectPostgres)}
	execMapperFixtureSQL(t, ctx, database.mapperDB, `CREATE TABLE organizations (uuid uuid PRIMARY KEY)`)
	execMapperFixtureSQL(t, ctx, database.mapperDB, `CREATE TABLE users (
		uuid uuid PRIMARY KEY DEFAULT gen_random_uuid(), external_id text NOT NULL, organization_uuid uuid NOT NULL,
		email text NOT NULL DEFAULT '', name text NOT NULL DEFAULT '', role text NOT NULL,
		added_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), deleted_at timestamptz)`)
	execMapperFixtureSQL(t, ctx, database.mapperDB, `CREATE TABLE workspace_members (
		organization_uuid uuid, workspace_uuid uuid, user_uuid uuid, deleted_at timestamptz, updated_at timestamptz DEFAULT now())`)
	execMapperFixtureSQL(t, ctx, database.mapperDB, `CREATE TABLE workspaces (
		uuid uuid PRIMARY KEY, organization_uuid uuid NOT NULL, is_default boolean NOT NULL DEFAULT false)`)
	orgUUID := "11111111-1111-4111-8111-111111111111"
	otherOrgUUID := "22222222-2222-4222-8222-222222222222"
	execMapperFixtureSQL(t, ctx, database.mapperDB, `INSERT INTO organizations VALUES ($1), ($2)`, orgUUID, otherOrgUUID)
	execMapperFixtureSQL(t, ctx, database.mapperDB, `INSERT INTO users (external_id, organization_uuid, role) VALUES ('user_admin', $1, 'admin'), ('user_other', $2, 'admin')`, orgUUID, otherOrgUUID)

	t.Run("拒绝移除或降级最后管理员", func(t *testing.T) {
		if _, err := database.DeleteAdminUser(ctx, orgUUID, "user_admin"); !errors.Is(err, ErrLastOrganizationAdmin) {
			t.Fatalf("delete error = %v", err)
		}
		if _, err := database.UpdateAdminUserRole(ctx, orgUUID, "user_admin", "user"); !errors.Is(err, ErrLastOrganizationAdmin) {
			t.Fatalf("update error = %v", err)
		}
	})
	t.Run("跨组织引用不存在", func(t *testing.T) {
		if _, err := database.DeleteAdminUser(ctx, orgUUID, "user_other"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("delete error = %v", err)
		}
	})
	t.Run("并发删除与降级不能清空管理员", func(t *testing.T) {
		execMapperFixtureSQL(t, ctx, database.mapperDB, `INSERT INTO users (external_id, organization_uuid, role) VALUES ('user_second', $1, 'admin')`, orgUUID)
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		workers.Go(func() { <-start; _, err := database.DeleteAdminUser(ctx, orgUUID, "user_admin"); results <- err })
		workers.Go(func() {
			<-start
			_, err := database.UpdateAdminUserRole(ctx, orgUUID, "user_second", "user")
			results <- err
		})
		close(start)
		workers.Wait()
		close(results)
		succeeded, rejected := 0, 0
		for err := range results {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ErrLastOrganizationAdmin):
				rejected++
			default:
				t.Fatalf("change error = %v", err)
			}
		}
		if succeeded != 1 || rejected != 1 {
			t.Fatalf("results = %d success, %d rejected", succeeded, rejected)
		}
	})
	t.Run("组织移除级联且重复移除失败", func(t *testing.T) {
		execMapperFixtureSQL(t, ctx, database.mapperDB, `INSERT INTO users (external_id, organization_uuid, role) VALUES ('user_member', $1, 'user')`, orgUUID)
		execMapperFixtureSQL(t, ctx, database.mapperDB, `INSERT INTO workspaces (uuid, organization_uuid, is_default) VALUES ('33333333-3333-4333-8333-333333333333', $1, false), ('44444444-4444-4444-8444-444444444444', $1, true)`, orgUUID)
		execMapperFixtureSQL(t, ctx, database.mapperDB, `INSERT INTO workspace_members (organization_uuid, workspace_uuid, user_uuid) SELECT organization_uuid, '33333333-3333-4333-8333-333333333333', uuid FROM users WHERE external_id = 'user_member'`)
		if _, err := database.DeleteAdminUser(ctx, orgUUID, "user_member"); err != nil {
			t.Fatal(err)
		}
		if _, err := database.DeleteAdminUser(ctx, orgUUID, "user_member"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("repeat error = %v", err)
		}
		var count int
		if err := sqlDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspace_members WHERE deleted_at IS NULL`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("active members = %d, %v", count, err)
		}
		if _, err := database.GetAdminUser(ctx, otherOrgUUID, "user_other"); err != nil {
			t.Fatalf("other organization changed: %v", err)
		}
	})
}
