package db

import (
	"github.com/superduck-ai/yourbatis"
	"testing"
	"time"
)

func TestSessionPublicationOrderMapperScopesAndFrozenCursor(t *testing.T) {
	at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionEventMapperListPageStatement,
		bound:     buildSessionEventMapperListPage(yourbatis.DialectPostgres, sessionEventPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: "thread", ProcessedOnly: true, Cursor: &SessionEventPageCursor{ExternalID: "queued", OrderingAt: &at}, FetchLimit: 21}),
		wantID:    "SessionEventMapper.ListPage", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.ThreadExternalID", "params.Cursor.OrderingAt", "params.WorkspaceUUID", "params.SessionExternalID", "params.Cursor.ExternalID", "params.FetchLimit"},
		wantSQLFragments:  []string{"processed_at IS NOT NULL", "thread_external_id = $3", "AND (processed_at, id) >", "CAST($4 AS timestamptz)", "ORDER BY session_events.processed_at ASC, id ASC"},
		wantSQLAbsent:     []string{"ORDER BY COALESCE", "AND (COALESCE(processed_at, created_at), id)"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionEventMapperListPageStatement,
		bound:     buildSessionEventMapperListPage(yourbatis.DialectPostgres, sessionEventPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", Cursor: &SessionEventPageCursor{ExternalID: "event"}, FetchLimit: 21}),
		wantID:    "SessionEventMapper.ListPage", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.WorkspaceUUID", "params.SessionExternalID", "params.Cursor.ExternalID", "params.FetchLimit"},
		wantSQLFragments:  []string{"AND (COALESCE(processed_at, created_at), id) >", "ORDER BY COALESCE(session_events.processed_at, session_events.created_at)"},
		wantSQLAbsent:     []string{"processed_at IS NOT NULL", "ORDER BY session_events.processed_at"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: sessionEventMapperLatestProcessedStatement, bound: buildSessionEventMapperLatestProcessed(yourbatis.DialectPostgres, "workspace", "session"), wantID: "SessionEventMapper.LatestProcessed", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"workspaceUUID", "sessionExternalID"}, wantSQLFragments: []string{"workspace_uuid = $1", "session_external_id = $2", "processed_at IS NOT NULL", "ORDER BY processed_at DESC, id DESC LIMIT 1"},
	})
}
