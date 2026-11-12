package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionEventOrderMapperContracts(t *testing.T) {
	after, through := int64(4), int64(9)
	write := sessionEventWriteParams{UUID: "event", ExternalID: "sevt", WorkspaceUUID: "workspace", SessionUUID: "session"}
	processed := write
	processed.ProcessedAt = &processedAtFixture
	for _, test := range []mapperBuilderContract{
		{
			statement: sessionEventMapperInsertStatement, bound: buildSessionEventMapperInsert(yourbatis.DialectPostgres, write),
			wantID: "SessionEventMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantSensitiveArgumentNames: []string{"params.Payload"},
			wantArgumentNames:          []string{"params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.SessionUUID", "params.SessionExternalID", "params.ThreadUUID", "params.ThreadExternalID", "params.EventType", "params.Payload", "params.PayloadBlobUUID", "params.ToolUseID", "params.ProcessedAt", "params.CreatedAt", "params.WorkspaceUUID", "params.SessionUUID"},
			wantSQLFragments:           []string{"NULL, (SELECT last_event_seq FROM sessions WHERE", "sessions.workspace_uuid = $15 AND sessions.uuid = $16", "seq, queued_after_seq"},
			wantSQLAbsent:              []string{"UPDATE sessions"},
		},
		{
			statement: sessionEventMapperInsertStatement, bound: buildSessionEventMapperInsert(yourbatis.DialectPostgres, processed),
			wantID: "SessionEventMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantSensitiveArgumentNames: []string{"params.Payload"},
			wantArgumentNames:          []string{"params.WorkspaceUUID", "params.SessionUUID", "params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.SessionUUID", "params.SessionExternalID", "params.ThreadUUID", "params.ThreadExternalID", "params.EventType", "params.Payload", "params.PayloadBlobUUID", "params.ToolUseID", "params.ProcessedAt", "params.CreatedAt"},
			wantSQLFragments:           []string{"WITH position AS (", "UPDATE sessions SET last_event_seq = last_event_seq + 1 WHERE sessions.workspace_uuid = $1 AND sessions.uuid = $2", "(SELECT last_event_seq FROM position), NULL"},
		},
		{
			statement: sessionEventMapperInsertIfAbsentStatement, bound: buildSessionEventMapperInsertIfAbsent(yourbatis.DialectPostgres, processed),
			wantID: "SessionEventMapper.InsertIfAbsent", wantKind: yourbatis.StatementSelect,
			wantSensitiveArgumentNames: []string{"params.Payload"},
			wantArgumentNames:          []string{"params.WorkspaceUUID", "params.SessionUUID", "params.WorkspaceUUID", "params.ExternalID", "params.UUID", "params.ExternalID", "params.OrganizationUUID", "params.WorkspaceUUID", "params.SessionUUID", "params.SessionExternalID", "params.ThreadUUID", "params.ThreadExternalID", "params.EventType", "params.Payload", "params.PayloadBlobUUID", "params.ToolUseID", "params.ProcessedAt", "params.CreatedAt"},
			wantSQLFragments:           []string{"AND NOT EXISTS (", "external_id = $4", "ON CONFLICT (workspace_uuid, external_id) DO NOTHING"},
		},
		{
			statement: sessionEventMapperMarkInputProcessedStatement, bound: buildSessionEventMapperMarkInputProcessed(yourbatis.DialectPostgres, "workspace", "session", "input", processedAtFixture),
			wantID: "SessionEventMapper.MarkInputProcessed", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "sessionExternalID", "eventExternalID", "workspaceUUID", "sessionExternalID", "processedAt"},
			wantSQLFragments:  []string{"EXISTS (SELECT 1 FROM target)", "seq = position.last_event_seq", "session_events.uuid IN (SELECT uuid FROM target)"},
		},
		{
			statement: sessionEventMapperListPageStatement,
			bound:     buildSessionEventMapperListPage(yourbatis.DialectPostgres, sessionEventPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", ThreadExternalID: "thread", ProcessedOnly: true, AfterSeq: &after, ThroughSeq: &through, Cursor: &SessionEventPageCursor{ExternalID: "input", AtQueuedPosition: true}, FetchLimit: 21}),
			wantID:    "SessionEventMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.ThreadExternalID", "params.AfterSeq", "params.ThroughSeq", "params.WorkspaceUUID", "params.SessionExternalID", "params.Cursor.ExternalID", "params.FetchLimit"},
			wantSQLFragments:  []string{"AND seq IS NOT NULL", "COALESCE(seq, queued_after_seq) > $4", "COALESCE(seq, queued_after_seq) <= $5", "AND (COALESCE(seq, queued_after_seq), seq IS NULL, id)", "COALESCE(queued_after_seq, seq - 1), true", "ORDER BY COALESCE(seq, queued_after_seq)  ASC , (seq IS NULL)  ASC , id  ASC"},
			wantSQLAbsent:     []string{"COALESCE(processed_at, created_at)", "CAST("},
		},
		{
			statement: sessionEventMapperListPageStatement,
			bound:     buildSessionEventMapperListPage(yourbatis.DialectPostgres, sessionEventPageMapperParams{WorkspaceUUID: "workspace", SessionExternalID: "session", Cursor: &SessionEventPageCursor{ExternalID: "event"}, Descending: true, FetchLimit: 21}),
			wantID:    "SessionEventMapper.ListPage", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"params.WorkspaceUUID", "params.SessionExternalID", "params.WorkspaceUUID", "params.SessionExternalID", "params.Cursor.ExternalID", "params.FetchLimit"},
			wantSQLFragments:  []string{"SELECT  COALESCE(seq, queued_after_seq), seq IS NULL , id", "ORDER BY COALESCE(seq, queued_after_seq)  DESC , (seq IS NULL)  DESC , id  DESC"},
			wantSQLAbsent:     []string{"seq IS NOT NULL", "queued_after_seq, seq - 1"},
		},
		{
			statement: sessionMapperLastEventSeqStatement, bound: buildSessionMapperLastEventSeq(yourbatis.DialectPostgres, "workspace", "session"),
			wantID: "SessionMapper.LastEventSeq", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "sessionExternalID"}, wantSQLFragments: []string{"SELECT last_event_seq FROM sessions", "deleted_at IS NULL"},
		},
	} {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
}

var processedAtFixture = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)
