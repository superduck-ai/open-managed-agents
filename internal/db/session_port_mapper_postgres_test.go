//go:build integration

package db

import (
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionPortMapperPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	for _, statement := range []string{
		`CREATE TABLE sessions (uuid uuid, external_id text, organization_uuid uuid, workspace_uuid uuid, archived_at timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE code_sessions (uuid uuid, external_id text, organization_uuid uuid, workspace_uuid uuid, session_uuid uuid,
			status text, deleted_at timestamptz, created_at timestamptz, port_last_access_at timestamptz)`,
		`CREATE TABLE environment_work (uuid uuid, organization_uuid uuid, workspace_uuid uuid, environment_uuid uuid, session_uuid uuid, state text, deleted_at timestamptz)`,
		`CREATE TABLE environment_sandboxes (uuid uuid, organization_uuid uuid, workspace_uuid uuid, environment_uuid uuid, work_uuid uuid,
			provider_sandbox_id text, state text, created_at timestamptz)`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	const (
		org     = "53000000-0000-0000-0000-000000000001"
		ws      = "53000000-0000-0000-0000-000000000002"
		session = "53000000-0000-0000-0000-000000000003"
		code    = "53000000-0000-0000-0000-000000000004"
		env     = "53000000-0000-0000-0000-000000000005"
		work    = "53000000-0000-0000-0000-000000000006"
	)
	mapper := NewSessionPortMapper(yourbatis.NewDB(database, yourbatis.DialectPostgres))
	if _, found, err := mapper.FindTarget(ctx, session); err != nil || found {
		t.Fatalf("missing session: found=%t err=%v", found, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO sessions VALUES ($1,'sesn_1',$2,$3,NULL,NULL)`, session, org, ws); err != nil {
		t.Fatal(err)
	}
	row, found, err := mapper.FindTarget(ctx, session)
	if err != nil || !found || row.Removed || row.CodeSessionUUID != "" || row.ProviderSandboxID != "" || row.SandboxState != "" {
		t.Fatalf("session without runtime = %+v found=%t err=%v", row, found, err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO code_sessions VALUES ($1,'cse_1',$2,$3,$4,'active',NULL,now(),NULL)`, code, org, ws, session); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO environment_work VALUES ($1,$2,$3,$4,$5,'active',NULL)`, work, org, ws, env, session); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO environment_sandboxes VALUES (gen_random_uuid(),$1,$2,$3,$4,'sbx_old','stopped',now()-interval '1 hour'),
		(gen_random_uuid(),$1,$2,$3,$4,'sbx_new','running',now())`, org, ws, env, work); err != nil {
		t.Fatal(err)
	}
	row, found, err = mapper.FindTarget(ctx, session)
	if err != nil || !found || row.CodeSessionUUID != code || row.CodeSessionExternalID != "cse_1" || row.ProviderSandboxID != "sbx_new" || row.SandboxState != "running" {
		t.Fatalf("session runtime = %+v found=%t err=%v", row, found, err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	params := sessionPortAccessParams{OrganizationUUID: org, WorkspaceUUID: ws, CodeSessionUUID: code, Now: now, Threshold: now.Add(-time.Minute)}
	if err := mapper.TouchPortLastAccess(ctx, params); err != nil {
		t.Fatal(err)
	}
	later := params
	later.Now, later.Threshold = now.Add(30*time.Second), now.Add(-30*time.Second)
	if err := mapper.TouchPortLastAccess(ctx, later); err != nil {
		t.Fatal(err)
	}
	var stored time.Time
	if err := database.QueryRowContext(ctx, `SELECT port_last_access_at FROM code_sessions WHERE uuid=$1`, code).Scan(&stored); err != nil || !stored.Equal(now) {
		t.Fatalf("throttled access = %s err=%v, want %s", stored, err, now)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET archived_at=now()`); err != nil {
		t.Fatal(err)
	}
	if row, _, err := mapper.FindTarget(ctx, session); err != nil || !row.Removed {
		t.Fatalf("archived session = %+v err=%v", row, err)
	}
}
