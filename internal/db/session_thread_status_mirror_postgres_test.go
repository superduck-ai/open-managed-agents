//go:build integration

package db

import (
	"context"
	"database/sql"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"testing"
	"time"
)

func TestChildStatusMirrorSurvivesAppliedStatePostgres(t *testing.T) {
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
 uuid uuid PRIMARY KEY, external_id text,organization_uuid uuid,workspace_uuid uuid,session_uuid uuid,session_external_id text,
 parent_thread_uuid uuid,parent_thread_external_id text,agent_snapshot jsonb,status text,usage jsonb,stats jsonb,
 stats_initialized boolean DEFAULT false,usage_initialized boolean DEFAULT false,created_at timestamptz,updated_at timestamptz,archived_at timestamptz,deleted_at timestamptz
 ) ON COMMIT DROP`)
	execMapperFixtureSQL(t, ctx, tx, `CREATE TEMP TABLE session_events (
 id bigint GENERATED ALWAYS AS IDENTITY,uuid uuid,external_id text,organization_uuid uuid,workspace_uuid uuid,session_uuid uuid,session_external_id text,
 thread_uuid uuid,thread_external_id text,event_type text,payload jsonb,payload_blob_uuid uuid,tool_use_id text,processed_at timestamptz,created_at timestamptz,deleted_at timestamptz,
 UNIQUE(workspace_uuid,external_id)
 ) ON COMMIT DROP`)
	const org = "88000000-0000-0000-0000-000000000001"
	const workspace = "88000000-0000-0000-0000-000000000002"
	const sid = "88000000-0000-0000-0000-000000000003"
	primary := SessionThread{UUID: "88000000-0000-0000-0000-000000000004", ExternalID: "primary"}
	child := SessionThread{UUID: "88000000-0000-0000-0000-000000000005", ExternalID: "child"}
	for _, thread := range []SessionThread{primary, child} {
		execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_threads(uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,agent_snapshot,status,usage,stats,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'own-session','{}','idle','{}','{}',NOW(),NOW())`, thread.UUID, thread.ExternalID, org, workspace, sid)
	}
	session := Session{UUID: sid, ExternalID: "own-session", OrganizationUUID: org, WorkspaceUUID: workspace, Status: "idle"}
	payload := []byte(`{"id":"original-status","type":"session.thread_status_idle","session_thread_id":"child","agent_name":"worker","stop_reason":{"type":"end_turn"}}`)
	now := time.Now().UTC()
	source := SessionEvent{UUID: "88000000-0000-0000-0000-000000000006", ExternalID: "original-status", EventType: "session.thread_status_idle", StatusThreadID: child.ExternalID, ThreadExternalID: &primary.ExternalID, Payload: payload, ProcessedAt: now, CreatedAt: now}
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_events(uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,thread_uuid,thread_external_id,event_type,payload,processed_at,created_at) VALUES($1,$2,$3,$4,$5,'own-session',$6,'primary','session.thread_status_idle',CAST($7 AS jsonb),$8,$8)`, source.UUID, source.ExternalID, org, workspace, sid, primary.UUID, string(payload), now)
	ordinary, err := shouldWriteSessionStatus(ctx, tx, session, primary, source)
	if err != nil || ordinary {
		t.Fatal("ordinary duplicate status suppression changed", err)
	}
	copies, err := sessionThreadStatusCopies(source, primary, child)
	if err != nil || len(copies) != 2 {
		t.Fatal(err)
	}
	write, err := insertSessionEventIfAbsentTx(ctx, tx, &session, primary, copies[1])
	if err != nil || !write.Inserted || write.Event.ThreadExternalID == nil || *write.Event.ThreadExternalID != child.ExternalID {
		t.Fatal("own lifecycle mirror filtered after state transition", err)
	}
	again, err := insertSessionEventIfAbsentTx(ctx, tx, &session, primary, copies[1])
	if err != nil || again.Inserted {
		t.Fatal("replayed mirror duplicated", err)
	}
	var own, parent int
	if err := tx.SQLTx().QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE thread_external_id='child'),count(*) FILTER(WHERE thread_external_id='primary') FROM session_events`).Scan(&own, &parent); err != nil || own != 1 || parent != 1 {
		t.Fatal("missing or duplicate stream history", err)
	}
	if session.Status != "idle" {
		t.Fatal("mirror changed session state")
	}
}
