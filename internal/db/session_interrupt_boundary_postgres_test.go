//go:build integration

package db

import (
	"os"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/yourbatis"
)

func TestInterruptedTurnIdleBoundaryPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if databaseURL == "" {
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		databaseURL = cfg.Database.URL
	}
	ctx, database, _ := newIsolatedMigrationTestDatabase(t, databaseURL)
	_, err := database.ExecContext(ctx, `CREATE TABLE session_events (
 id bigint generated always as identity, workspace_uuid uuid, session_external_id text,
 thread_external_id text,event_type text,payload jsonb DEFAULT '{}',processed_at timestamptz,deleted_at timestamptz
 )`)
	if err != nil {
		t.Fatal(err)
	}
	const workspace = "52000000-0000-0000-0000-000000000001"
	const foreign = "52000000-0000-0000-0000-000000000002"
	now := time.Date(2026, 10, 18, 0, 45, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, interruptWorkspace, interruptSession, interruptThread string
		offset                                                      time.Duration
		unprocessed, deleted, noStart, noPending, wantApply         bool
	}{
		{name: "processed interrupt preserves queued next-turn boundary", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "thread", offset: time.Second, wantApply: true},
		{name: "ordinary queued message keeps running"},
		{name: "previous turn interrupt does not close current turn", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "thread", offset: -time.Second},
		{name: "unprocessed interrupt does not close current turn", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "thread", unprocessed: true},
		{name: "future interrupt cannot justify earlier result", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "thread", offset: 3 * time.Second},
		{name: "foreign thread interrupt cannot close current turn", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "child", offset: time.Second},
		{name: "foreign session interrupt cannot close current turn", interruptWorkspace: workspace, interruptSession: "other", interruptThread: "thread", offset: time.Second},
		{name: "foreign workspace interrupt cannot close current turn", interruptWorkspace: foreign, interruptSession: "session", interruptThread: "thread", offset: time.Second},
		{name: "deleted interrupt cannot close current turn", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "thread", offset: time.Second, deleted: true},
		{name: "missing turn start cannot claim current interruption", interruptWorkspace: workspace, interruptSession: "session", interruptThread: "thread", offset: time.Second, noStart: true},
		{name: "ordinary completion without pending input stays valid", noPending: true, wantApply: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `DELETE FROM session_events`); err != nil {
				t.Fatal(err)
			}
			if !tc.noStart {
				if _, err := database.ExecContext(ctx, `INSERT INTO session_events(workspace_uuid,session_external_id,thread_external_id,event_type,payload,processed_at) VALUES($1,'session','thread','session.thread_status_running','{"session_thread_id":"thread"}',$2)`, workspace, now); err != nil {
					t.Fatal(err)
				}
			}
			if tc.interruptWorkspace != "" {
				var processed, deleted any = now.Add(tc.offset), nil
				if tc.unprocessed {
					processed = nil
				}
				if tc.deleted {
					deleted = now
				}
				if _, err := database.ExecContext(ctx, `INSERT INTO session_events(workspace_uuid,session_external_id,thread_external_id,event_type,processed_at,deleted_at) VALUES($1,$2,$3,'user.interrupt',$4,$5)`, tc.interruptWorkspace, tc.interruptSession, tc.interruptThread, processed, deleted); err != nil {
					t.Fatal(err)
				}
			}
			if !tc.noPending {
				if _, err := database.ExecContext(ctx, `INSERT INTO session_events(workspace_uuid,session_external_id,thread_external_id,event_type) VALUES($1,'session','thread','user.message')`, workspace); err != nil {
					t.Fatal(err)
				}
			}
			source := SessionEvent{ProcessedAt: now.Add(2 * time.Second)}
			_, apply, err := resolveIdleStopReasonTx(ctx, yourbatis.NewDB(database, yourbatis.DialectPostgres), Session{WorkspaceUUID: workspace, ExternalID: "session"}, SessionThread{ExternalID: "thread", Status: "running"}, &source, &sessionStopReason{Type: "end_turn"})
			if err != nil || apply != tc.wantApply {
				t.Fatalf("apply=%t want=%t err=%v", apply, tc.wantApply, err)
			}
		})
	}
}
