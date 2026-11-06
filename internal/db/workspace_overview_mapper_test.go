package db

import (
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestWorkspaceOverviewMapper(t *testing.T) {
	q := WorkspaceOverviewQuery{OrganizationUUID: "org", WorkspaceUUID: "workspace", Timezone: "Asia/Shanghai", Start: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)}
	t.Run("propagates errors", func(t *testing.T) {
		want := errors.New("database failed")
		ex := newMapperTestExecutor(t, mapperTestResponse{queryErr: want})
		if _, err := NewWorkspaceOverviewMapper(ex).Aggregate(t.Context(), q); !errors.Is(err, want) {
			t.Fatal(err)
		}
	})
	t.Run("binds scope and dates and scans aggregate", func(t *testing.T) {
		ex := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"date", "agent_id", "agent_name", "session_count", "failed_session_count", "last_run_at"}, rows: [][]driver.Value{{"2026-11-06", "agent_a", "A", int64(3), int64(1), q.End}}})
		rows, err := NewWorkspaceOverviewMapper(ex).Aggregate(t.Context(), q)
		if err != nil || len(rows) != 1 || rows[0].FailedSessionCount != 1 || rows[0].SessionCount != 3 || !rows[0].LastRunAt.Equal(q.End) {
			t.Fatalf("%+v %v", rows, err)
		}
		assertMapperTestExecution(t, ex, "WorkspaceOverviewMapper.Aggregate", yourbatis.StatementSelect, []any{q.Timezone, q.OrganizationUUID, q.WorkspaceUUID, q.Start, q.End, q.WorkspaceUUID, q.OrganizationUUID, q.WorkspaceUUID, q.Start, q.End}, "s.organization_uuid = $7", "s.workspace_uuid = $8", "s.deleted_at IS NULL", "COUNT(*) FILTER (WHERE EXISTS", "'exhausted', 'terminal'", "ORDER BY date, s.agent_external_id")
	})
	t.Run("counts only the scoped live agents", func(t *testing.T) {
		ex := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"count"}, rows: [][]driver.Value{{int64(2)}}})
		count, err := NewWorkspaceOverviewMapper(ex).CountAgents(t.Context(), q.OrganizationUUID, q.WorkspaceUUID)
		if err != nil || count != 2 {
			t.Fatalf("%d %v", count, err)
		}
		assertMapperTestExecution(t, ex, "WorkspaceOverviewMapper.CountAgents", yourbatis.StatementSelect, []any{q.WorkspaceUUID, q.OrganizationUUID}, "a.archived_at IS NULL", "w.organization_uuid = $2")
	})
}
