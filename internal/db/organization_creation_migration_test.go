//go:build integration

package db

import (
	"encoding/json/v2"
	"os"
	"testing"
	"uuid"
)

func TestOrganizationCreationMigration(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不能视为验收通过")
	}
	for _, scenario := range []string{"缺失组织", "重复组织绑定", "现存与过期记录"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, address)
			if _, err := provider.UpTo(ctx, 94); err != nil {
				t.Fatal(err)
			}
			account := uuid.NewV4().String()
			orgs := []string{uuid.NewV4().String(), uuid.NewV4().String()}
			if scenario != "缺失组织" {
				for _, org := range orgs {
					if _, err := database.ExecContext(ctx, `INSERT INTO organizations (uuid,name) VALUES ($1,'当前名称')`, org); err != nil {
						t.Fatal(err)
					}
				}
			}
			for i, key := range []string{"active", "expired"} {
				org := orgs[i]
				if scenario == "重复组织绑定" {
					org = orgs[0]
				}
				if _, err := database.ExecContext(ctx, `INSERT INTO platform_admin_create_requests (account_uuid,idempotency_key,request_hash,response,expires_at)
 VALUES ($1,$2,'digest',jsonb_build_object('uuid',$3::text,'name','首次名称'),now() + $4 * interval '1 day')`, account, key, org, 1-2*i); err != nil {
					t.Fatal(err)
				}
			}
			_, err := provider.UpTo(ctx, 95)
			if scenario != "现存与过期记录" {
				if err == nil {
					t.Fatal("无效绑定应阻止迁移")
				}
				var count int
				if err := database.QueryRowContext(ctx, `SELECT count(*) FROM platform_admin_create_requests`).Scan(&count); err != nil || count != 2 {
					t.Fatalf("迁移失败丢失记录：%d %v", count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			verify := func() {
				t.Helper()
				var count int
				var removed bool
				if err := database.QueryRowContext(ctx, `SELECT count(*), to_regclass(current_schema() || '.platform_admin_create_requests') IS NULL FROM organizations WHERE creation_account_uuid=$1 AND creation_request_hash='digest' AND creation_idempotency_key IN ('active','expired')`, account).Scan(&count, &removed); err != nil || count != 2 || !removed {
					t.Fatalf("幂等记录未归并：%d %v %v", count, removed, err)
				}
				if _, err := database.ExecContext(ctx, `INSERT INTO organizations (name,creation_account_uuid,creation_idempotency_key,creation_request_hash) VALUES ('冲突',$1,'expired','other')`, account); err == nil {
					t.Fatal("数据库唯一约束未生效")
				}
				if _, err := database.ExecContext(ctx, `INSERT INTO organizations (name,creation_account_uuid) VALUES ('不完整',$1)`, account); err == nil {
					t.Fatal("不完整创建信息应被拒绝")
				}
				if _, err := database.ExecContext(ctx, `INSERT INTO organizations (name) VALUES ('普通注册'),('另一注册')`); err != nil {
					t.Fatal(err)
				}
			}
			verify()
			if _, err := provider.Down(ctx); err != nil {
				t.Fatal(err)
			}
			var response []byte
			var permanent bool
			if err := database.QueryRowContext(ctx, `SELECT response, expires_at='infinity'::timestamptz FROM platform_admin_create_requests WHERE account_uuid=$1 AND idempotency_key='expired'`, account).Scan(&response, &permanent); err != nil {
				t.Fatal(err)
			}
			var restored struct {
				UUID    string `json:"uuid"`
				Name    string `json:"name"`
				Version int64  `json:"version"`
			}
			if err := json.Unmarshal(response, &restored); err != nil || restored.UUID != orgs[1] || restored.Name != "当前名称" || restored.Version != 1 || !permanent {
				t.Fatalf("回滚未恢复当前资料和长期键：%+v %v %v", restored, permanent, err)
			}
			if _, err := provider.UpTo(ctx, 95); err != nil {
				t.Fatal(err)
			}
			verify()
		})
	}
}
