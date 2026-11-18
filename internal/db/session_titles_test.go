package db

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionTitleMapperBindings(t *testing.T) {
	const workspaceID = "00000000-0000-0000-0000-000000000001"
	const sessionID = "sesn_title"
	const title = "Diagnose session latency"
	at := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	first := buildSessionEventMapperFirstUserMessageUUID(yourbatis.DialectPostgres, workspaceID, sessionID)
	if sessionEventMapperFirstUserMessageUUIDStatement.Kind != yourbatis.StatementSelect ||
		!slices.Equal(first.Values(), []any{workspaceID, sessionID, workspaceID, sessionID}) ||
		!strings.Contains(first.SQL, "workspace_uuid = $1 AND session_external_id = $2") ||
		!strings.Contains(first.SQL, "parent_thread_uuid IS NULL AND deleted_at IS NULL") ||
		!strings.Contains(first.SQL, "ORDER BY id ASC LIMIT 1") {
		t.Fatalf("unexpected first message query: %+v", first)
	}
	update := buildSessionMapperSetGeneratedTitle(yourbatis.DialectPostgres, workspaceID, sessionID, title, at)
	if sessionMapperSetGeneratedTitleStatement.Kind != yourbatis.StatementUpdate ||
		!slices.Equal(update.Values(), []any{title, at, workspaceID, sessionID}) ||
		!update.Args[0].Sensitive ||
		!strings.Contains(update.SQL, "workspace_uuid = $3 AND external_id = $4") ||
		!strings.Contains(update.SQL, "COALESCE(title, '') = '' AND archived_at IS NULL AND deleted_at IS NULL") {
		t.Fatalf("unexpected title update: %+v", update)
	}
}

func TestSessionTitleSavepointBindings(t *testing.T) {
	for _, tc := range []struct {
		statement yourbatis.Statement
		bound     yourbatis.BoundSQL
		query     string
	}{
		{sessionEventMapperBeginTitleEnqueueStatement, buildSessionEventMapperBeginTitleEnqueue(yourbatis.DialectPostgres), "SAVEPOINT session_title_enqueue"},
		{sessionEventMapperRollbackTitleEnqueueStatement, buildSessionEventMapperRollbackTitleEnqueue(yourbatis.DialectPostgres), "ROLLBACK TO SAVEPOINT session_title_enqueue"},
		{sessionEventMapperReleaseTitleEnqueueStatement, buildSessionEventMapperReleaseTitleEnqueue(yourbatis.DialectPostgres), "RELEASE SAVEPOINT session_title_enqueue"},
	} {
		if tc.statement.Kind != yourbatis.StatementUpdate || tc.bound.SQL != tc.query || len(tc.bound.Args) != 0 {
			t.Fatalf("unexpected savepoint statement %v: %+v", tc.statement, tc.bound)
		}
	}
}
