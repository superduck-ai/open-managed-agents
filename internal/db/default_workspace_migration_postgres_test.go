package db

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultWorkspaceMarkerMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("设置 TEST_MIGRATION_DATABASE_URL 运行真实迁移验收；跳过不能视为验收通过")
	}
	for _, ambiguous := range []bool{true, false} {
		name := "保留历史成员与资源身份"
		if ambiguous {
			name = "歧义默认空间原子失败"
		}
		t.Run(name, func(t *testing.T) {
			ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
			if _, err := provider.UpTo(ctx, 58); err != nil {
				t.Fatal(err)
			}
			_, err := database.ExecContext(ctx, `
    INSERT INTO organizations (uuid,name) VALUES ('aa000000-0000-4000-8000-000000000001','标识迁移');
    INSERT INTO workspaces (uuid,external_id,organization_uuid,name)
    VALUES ('aa000000-0000-4000-8000-000000000002','wrkspc_marker_default','aa000000-0000-4000-8000-000000000001','default');
    INSERT INTO users (uuid,external_id,organization_uuid,email,name,role)
    VALUES ('aa000000-0000-4000-8000-000000000003','user_marker','aa000000-0000-4000-8000-000000000001','marker@example.local','成员','user');
    INSERT INTO workspace_members (external_id,organization_uuid,workspace_uuid,workspace_external_id,user_uuid,user_external_id,workspace_role)
    VALUES ('wmem_marker','aa000000-0000-4000-8000-000000000001','aa000000-0000-4000-8000-000000000002','wrkspc_marker_default','aa000000-0000-4000-8000-000000000003','user_marker','workspace_admin');
   `)
			if err != nil {
				t.Fatal(err)
			}
			var before string
			if err := database.QueryRowContext(ctx, `SELECT row_to_json(wm)::text FROM workspace_members wm WHERE external_id='wmem_marker'`).Scan(&before); err != nil {
				t.Fatal(err)
			}
			if ambiguous {
				if _, err := database.ExecContext(ctx, `INSERT INTO workspaces (external_id,organization_uuid,name) VALUES ('wrkspc_ambiguous','aa000000-0000-4000-8000-000000000001','Default')`); err != nil {
					t.Fatal(err)
				}
			}
			_, migrationErr := provider.UpTo(ctx, 59)
			if ambiguous {
				if migrationErr == nil || !strings.Contains(migrationErr.Error(), "Cannot identify active default workspace") {
					t.Fatalf("err = %v", migrationErr)
				}
			} else {
				if migrationErr != nil {
					t.Fatal(migrationErr)
				}
				var marked bool
				if err := database.QueryRowContext(ctx, `SELECT is_default FROM workspaces WHERE uuid='aa000000-0000-4000-8000-000000000002'`).Scan(&marked); err != nil || !marked {
					t.Fatalf("marked = %t, err = %v", marked, err)
				}
				if _, err := database.ExecContext(ctx, `INSERT INTO workspaces (external_id,organization_uuid,name,is_default) VALUES ('wrkspc_duplicate','aa000000-0000-4000-8000-000000000001','Duplicate',true)`); err == nil {
					t.Fatal("允许了第二个默认空间")
				}
			}
			var after string
			if err := database.QueryRowContext(ctx, `SELECT row_to_json(wm)::text FROM workspace_members wm WHERE external_id='wmem_marker'`).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("历史成员记录被改写")
			}
		})
	}
}
