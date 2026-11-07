//go:build integration

package db

import (
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestWorkspaceOverviewPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is required")
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, address)
	ddl := `CREATE TABLE workspaces(uuid uuid,organization_uuid uuid);
 CREATE TABLE agents(uuid uuid,workspace_uuid uuid,name text,archived_at timestamptz,deleted_at timestamptz);
 CREATE TABLE sessions(uuid uuid,organization_uuid uuid,workspace_uuid uuid,agent_uuid uuid,agent_external_id text,agent_snapshot jsonb,created_at timestamptz,deleted_at timestamptz);
 CREATE TABLE session_events(organization_uuid uuid,workspace_uuid uuid,session_uuid uuid,event_type text,payload jsonb,created_at timestamptz,deleted_at timestamptz);`
	if _, err := database.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	const org = "98000000-0000-0000-0000-000000000001"
	const ws = "98000000-0000-0000-0000-000000000002"
	const agent = "98000000-0000-0000-0000-000000000003"
	const other = "98000000-0000-0000-0000-000000000004"
	if _, err := database.ExecContext(ctx, `INSERT INTO workspaces VALUES($1,$2);`, ws, org); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO agents VALUES($1,$2,'A',NULL,NULL)`, agent, ws); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 5, 16, 30, 0, 0, time.UTC)
	sessions := []struct {
		id        string
		workspace string
		created   time.Time
		deleted   any
	}{
		{"98000000-0000-0000-0000-000000000011", ws, at, nil},
		{"98000000-0000-0000-0000-000000000012", ws, at.Add(time.Hour), nil},
		{"98000000-0000-0000-0000-000000000013", ws, at.Add(2 * time.Hour), nil},
		{"98000000-0000-0000-0000-000000000014", other, at, nil},
		{"98000000-0000-0000-0000-000000000015", ws, at, at},
		{"98000000-0000-0000-0000-000000000016", ws, at.Add(-24 * time.Hour), nil},
	}
	for _, s := range sessions {
		if _, err := database.ExecContext(ctx, `INSERT INTO sessions VALUES($1,$2,$3,$4,'agent_a','{"name":"Snapshot agent"}',$5,$6)`, s.id, org, s.workspace, agent, s.created, s.deleted); err != nil {
			t.Fatal(err)
		}
	}
	events := []struct {
		sid, kind, payload, workspace string
		created                       time.Time
	}{
		{sessions[0].id, "session.status_idle", `{"stop_reason":{"type":"retries_exhausted"}}`, ws, at},
		{sessions[0].id, "session.error", `{"error":{"retry_status":{"type":"exhausted"}}}`, ws, at},
		{sessions[1].id, "session.error", `{"error":{"retry_status":{"type":"retrying"}}}`, ws, at},
		{sessions[1].id, "session.status_idle", `{"stop_reason":{"type":"end_turn"}}`, ws, at},
		{sessions[2].id, "session.status_terminated", `{}`, ws, at},
		{sessions[2].id, "session.error", `{"error":{"retry_status":{"type":"terminal"}}}`, other, at},
		{sessions[2].id, "session.error", `{"error":{"retry_status":{"type":"terminal"}}}`, ws, at.Add(48 * time.Hour)},
	}
	for _, e := range events {
		if _, err := database.ExecContext(ctx, `INSERT INTO session_events VALUES($1,$2,$3,$4,$5,$6,NULL)`, org, e.workspace, e.sid, e.kind, e.payload, e.created); err != nil {
			t.Fatal(err)
		}
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	q := WorkspaceOverviewQuery{OrganizationUUID: org, WorkspaceUUID: ws, Start: at.Add(-30 * time.Minute), End: at.Add(24 * time.Hour), Timezone: "Asia/Shanghai"}
	rows, err := store.AggregateWorkspaceOverview(ctx, q)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].Date != "2026-11-06" || rows[0].SessionCount != 3 || rows[0].FailedSessionCount != 1 || !rows[0].LastRunAt.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("unexpected aggregate: %+v", rows)
	}
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET agent_uuid=$1 WHERE workspace_uuid=$2`, other, ws); err != nil {
		t.Fatal(err)
	}
	rows, err = store.AggregateWorkspaceOverview(ctx, q)
	if err != nil || len(rows) != 1 || rows[0].AgentName != "Snapshot agent" {
		t.Fatalf("snapshot name missing: %+v %v", rows, err)
	}

	q.OrganizationUUID = other
	rows, err = store.AggregateWorkspaceOverview(ctx, q)
	if err != nil || len(rows) != 0 {
		t.Fatalf("organization scope leaked: %+v %v", rows, err)
	}
	count, err := store.CountWorkspaceOverviewAgents(ctx, other, ws)
	if err != nil || count != 0 {
		t.Fatalf("agent scope leaked: %d %v", count, err)
	}
}
