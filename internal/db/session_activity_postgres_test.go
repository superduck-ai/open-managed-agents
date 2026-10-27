//go:build integration

package db

import (
	"encoding/json"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestSessionActivityMetricsPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		databaseURL = cfg.Database.URL
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	ddl := `CREATE TABLE sessions(uuid uuid PRIMARY KEY,workspace_uuid uuid,status text,updated_at timestamptz,deleted_at timestamptz);
 CREATE TABLE session_threads(session_uuid uuid,workspace_uuid uuid,external_id text,status text,updated_at timestamptz,archived_at timestamptz,deleted_at timestamptz);
 CREATE TABLE session_events(id bigint GENERATED ALWAYS AS IDENTITY,workspace_uuid uuid,session_uuid uuid,thread_external_id text,event_type text,payload jsonb,processed_at timestamptz,deleted_at timestamptz);`
	if _, err := database.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("migrations/00088_session_activity_metrics.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, strings.Split(string(raw), "-- +goose Down")[0]); err != nil {
		t.Fatal(err)
	}
	const workspace = "98000000-0000-0000-0000-000000000001"
	const sid = "98000000-0000-0000-0000-000000000002"
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	if _, err := database.ExecContext(ctx, `INSERT INTO sessions VALUES($1,$2,'idle',$3,NULL)`, sid, workspace, base.Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
	add := func(thread string, start, end int) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `INSERT INTO session_threads VALUES($1,$2,$3,'idle',$4,NULL,NULL)`, sid, workspace, thread, base.Add(time.Duration(end)*time.Second)); err != nil {
			t.Fatal(err)
		}
		for _, event := range []struct {
			kind string
			at   int
		}{{"running", start}, {"idle", end}} {
			if _, err := database.ExecContext(ctx, `INSERT INTO session_events(workspace_uuid,session_uuid,thread_external_id,event_type,payload,processed_at) VALUES($1,$2,$3,$4,jsonb_build_object('session_thread_id',$3::text),$5)`, workspace, sid, thread, "session.thread_status_"+event.kind, base.Add(time.Duration(event.at)*time.Second)); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("primary", 0, 10)
	add("a", 2, 8)
	add("b", 3, 9)
	check := func(thread any, asOf time.Time, active, sum float64) {
		t.Helper()
		var raw []byte
		if err := database.QueryRowContext(ctx, `SELECT oma_session_activity_metrics($1,$2,$3,$4)`, workspace, sid, thread, asOf).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Active float64 `json:"active_seconds"`
			Sum    float64 `json:"stats_active_seconds"`
		}
		if err := json.Unmarshal(raw, &got); err != nil || got.Active != active || got.Sum != sum {
			t.Fatalf("thread=%v metrics=%s expectedactive=%g sum=%g err=%v", thread, raw, active, sum, err)
		}
	}
	check(nil, base.Add(20*time.Second), 10, 22)
	check("a", base.Add(20*time.Second), 6, 6)
	if _, err := database.ExecContext(ctx, `INSERT INTO session_events(workspace_uuid,session_uuid,thread_external_id,event_type,payload,processed_at) VALUES($1,$2,'primary','session.thread_status_running','{"session_thread_id":"b"}',$3)`, workspace, sid, base.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	check(nil, base.Add(20*time.Second), 10, 22)
	if _, err := database.ExecContext(ctx, `UPDATE session_threads SET status='running',updated_at=$3 WHERE session_uuid=$1 AND external_id=$2;`, sid, "b", base.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO session_events(workspace_uuid,session_uuid,thread_external_id,event_type,payload,processed_at) VALUES($1,$2,'b','session.thread_status_running','{"session_thread_id":"b"}',$3)`, workspace, sid, base.Add(12*time.Second)); err != nil {
		t.Fatal(err)
	}
	check(nil, base.Add(15*time.Second), 13, 25)
	if _, err := database.ExecContext(ctx, `UPDATE sessions SET status='terminated',updated_at=$2 WHERE uuid=$1`, sid, base.Add(14*time.Second)); err != nil {
		t.Fatal(err)
	}
	check(nil, base.Add(30*time.Second), 12, 24)
	if _, err := database.ExecContext(ctx, `UPDATE session_threads SET status='terminated',updated_at=$2 WHERE session_uuid=$1 AND external_id='b'`, sid, base.Add(13*time.Second)); err != nil {
		t.Fatal(err)
	}
	check("b", base.Add(30*time.Second), 7, 7)
	var projection sessionActivityProjection
	if err := database.QueryRowContext(ctx, `SELECT oma_session_activity_projection($1,$2,NULL,$3,'{"input_tokens":19}','{"other":1}')`, workspace, sid, base.Add(30*time.Second)).Scan(&projection); err != nil {
		t.Fatal(err)
	}
	var usage map[string]any
	if err := json.Unmarshal(projection.Usage, &usage); err != nil || usage["input_tokens"] != float64(19) || usage["active_seconds"] != float64(11) {
		t.Fatal("lost counters", usage, err)
	}
}

func TestSessionActivityNewWorkerFactAndReplayPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		databaseURL = cfg.Database.URL
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	ddl := `CREATE TABLE sessions(uuid uuid PRIMARY KEY,workspace_uuid uuid,status text,updated_at timestamptz,deleted_at timestamptz);
 CREATE TABLE session_threads(uuid uuid PRIMARY KEY DEFAULT gen_random_uuid(),external_id text,organization_uuid uuid,workspace_uuid uuid,session_uuid uuid,session_external_id text,parent_thread_uuid uuid,parent_thread_external_id text,agent_snapshot jsonb,status text,stats_initialized boolean,usage_initialized boolean,usage jsonb,stats jsonb,created_at timestamptz,updated_at timestamptz,archived_at timestamptz,deleted_at timestamptz);
 CREATE TABLE session_events(id bigint GENERATED ALWAYS AS IDENTITY,uuid uuid,external_id text,organization_uuid uuid,workspace_uuid uuid,session_uuid uuid,session_external_id text,thread_uuid uuid,thread_external_id text,event_type text,payload jsonb,payload_blob_uuid uuid,tool_use_id text,processed_at timestamptz,created_at timestamptz,deleted_at timestamptz,UNIQUE(workspace_uuid,external_id));
 CREATE TABLE code_sessions(uuid uuid DEFAULT gen_random_uuid(),external_id text,workspace_uuid uuid,session_uuid uuid,status text,worker_turn_started boolean,worker_status text,worker_external_metadata jsonb,created_at timestamptz,deleted_at timestamptz);`
	if _, err := database.ExecContext(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("migrations/00088_session_activity_metrics.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, strings.Split(string(raw), "-- +goose Down")[0]); err != nil {
		t.Fatal(err)
	}
	const workspace = "98000000-0000-0000-0000-000000000011"
	const sid = "98000000-0000-0000-0000-000000000012"
	const org = "98000000-0000-0000-0000-000000000013"
	const primary = "primary"
	if _, err := database.ExecContext(ctx, `INSERT INTO sessions VALUES($1,$2,'running',NOW(),NULL);`, sid, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO session_threads(external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,agent_snapshot,status,stats_initialized,usage_initialized,usage,stats,created_at,updated_at) VALUES('primary',$1,$2,$3,'activity-session','{}','running',TRUE,TRUE,'{}','{}',NOW(),NOW())`, org, workspace, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO code_sessions(external_id,workspace_uuid,session_uuid,status,worker_turn_started,worker_status,worker_external_metadata,created_at) VALUES('own-worker',$1,$2,'active',TRUE,'running','{}',NOW())`, workspace, sid); err != nil {
		t.Fatal(err)
	}
	store := yourbatis.NewDB(database, yourbatis.DialectPostgres)
	tx, err := store.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	session := Session{UUID: sid, ExternalID: "activity-session", OrganizationUUID: org, WorkspaceUUID: workspace, Status: "running"}
	now := time.Now().UTC()
	event := SessionEvent{UUID: uuid.NewV4().String(), ExternalID: "fresh-worker-fact", EventType: "agent.thinking", Payload: []byte(`{"type":"agent.thinking","content":[{"type":"thinking","thinking":"measured"}]}`), CreatedAt: now, ProcessedAt: now, ThreadExternalID: func() *string { v := primary; return &v }()}
	first, err := insertSessionHistoryTx(ctx, tx, session, []SessionEvent{event})
	if err != nil || len(first.Events) != 1 {
		t.Fatal("new worker fact must survive missing-history lookup", len(first.Events), err)
	}
	replay, err := insertSessionHistoryTx(ctx, tx, session, []SessionEvent{event})
	if err != nil || len(replay.Events) != 0 {
		t.Fatal("worker replay must remain idempotent", len(replay.Events), err)
	}
}
