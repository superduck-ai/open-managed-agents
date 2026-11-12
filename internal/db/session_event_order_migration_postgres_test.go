//go:build integration

package db

import (
	"os"
	"reflect"
	"testing"
)

func TestSessionEventOrderMigrationBackfillsPositionsPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 117); err != nil {
		t.Fatal(err)
	}
	const organization, workspace = "54000000-0000-0000-0000-000000000001", "54000000-0000-0000-0000-000000000002"
	sessionInsert := `INSERT INTO sessions(uuid,external_id,environment_external_id,agent_external_id,agent_version,agent_snapshot,organization_uuid,workspace_uuid,environment_uuid,agent_uuid)
		VALUES($1,$2,'env','agent',1,'{}',$3,$4,'54000000-0000-0000-0000-0000000000e0','54000000-0000-0000-0000-0000000000a0')`
	execClaimTest(t, ctx, database, sessionInsert, "54000000-0000-0000-0000-0000000000b1", "sesn_one", organization, workspace)
	execClaimTest(t, ctx, database, sessionInsert, "54000000-0000-0000-0000-0000000000b2", "sesn_two", organization, workspace)
	execClaimTest(t, ctx, database, sessionInsert, "54000000-0000-0000-0000-0000000000b3", "sesn_empty", organization, workspace)
	eventInsert := `INSERT INTO session_events(external_id,session_external_id,event_type,payload,processed_at,created_at,organization_uuid,workspace_uuid,session_uuid)
		VALUES($1,$2,'agent.message','{}',$3::timestamptz,$4::timestamptz,$5,$6,$7)`
	for _, row := range [][]any{
		{"late-processed", "sesn_one", "2026-11-12T00:00:05Z", "2026-11-12T00:00:01Z"},
		{"queued", "sesn_one", nil, "2026-11-12T00:00:03Z"},
		{"first", "sesn_one", "2026-11-12T00:00:02Z", "2026-11-12T00:00:02Z"},
		{"queued-before-all", "sesn_one", nil, "2026-11-12T00:00:00Z"},
		{"other-session", "sesn_two", "2026-11-12T00:00:04Z", "2026-11-12T00:00:04Z"},
	} {
		uuidBySession := map[string]string{"sesn_one": "54000000-0000-0000-0000-0000000000b1", "sesn_two": "54000000-0000-0000-0000-0000000000b2"}
		execClaimTest(t, ctx, database, eventInsert, row[0], row[1], row[2], row[3], organization, workspace, uuidBySession[row[1].(string)])
	}
	if _, err := migrations.UpTo(ctx, 118); err != nil {
		t.Fatal(err)
	}

	t.Run("rejects positions that contradict processing state", func(t *testing.T) {
		for _, statement := range []string{
			`UPDATE session_events SET seq = NULL WHERE external_id = 'first'`,
			`UPDATE session_events SET seq = 99 WHERE external_id = 'queued'`,
			`UPDATE session_events SET queued_after_seq = NULL WHERE external_id = 'queued'`,
			`UPDATE session_events SET seq = 1 WHERE external_id = 'late-processed'`,
		} {
			if _, err := database.ExecContext(ctx, statement); err == nil {
				t.Fatalf("accepted %s", statement)
			}
		}
	})

	t.Run("orders by previous public position and records queue positions", func(t *testing.T) {
		rows, err := database.QueryContext(ctx, `SELECT external_id, COALESCE(seq, 0), COALESCE(queued_after_seq, -1) FROM session_events WHERE session_external_id = 'sesn_one' ORDER BY COALESCE(seq, queued_after_seq), seq IS NULL, id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got [][3]any
		for rows.Next() {
			var id string
			var seq, queued int64
			if err := rows.Scan(&id, &seq, &queued); err != nil {
				t.Fatal(err)
			}
			got = append(got, [3]any{id, seq, queued})
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		want := [][3]any{{"queued-before-all", int64(0), int64(0)}, {"first", int64(1), int64(-1)}, {"queued", int64(0), int64(1)}, {"late-processed", int64(2), int64(-1)}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("positions=%v want=%v", got, want)
		}
	})

	t.Run("session counters continue from the backfilled maximum", func(t *testing.T) {
		got := map[string]int64{}
		rows, err := database.QueryContext(ctx, `SELECT external_id, last_event_seq FROM sessions`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			var seq int64
			if err := rows.Scan(&id, &seq); err != nil {
				t.Fatal(err)
			}
			got[id] = seq
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if want := map[string]int64{"sesn_one": 2, "sesn_two": 1, "sesn_empty": 0}; !reflect.DeepEqual(got, want) {
			t.Fatalf("counters=%v want=%v", got, want)
		}
	})

	t.Run("down migration removes positions and up reapplies", func(t *testing.T) {
		if _, err := migrations.DownTo(ctx, 117); err != nil {
			t.Fatal(err)
		}
		var columns int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema() AND ((table_name = 'session_events' AND column_name IN ('seq', 'queued_after_seq')) OR (table_name = 'sessions' AND column_name = 'last_event_seq'))`).Scan(&columns); err != nil || columns != 0 {
			t.Fatalf("remaining columns=%d err=%v", columns, err)
		}
		if _, err := migrations.UpTo(ctx, 118); err != nil {
			t.Fatal(err)
		}
	})
}
