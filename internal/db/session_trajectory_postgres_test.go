//go:build integration

package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestSessionTrajectoryQueriesPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	database, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.mapperDB.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`CREATE TEMP TABLE sessions(uuid uuid PRIMARY KEY,external_id text,workspace_uuid uuid,agent_external_id text,agent_version int,title text,status text,deleted_at timestamptz) ON COMMIT DROP`,
		`CREATE TEMP TABLE session_threads(uuid uuid,workspace_uuid uuid,session_uuid uuid,parent_thread_uuid uuid,deleted_at timestamptz,external_id text,agent_snapshot jsonb) ON COMMIT DROP`,
		`CREATE TEMP TABLE code_sessions(uuid uuid,external_id text,workspace_uuid uuid,session_external_id text,created_at timestamptz,deleted_at timestamptz) ON COMMIT DROP`,
	} {
		execMapperFixtureSQL(t, ctx, tx, statement)
	}
	workspace, other := "97000000-0000-0000-0000-000000000001", "97000000-0000-0000-0000-000000000002"
	live, removed, foreign := "97000000-0000-0000-0000-000000000011", "97000000-0000-0000-0000-000000000012", "97000000-0000-0000-0000-000000000013"
	untitled := "97000000-0000-0000-0000-000000000014"
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO sessions VALUES
		($1,'sesn_live',$5,'agent_a',3,'Live','idle',NULL),
		($2,'sesn_removed',$5,'agent_a',1,NULL,'idle',NOW()),
		($3,'sesn_live',$6,'agent_b',1,NULL,'idle',NULL),
		($4,'sesn_untitled',$5,'agent_b',1,NULL,'running',NULL)`, live, removed, foreign, untitled, workspace, other)
	primary := "97000000-0000-0000-0000-000000000021"
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_threads VALUES
		($1,$3,$2,NULL,NULL),
		(gen_random_uuid(),$3,$2,$1,NULL),
		(gen_random_uuid(),$3,$2,$1,NULL),
		(gen_random_uuid(),$3,$2,$1,NOW()),
		(gen_random_uuid(),$4,$2,$1,NULL)`, primary, live, workspace, other)
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO session_threads(uuid,workspace_uuid,session_uuid,parent_thread_uuid,external_id,agent_snapshot) VALUES
		(gen_random_uuid(),$1,$2,$3,'sthr_named','{"name":"reviewer"}'),
		(gen_random_uuid(),$1,$2,$3,'sthr_unnamed','{}'),
		(gen_random_uuid(),$1,$2,NULL,'sthr_root','{"name":"root"}'),
		(gen_random_uuid(),$4,$2,$3,'sthr_foreign','{"name":"foreign"}')`, workspace, "97000000-0000-0000-0000-000000000031", primary, other)
	names, err := NewSessionThreadMapper(tx).ListSubagentNames(ctx, workspace, []string{"sthr_named", "sthr_unnamed", "sthr_root", "sthr_foreign"})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 {
		t.Fatalf("root or foreign threads leaked: %+v", names)
	}
	for _, row := range names {
		if (row.ExternalID == "sthr_named" && row.Name != "reviewer") || (row.ExternalID == "sthr_unnamed" && row.Name != "") {
			t.Fatalf("names = %+v", names)
		}
	}
	base := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	execMapperFixtureSQL(t, ctx, tx, `INSERT INTO code_sessions VALUES
		(gen_random_uuid(),'cse_new',$1,'sesn_live',$3,NULL),
		(gen_random_uuid(),'cse_old',$1,'sesn_live',$4,NULL),
		(gen_random_uuid(),'cse_deleted',$1,'sesn_live',$5,NOW()),
		(gen_random_uuid(),'cse_foreign',$2,'sesn_live',$4,NULL)`, workspace, other, base.Add(time.Hour), base, base.Add(2*time.Hour))

	rows, err := NewSessionMapper(tx).ListTrajectorySummaries(ctx, workspace, []string{"sesn_live", "sesn_removed", "sesn_untitled", "sesn_missing"})
	if err != nil {
		t.Fatal(err)
	}
	summaries := map[string]sessionTrajectoryRow{}
	for _, row := range rows {
		summaries[row.ExternalID] = row
	}
	if len(summaries) != 2 {
		t.Fatalf("deleted, missing or foreign sessions leaked: %+v", rows)
	}
	got := summaries["sesn_live"]
	if got.AgentExternalID != "agent_a" || got.AgentVersion != 3 || got.Title == nil || *got.Title != "Live" || got.SubagentCount != 2 {
		t.Fatalf("live summary = %+v", got)
	}
	if untitledRow := summaries["sesn_untitled"]; untitledRow.Title != nil || untitledRow.Status != "running" || untitledRow.SubagentCount != 0 {
		t.Fatalf("untitled summary = %+v", untitledRow)
	}

	segments, err := NewCodeSessionMapper(tx).ListSegmentsBySessionExternalID(ctx, workspace, "sesn_live")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || segments[0].ExternalID != "cse_old" || segments[1].ExternalID != "cse_new" || !segments[0].CreatedAt.Equal(base) {
		t.Fatalf("segments = %+v", segments)
	}
}
