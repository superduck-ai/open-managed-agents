package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHTranscriptMapperContracts(t *testing.T) {
	scope := DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code", Epoch: 7}
	dialect := yourbatis.DialectPostgres
	tests := []mapperBuilderContract{
		{statement: dSHTranscriptMapperLockWorkerStatement, bound: buildDSHTranscriptMapperLockWorker(dialect, scope), wantID: "DSHTranscriptMapper.LockWorker", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"FOR UPDATE", "deleted_at IS NULL", "AS lease_valid"}},
		{statement: dSHTranscriptMapperFindStatement, bound: buildDSHTranscriptMapperFind(dialect, scope, "child"), wantID: "DSHTranscriptMapper.Find", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "dshSessionID"}, wantSQLFragments: []string{"FROM dsh_transcripts", "dsh_session_id = $4"}},
		{statement: dSHTranscriptMapperInsertStatement, bound: buildDSHTranscriptMapperInsert(dialect, dshTranscriptInsert{Scope: scope, DSHSessionID: "child", Header: []byte(`{}`)}), wantSensitiveArgumentNames: []string{"input.Header"}, wantID: "DSHTranscriptMapper.Insert", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"input.Scope.OrganizationUUID", "input.Scope.WorkspaceUUID", "input.Scope.CodeSessionUUID", "input.DSHSessionID", "input.ParentDSHSessionID", "input.Header"}, wantSQLFragments: []string{"ON CONFLICT (organization_uuid, workspace_uuid, code_session_uuid, dsh_session_id) DO NOTHING"}},
		{statement: dSHTranscriptMapperListStatement, bound: buildDSHTranscriptMapperList(dialect, scope, "child", 101), wantID: "DSHTranscriptMapper.List", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "after", "limit"}, wantSQLFragments: []string{"ORDER BY dsh_session_id LIMIT $5"}},
		{statement: dSHTranscriptMapperReadEventsStatement, bound: buildDSHTranscriptMapperReadEvents(dialect, scope, "transcript", 0, 200), wantID: "DSHTranscriptMapper.ReadEvents", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "transcriptUUID", "fromSeq", "limit"}, wantSQLFragments: []string{"seq >= $5", "ORDER BY seq LIMIT $6"}},
		{statement: dSHTranscriptMapperInsertEventStatement, bound: buildDSHTranscriptMapperInsertEvent(dialect, dshTranscriptEventInsert{Scope: scope, TranscriptUUID: "transcript", Seq: 0, Payload: []byte(`{}`)}), wantSensitiveArgumentNames: []string{"input.Payload"}, wantID: "DSHTranscriptMapper.InsertEvent", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"input.Scope.OrganizationUUID", "input.Scope.WorkspaceUUID", "input.Scope.CodeSessionUUID", "input.TranscriptUUID", "input.Seq", "input.Payload"}, wantSQLFragments: []string{"INSERT INTO dsh_transcript_events"}},
		{statement: dSHTranscriptMapperSetNextSeqStatement, bound: buildDSHTranscriptMapperSetNextSeq(dialect, scope, "transcript", 1), wantID: "DSHTranscriptMapper.SetNextSeq", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"nextSeq", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "transcriptUUID"}, wantSQLFragments: []string{"UPDATE dsh_transcripts SET next_seq = $1", "uuid = $5"}},
	}
	for _, test := range tests {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
}

func TestDSHTranscriptMapperReadFailures(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("query failed")
	executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure})
	_, _, err := NewDSHTranscriptMapper(executor).Find(ctx, DSHScope{}, "child")
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	executor = newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "dsh_session_id", "parent_dsh_session_id", "session_thread_id", "header", "next_seq"}})
	_, found, err := NewDSHTranscriptMapper(executor).Find(ctx, DSHScope{}, "missing")
	if err != nil || found {
		t.Fatalf("missing=%v,%v", found, err)
	}
}

func TestDSHTranscriptMapperScansNullableParentAndJSON(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "dsh_session_id", "parent_dsh_session_id", "session_thread_id", "header", "next_seq"}, rows: [][]driver.Value{{"uuid", "root", nil, nil, []byte(`{"version":4}`), int64(0)}}})
	row, found, err := NewDSHTranscriptMapper(executor).Find(context.Background(), DSHScope{}, "root")
	if err != nil || !found || row.ParentDSHSessionID != nil || row.SessionThreadID != nil || row.NextSeq != 0 || !reflect.DeepEqual(row.Header, []byte(`{"version":4}`)) {
		t.Fatalf("row=%+v,found=%v,error=%v", row, found, err)
	}
}
