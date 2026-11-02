package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionTrajectorySummaryQuery(t *testing.T) {
	ctx := context.Background()
	t.Run("propagates query failure", func(t *testing.T) {
		expected := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: expected})
		if _, err := NewSessionMapper(executor).ListTrajectorySummaries(ctx, "workspace", []string{"sesn_a"}); !errors.Is(err, expected) {
			t.Fatalf("error = %v, want query failure", err)
		}
	})
	t.Run("empty IDs need no database", func(t *testing.T) {
		summaries, err := (&DB{}).ListSessionTrajectorySummaries(ctx, "workspace", nil)
		if err != nil || len(summaries) != 0 {
			t.Fatalf("summaries = %v, error = %v", summaries, err)
		}
	})
	t.Run("binds workspace and IDs and scans nullable titles", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"external_id", "agent_external_id", "agent_version", "title", "status", "subagent_count"},
			rows: [][]driver.Value{
				{"sesn_a", "agent_a", int64(2), "Title", "idle", int64(3)},
				{"sesn_b", "agent_b", int64(1), nil, "running", int64(0)},
			},
		})
		rows, err := NewSessionMapper(executor).ListTrajectorySummaries(ctx, "workspace", []string{"sesn_a", "sesn_b"})
		if err != nil || len(rows) != 2 {
			t.Fatalf("rows = %v, error = %v", rows, err)
		}
		if rows[0].Title == nil || *rows[0].Title != "Title" || rows[0].SubagentCount != 3 || rows[1].Title != nil {
			t.Fatalf("scanned rows = %+v", rows)
		}
		assertMapperTestExecution(t, executor, "SessionMapper.ListTrajectorySummaries", yourbatis.StatementSelect,
			[]any{"workspace", "sesn_a", "sesn_b"},
			"s.workspace_uuid = $1", "s.deleted_at IS NULL", "s.external_id IN ( $2 , $3 )",
			"t.parent_thread_uuid IS NOT NULL", "t.deleted_at IS NULL")
	})
}

func TestCodeSessionSegmentQuery(t *testing.T) {
	ctx := context.Background()
	t.Run("propagates query failure", func(t *testing.T) {
		expected := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: expected})
		if _, err := NewCodeSessionMapper(executor).ListSegmentsBySessionExternalID(ctx, "workspace", "sesn_a"); !errors.Is(err, expected) {
			t.Fatalf("error = %v, want query failure", err)
		}
	})
	t.Run("binds workspace and session in creation order", func(t *testing.T) {
		created := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"external_id", "created_at"},
			rows:    [][]driver.Value{{"cse_old", created}, {"cse_new", created.Add(time.Hour)}},
		})
		rows, err := NewCodeSessionMapper(executor).ListSegmentsBySessionExternalID(ctx, "workspace", "sesn_a")
		if err != nil || len(rows) != 2 || rows[0].ExternalID != "cse_old" || !rows[1].CreatedAt.Equal(created.Add(time.Hour)) {
			t.Fatalf("rows = %+v, error = %v", rows, err)
		}
		assertMapperTestExecution(t, executor, "CodeSessionMapper.ListSegmentsBySessionExternalID", yourbatis.StatementSelect,
			[]any{"workspace", "sesn_a"},
			"workspace_uuid = $1", "session_external_id = $2", "deleted_at IS NULL", "ORDER BY created_at ASC, uuid ASC")
	})
}

func TestSubagentThreadNameQuery(t *testing.T) {
	ctx := context.Background()
	t.Run("propagates query failure", func(t *testing.T) {
		expected := errors.New("query failed")
		executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: expected})
		if _, err := NewSessionThreadMapper(executor).ListSubagentNames(ctx, "workspace", []string{"sthr_a"}); !errors.Is(err, expected) {
			t.Fatalf("error = %v, want query failure", err)
		}
	})
	t.Run("empty IDs need no database", func(t *testing.T) {
		names, err := (&DB{}).ListSubagentThreadNames(ctx, "workspace", nil)
		if err != nil || len(names) != 0 {
			t.Fatalf("names = %v, error = %v", names, err)
		}
	})
	t.Run("binds workspace and child thread IDs", func(t *testing.T) {
		executor := newMapperTestExecutor(t, mapperTestResponse{
			columns: []string{"external_id", "name"},
			rows:    [][]driver.Value{{"sthr_a", "reviewer"}, {"sthr_b", ""}},
		})
		rows, err := NewSessionThreadMapper(executor).ListSubagentNames(ctx, "workspace", []string{"sthr_a", "sthr_b"})
		if err != nil || len(rows) != 2 || rows[0].Name != "reviewer" {
			t.Fatalf("rows = %+v, error = %v", rows, err)
		}
		assertMapperTestExecution(t, executor, "SessionThreadMapper.ListSubagentNames", yourbatis.StatementSelect,
			[]any{"workspace", "sthr_a", "sthr_b"},
			"workspace_uuid = $1", "parent_thread_uuid IS NOT NULL", "COALESCE(agent_snapshot->>'name', '') AS name")
	})
}
