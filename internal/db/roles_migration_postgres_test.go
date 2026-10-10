package db

import (
	"os"
	"testing"
)

func TestSimplifyRolesMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_MIGRATION_DATABASE_URL to verify role migration with PostgreSQL")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, databaseURL)
	if _, err := provider.UpTo(ctx, 72); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
        INSERT INTO organizations (uuid,name) VALUES ('bb000000-0000-4000-8000-000000000001','roles');
        INSERT INTO workspaces (uuid,external_id,organization_uuid,name,is_default)
        VALUES ('bb000000-0000-4000-8000-000000000002','workspace_roles','bb000000-0000-4000-8000-000000000001','default',true);
        INSERT INTO users (external_id,organization_uuid,email,role,deleted_at)
        SELECT 'user_' || role, 'bb000000-0000-4000-8000-000000000001',role || '@example.local',role,
            CASE WHEN role = 'billing' THEN now() END
        FROM unnest(ARRAY['user','developer','billing','claude_code_user','admin']) AS role;
        INSERT INTO organization_invites (external_id,organization_uuid,email,role,expires_at,status)
        SELECT 'invite_' || role, 'bb000000-0000-4000-8000-000000000001',role || '@example.local',role,now() + interval '1 day','pending'
        FROM unnest(ARRAY['user','developer','billing','claude_code_user','admin']) AS role;
        INSERT INTO workspace_members (external_id,organization_uuid,workspace_uuid,workspace_external_id,user_uuid,user_external_id,workspace_role)
        SELECT 'member_' || role, 'bb000000-0000-4000-8000-000000000001','bb000000-0000-4000-8000-000000000002',
            'workspace_roles',gen_random_uuid(),'user_' || role,role
        FROM unnest(ARRAY['workspace_user','workspace_developer','workspace_restricted_developer','workspace_admin','workspace_billing']) AS role;
    `); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 73); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"UPDATE users SET role = 'developer' WHERE external_id = 'user_user'",
		"UPDATE organization_invites SET role = 'billing' WHERE external_id = 'invite_user'",
		"UPDATE workspace_members SET workspace_role = 'workspace_restricted_developer' WHERE external_id = 'member_workspace_user'",
	} {
		if _, err := database.ExecContext(ctx, statement); err == nil {
			t.Fatalf("removed role accepted: %s", statement)
		}
	}
	for _, statement := range []string{
		"SELECT count(*) FROM users WHERE role = CASE WHEN external_id = 'user_admin' THEN 'admin' ELSE 'user' END",
		"SELECT count(*) FROM organization_invites WHERE role = CASE WHEN external_id = 'invite_admin' THEN 'admin' ELSE 'user' END",
		"SELECT count(*) FROM workspace_members WHERE workspace_role = CASE WHEN external_id = 'member_workspace_admin' THEN 'workspace_admin' ELSE 'workspace_user' END",
	} {
		var count int
		if err := database.QueryRowContext(ctx, statement).Scan(&count); err != nil || count != 5 {
			t.Fatalf("normalized records = %d, err = %v", count, err)
		}
	}
	if _, err := provider.DownTo(ctx, 72); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE role = 'user'").Scan(&count); err != nil || count != 4 {
		t.Fatalf("rollback changed normalized users: count = %d, err = %v", count, err)
	}
	if _, err := database.ExecContext(ctx, "UPDATE users SET role = 'developer' WHERE external_id = 'user_user'"); err != nil {
		t.Fatalf("rollback did not restore old constraint: %v", err)
	}
}
