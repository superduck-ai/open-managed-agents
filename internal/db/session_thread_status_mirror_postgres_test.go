//go:build integration

package db

import (
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func threadStatusFixture(t *testing.T) (*yourbatis.Tx, Session, SessionThread, SessionThread) {
	t.Helper()
	ctx := t.Context()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	tx, err := store.mapperDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
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
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_threads(uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,parent_thread_uuid,agent_snapshot,status,usage,stats,created_at,updated_at)
		VALUES ($1,'primary',$3,$4,$5,'own-session',NULL,'{}','running','{}','{}',NOW(),NOW()),
		       ($2,'child',$3,$4,$5,'own-session',$1,'{}','running','{}','{}',NOW(),NOW())`,
		primary.UUID, child.UUID, org, workspace, sid)
	return tx, Session{UUID: sid, ExternalID: "own-session", OrganizationUUID: org, WorkspaceUUID: workspace, Status: "running"}, primary, child
}

func TestChildStatusMirrorSurvivesAppliedStatePostgres(t *testing.T) {
	ctx := t.Context()
	tx, session, primary, child := threadStatusFixture(t)
	payload := []byte(`{"id":"original-status","type":"session.thread_status_running","session_thread_id":"child","agent_name":"worker"}`)
	now := time.Now().UTC()
	source := SessionEvent{UUID: "88000000-0000-0000-0000-000000000006", ExternalID: "original-status", EventType: "session.thread_status_running", StatusThreadID: child.ExternalID, ThreadExternalID: &primary.ExternalID, Payload: payload, ProcessedAt: now, CreatedAt: now}
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_events(uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,thread_uuid,thread_external_id,event_type,payload,processed_at,created_at) VALUES($1,$2,$3,$4,$5,'own-session',$6,'primary','session.thread_status_running',CAST($7 AS jsonb),$8,$8)`, source.UUID, source.ExternalID, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, primary.UUID, string(payload), now)
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
	if session.Status != "running" {
		t.Fatal("mirror changed session state")
	}
}

func TestPublicIdleHistoryPostgres(t *testing.T) {
	ctx := t.Context()
	tx, session, _, _ := threadStatusFixture(t)

	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_events(uuid,external_id,organization_uuid,workspace_uuid,session_uuid,session_external_id,thread_external_id,event_type,payload,processed_at,created_at)
		SELECT gen_random_uuid(), external_id, $1, $2, $3, 'own-session', thread_id, event_type, payload::jsonb, $4, $4 FROM (VALUES
			('child-idle', 'primary', 'session.thread_status_idle', '{"session_thread_id":"child"}'),
			('child-copy', 'child', 'session.thread_status_idle', '{"session_thread_id":"child","original_event_id":"child-idle"}'),
			('parent-idle', 'primary', 'session.thread_status_idle', '{"session_thread_id":"primary"}'),
			('second-child-idle', 'child', 'session.thread_status_idle', '{"session_thread_id":"child"}'),
			('second-child-copy', 'primary', 'session.thread_status_idle', '{"session_thread_id":"child","original_event_id":"second-child-idle"}'),
			('session-idle', 'primary', 'session.status_idle', '{}'),
			('legacy-child-idle', 'child', 'session.thread_status_idle', '{}')
		) entries(external_id, thread_id, event_type, payload)`, session.OrganizationUUID, session.WorkspaceUUID, session.UUID, time.Now().UTC())
	for _, tc := range []struct {
		name        string
		threadID    string
		sessionView bool
		cursor      string
		descending  bool
		wantIDs     []string
	}{
		{"session", "", true, "", false, []string{"child-idle", "parent-idle", "second-child-idle", "session-idle", "legacy-child-idle"}},
		{"primary", "primary", false, "", false, []string{"parent-idle", "session-idle"}},
		{"child", "child", false, "", false, []string{"child-idle", "second-child-idle", "legacy-child-idle"}},
		{"child after original", "child", false, "child-idle", false, []string{"second-child-idle", "legacy-child-idle"}},
		{"session before child", "primary", true, "second-child-idle", true, []string{"parent-idle", "child-idle"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := sessionEventPageMapperParams{
				WorkspaceUUID: session.WorkspaceUUID, SessionExternalID: session.ExternalID, ThreadExternalID: tc.threadID,
				PublicThreadIdle: true, PrimaryOnly: tc.sessionView, FetchLimit: 1, Descending: tc.descending,
			}
			if tc.cursor != "" {
				params.Cursor = &SessionEventPageCursor{ExternalID: tc.cursor}
			}
			var ids []string
			for len(ids) <= len(tc.wantIDs) {
				rows, err := NewSessionEventMapper(tx).ListPage(ctx, params)
				if err != nil {
					t.Fatal(err)
				}
				if len(rows) == 0 {
					break
				}
				ids = append(ids, rows[0].ExternalID)
				params.Cursor = &SessionEventPageCursor{ExternalID: rows[0].ExternalID}
			}
			if !slices.Equal(ids, tc.wantIDs) {
				t.Errorf("paged history = %v, want %v", ids, tc.wantIDs)
			}
		})
	}
}
