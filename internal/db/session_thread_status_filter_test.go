package db

import (
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionThreadPageStatusFilterIsBoundBeforePagination(t *testing.T) {
	now := time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
	params := sessionThreadPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", Statuses: []string{"idle", "running"}, FetchLimit: 2, Cursor: &SessionThreadPageCursor{CreatedAt: now, UUID: "thread-uuid"}}
	bound := buildSessionThreadMapperListPage(yourbatis.DialectPostgres, params)
	sql := strings.Join(strings.Fields(bound.SQL), " ")
	filter, cursor, limit := strings.Index(sql, "status IN ( $3 , $4 )"), strings.Index(sql, "(created_at, uuid) < ($5, $6)"), strings.Index(sql, "LIMIT $7")
	if filter < 0 || cursor <= filter || limit <= cursor {
		t.Fatalf("status OR filter must precede keyset and page limit: %s", sql)
	}
	executor := newMapperTestExecutor(t, mapperTestResponse{})
	if _, err := NewSessionThreadMapper(executor).ListPage(t.Context(), params); err != nil {
		t.Fatal(err)
	}
	assertMapperTestExecution(t, executor, "SessionThreadMapper.ListPage", yourbatis.StatementSelect, []any{"workspace", "session", "idle", "running", now, "thread-uuid", 2})
}
func TestSessionThreadPageOmittedStatusesRemainUnfiltered(t *testing.T) {
	bound := buildSessionThreadMapperListPage(yourbatis.DialectPostgres, sessionThreadPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", FetchLimit: 2})
	sql := strings.Join(strings.Fields(bound.SQL), " ")
	if strings.Contains(sql, "status IN") || !strings.Contains(sql, "deleted_at IS NULL") || !strings.Contains(sql, "LIMIT $3") {
		t.Fatalf("unfiltered thread listing changed: %s", sql)
	}
}
func TestSessionThreadPageSingleStatusIsAParameter(t *testing.T) {
	for _, status := range []string{"terminated", "idle') OR true --"} {
		bound := buildSessionThreadMapperListPage(yourbatis.DialectPostgres, sessionThreadPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", Statuses: []string{status}, FetchLimit: 2})
		sql := strings.Join(strings.Fields(bound.SQL), " ")
		if !strings.Contains(sql, "status IN ( $3 )") || strings.Contains(sql, status) {
			t.Fatalf("status not safely bound: %s", sql)
		}
		values := bound.Values()
		if len(values) != 4 || values[2] != status {
			t.Fatalf("status binding lost: %#v", values)
		}
	}
}
