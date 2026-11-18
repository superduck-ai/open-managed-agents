package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionTitleMapperContracts(t *testing.T) {
	for _, tc := range []mapperBuilderContract{
		{statement: sessionTitleJobMapperEnqueueStatement,
			bound:  buildSessionTitleJobMapperEnqueue(yourbatis.DialectPostgres, "org", "ws", "session", "first input"),
			wantID: "SessionTitleJobMapper.Enqueue", wantKind: yourbatis.StatementInsert,
			wantArgumentNames:          []string{"input", "organizationUUID", "workspaceUUID", "sessionUUID"},
			wantSensitiveArgumentNames: []string{"input"},
			wantSQLFragments:           []string{"s.organization_uuid = $2", "s.workspace_uuid = $3", "s.uuid = $4", "CASE WHEN NULLIF(s.title, '') IS NULL THEN 'pending' ELSE 'completed' END", "ON CONFLICT (external_id) DO NOTHING"}},
		{statement: sessionTitleJobMapperLeaseStatement,
			bound:  buildSessionTitleJobMapperLease(yourbatis.DialectPostgres, "worker", 4),
			wantID: "SessionTitleJobMapper.Lease", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"limit", "workerID"},
			wantSQLFragments:  []string{"FOR UPDATE SKIP LOCKED", "locked_until < now()", "type = 'session_title'", "LIMIT $1"}},
		{statement: sessionTitleJobMapperCompleteStatement,
			bound:  buildSessionTitleJobMapperComplete(yourbatis.DialectPostgres, sessionTitleCompletionParams{WorkspaceUUID: "ws", JobUUID: "job", WorkerID: "worker", Title: "Title"}),
			wantID: "SessionTitleJobMapper.Complete", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames:          []string{"params.WorkspaceUUID", "params.JobUUID", "params.WorkerID", "params.Title", "params.Title"},
			wantSensitiveArgumentNames: []string{"params.Title", "params.Title"},
			wantSQLFragments:           []string{"workspace_uuid = $1", "locked_by = $3", "locked_until > now()", "NULLIF(s.title, '') IS NULL", "s.workspace_uuid = j.workspace_uuid", "payload = payload - 'input'"}},
	} {
		t.Run(tc.wantID, func(t *testing.T) { assertMapperBuilderContract(t, tc) })
	}
}
