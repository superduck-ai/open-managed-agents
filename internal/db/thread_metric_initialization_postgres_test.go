//go:build integration

package db

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestThreadMetricInitializationPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	tx, err := store.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	execMapperFixtureSQL(t, ctx, tx, `CREATE TEMP TABLE session_threads (
 uuid uuid PRIMARY KEY, external_id text, organization_uuid uuid, workspace_uuid uuid, session_uuid uuid, session_external_id text,
 parent_thread_uuid uuid, parent_thread_external_id text, agent_snapshot jsonb, status text, usage jsonb, stats jsonb,
 created_at timestamptz, updated_at timestamptz, archived_at timestamptz, deleted_at timestamptz
 ) ON COMMIT DROP`)
	execMapperFixtureSQL(t, ctx, tx, `CREATE TEMP TABLE session_events (workspace_uuid uuid,session_uuid uuid, event_type text,payload jsonb) ON COMMIT DROP`)
	org, workspace, sess := "86000000-0000-0000-0000-000000000001", "86000000-0000-0000-0000-000000000002", "86000000-0000-0000-0000-000000000003"
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_threads (uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,agent_snapshot,status,usage,stats,created_at,updated_at) VALUES ($1,'old-root',$2,$3,$4,'session','{}','idle','{}','{}',NOW(),NOW()),($5,'old-child',$2,$3,$4,'session','{}','idle','{"input_tokens":9}','{}',NOW(),NOW())`, "86000000-0000-0000-0000-000000000004", org, workspace, sess, "86000000-0000-0000-0000-000000000005")
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_events VALUES ($1,$2,'session.thread_status_running','{"session_thread_id":"old-child"}')`, workspace, sess)
	raw, err := os.ReadFile("migrations/00087_thread_metric_initialization.sql")
	if err != nil {
		t.Fatal(err)
	}
	execMapperFixtureSQL(t, ctx, tx, strings.Split(string(raw), "-- +goose Down")[0])
	mapper := NewSessionThreadMapper(tx)
	root, err := mapper.FindByExternalID(ctx, workspace, "session", "old-root")
	if err != nil || root.StatsInitialized || root.UsageInitialized {
		t.Fatalf("root wrongly inherited child status: %+v %v", root, err)
	}
	child, err := mapper.FindByExternalID(ctx, workspace, "session", "old-child")
	if err != nil || !child.StatsInitialized || child.UsageInitialized {
		t.Fatalf("running child must hide accumulated usage before idle: %+v %v", child, err)
	}
	if _, err = mapper.SetStatus(ctx, workspace, "session", "old-child", "idle"); err != nil {
		t.Fatal(err)
	}
	child, err = mapper.FindByExternalID(ctx, workspace, "session", "old-child")
	if err != nil || !child.UsageInitialized || string(child.Usage) != `{"input_tokens": 9}` {
		t.Fatalf("idle lost usage: %+v %v", child, err)
	}
	for _, status := range []string{"running", "idle", "rescheduling"} {
		if _, err = mapper.SetStatus(ctx, workspace, "session", "old-child", status); err != nil {
			t.Fatal(err)
		}
	}
	child, err = mapper.FindByExternalID(ctx, workspace, "session", "old-child")
	if err != nil || !child.UsageInitialized {
		t.Fatal("later state reset first idle", err)
	}
	p := sessionThreadWriteParams{UUID: "86000000-0000-0000-0000-000000000006", ExternalID: "fresh", OrganizationUUID: org, WorkspaceUUID: workspace, SessionUUID: sess, SessionExternalID: "session", AgentSnapshot: []byte(`{}`), Status: "idle", Stats: []byte(`{}`), Usage: []byte(`{}`), CreatedAt: time.Now()}
	fresh, err := mapper.Insert(ctx, p)
	if err != nil || fresh.StatsInitialized || fresh.UsageInitialized {
		t.Fatalf("cold insert %+v %v", fresh, err)
	}
	archived, err := mapper.Archive(ctx, workspace, "session", "fresh")
	if err != nil || !archived.StatsInitialized || archived.UsageInitialized {
		t.Fatalf("archive is status not first idle: %+v %v", archived, err)
	}
	p.UUID = "86000000-0000-0000-0000-000000000007"
	p.ExternalID = "fallback"
	p.Status = "running"
	fallback, err := mapper.Insert(ctx, p)
	if err != nil || !fallback.StatsInitialized || fallback.UsageInitialized {
		t.Fatalf("running fallback %+v %v", fallback, err)
	}
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_events VALUES ($1,$2,'session.status_idle','{}')`, workspace, sess)
	p.UUID = "86000000-0000-0000-0000-000000000008"
	p.ExternalID = "legacy-root"
	p.Status = "idle"
	fallback, err = mapper.Insert(ctx, p)
	if err != nil || !fallback.UsageInitialized || !fallback.StatsInitialized {
		t.Fatalf("legacy root lost actual earlier idle: %+v %v", fallback, err)
	}
}
