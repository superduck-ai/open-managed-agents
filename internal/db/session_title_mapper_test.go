package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestSessionTitleMapperContracts(t *testing.T) {
	for _, tc := range []mapperBuilderContract{
		{statement: sessionTitleJobMapperBeginEnqueueStatement,
			bound:  buildSessionTitleJobMapperBeginEnqueue(yourbatis.DialectPostgres),
			wantID: "SessionTitleJobMapper.BeginEnqueue", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{}, wantSQLFragments: []string{"SAVEPOINT session_title_enqueue"}},
		{statement: sessionTitleJobMapperRollbackEnqueueStatement,
			bound:  buildSessionTitleJobMapperRollbackEnqueue(yourbatis.DialectPostgres),
			wantID: "SessionTitleJobMapper.RollbackEnqueue", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{}, wantSQLFragments: []string{"ROLLBACK TO SAVEPOINT session_title_enqueue"}},
		{statement: sessionTitleJobMapperReleaseEnqueueStatement,
			bound:  buildSessionTitleJobMapperReleaseEnqueue(yourbatis.DialectPostgres),
			wantID: "SessionTitleJobMapper.ReleaseEnqueue", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{}, wantSQLFragments: []string{"RELEASE SAVEPOINT session_title_enqueue"}},
		{statement: sessionTitleJobMapperEnqueueStatement,
			bound:  buildSessionTitleJobMapperEnqueue(yourbatis.DialectPostgres, "org", "ws", "session"),
			wantID: "SessionTitleJobMapper.Enqueue", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "sessionUUID"},
			wantSQLFragments:  []string{"s.organization_uuid = $1", "s.workspace_uuid = $2", "s.uuid = $3", "CASE WHEN NULLIF(s.title, '') IS NULL THEN 'pending' ELSE 'completed' END", "ON CONFLICT (external_id) DO NOTHING"}},
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
		{statement: sessionEventMapperListTitleInputsStatement,
			bound:  buildSessionEventMapperListTitleInputs(yourbatis.DialectPostgres, "ws", "session", "previous-event"),
			wantID: "SessionEventMapper.ListTitleInputs", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "sessionUUID", "workspaceUUID", "sessionUUID", "workspaceUUID", "sessionUUID", "afterUUID"},
			wantSQLFragments:  []string{"event_type = 'user.message'", "parent_thread_uuid IS NULL", "uuid = $7", "ORDER BY id ASC LIMIT 16"}},
	} {
		t.Run(tc.wantID, func(t *testing.T) { assertMapperBuilderContract(t, tc) })
	}
}
