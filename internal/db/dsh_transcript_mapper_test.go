package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHTranscriptMapperContracts(t *testing.T) {
	scope := DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code", Epoch: 7}
	dialect := yourbatis.DialectPostgres
	tests := []mapperBuilderContract{
		{statement: dSHTranscriptMapperLockWorkerStatement, bound: buildDSHTranscriptMapperLockWorker(dialect, scope), wantID: "DSHTranscriptMapper.LockWorker", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}, wantSQLFragments: []string{"FOR UPDATE", "deleted_at IS NULL", "AS lease_valid"}},
		{statement: dSHTranscriptMapperFindStatement, bound: buildDSHTranscriptMapperFind(dialect, scope, "child"), wantID: "DSHTranscriptMapper.Find", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "dshSessionID"}, wantSQLFragments: []string{"FROM dsh_transcripts", "dsh_session_id = $4"}},
		{statement: dSHTranscriptMapperInsertStatement, bound: buildDSHTranscriptMapperInsert(dialect, dshTranscriptInsert{Scope: scope, DSHSessionID: "child"}), wantID: "DSHTranscriptMapper.Insert", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"input.Scope.OrganizationUUID", "input.Scope.WorkspaceUUID", "input.Scope.CodeSessionUUID", "input.DSHSessionID", "input.ParentDSHSessionID"}, wantSQLFragments: []string{"ON CONFLICT (organization_uuid, workspace_uuid, code_session_uuid, dsh_session_id) DO NOTHING"}},
		{statement: dSHTranscriptMapperListStatement, bound: buildDSHTranscriptMapperList(dialect, scope, "child", 101), wantID: "DSHTranscriptMapper.List", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "after", "limit"}, wantSQLFragments: []string{"ORDER BY dsh_session_id LIMIT $5"}},
		{statement: dSHTranscriptMapperReadRecordsStatement, bound: buildDSHTranscriptMapperReadRecords(dialect, scope, "transcript", 0, 200), wantID: "DSHTranscriptMapper.ReadRecords", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "transcriptUUID", "fromOffset", "limit"}, wantSQLFragments: []string{"record_offset >= $5", "ORDER BY record_offset LIMIT $6"}},
		{statement: dSHTranscriptMapperReadRecordPageStatement, bound: buildDSHTranscriptMapperReadRecordPage(dialect, scope, "transcript", 0, 200, 4<<20), wantID: "DSHTranscriptMapper.ReadRecordPage", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "transcriptUUID", "fromOffset", "limit", "budget", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "transcriptUUID"}, wantSQLFragments: []string{"octet_length(payload) AS size", "ORDER BY record_offset LIMIT $6", "WHERE preceding < $7", "records.transcript_uuid = $11"}},
		{statement: dSHTranscriptMapperSetNextOffsetStatement, bound: buildDSHTranscriptMapperSetNextOffset(dialect, scope, "transcript", 1), wantID: "DSHTranscriptMapper.SetNextOffset", wantKind: yourbatis.StatementUpdate, wantArgumentNames: []string{"nextOffset", "scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID", "transcriptUUID"}, wantSQLFragments: []string{"UPDATE dsh_transcripts SET next_offset = $1", "uuid = $5"}},
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
	executor = newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "dsh_session_id", "parent_dsh_session_id", "session_thread_id", "next_offset"}})
	_, found, err := NewDSHTranscriptMapper(executor).Find(ctx, DSHScope{}, "missing")
	if err != nil || found {
		t.Fatalf("missing=%v,%v", found, err)
	}
}

func TestDSHTranscriptMapperScansNullableParentAndJSON(t *testing.T) {
	executor := newMapperTestExecutor(t, mapperTestResponse{columns: []string{"uuid", "dsh_session_id", "parent_dsh_session_id", "session_thread_id", "next_offset"}, rows: [][]driver.Value{{"uuid", "root", nil, nil, int64(0)}}})
	row, found, err := NewDSHTranscriptMapper(executor).Find(context.Background(), DSHScope{}, "root")
	if err != nil || !found || row.ParentDSHSessionID != nil || row.SessionThreadID != nil || row.NextOffset != 0 {
		t.Fatalf("row=%+v,found=%v,error=%v", row, found, err)
	}
}

func TestDSHTranscriptMapperBatchInsertBindings(t *testing.T) {
	for _, count := range []int{1, 2, 200} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			input := dshTranscriptRecordsInsert{Scope: DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code"}, TranscriptUUID: "transcript", Records: dshBatchRecords(10, count)}
			var names, sensitive []string
			var values []any
			for _, event := range input.Records {
				names = append(names, "input.Scope.OrganizationUUID", "input.Scope.WorkspaceUUID", "input.Scope.CodeSessionUUID", "input.TranscriptUUID", "event.Offset", "event.Payload")
				sensitive = append(sensitive, "event.Payload")
				values = append(values, "org", "workspace", "code", "transcript", event.Offset, event.Payload)
			}
			bound := buildDSHTranscriptMapperInsertRecords(yourbatis.DialectPostgres, input)
			assertMapperBuilderContract(t, mapperBuilderContract{statement: dSHTranscriptMapperInsertRecordsStatement, bound: bound, wantID: "DSHTranscriptMapper.InsertRecords", wantKind: yourbatis.StatementInsert, wantArgumentNames: names, wantSensitiveArgumentNames: sensitive, wantSQLFragments: []string{"INSERT INTO dsh_transcript_records", fmt.Sprintf("$%d", count*6)}})
			if strings.Count(bound.SQL, "INSERT INTO") != 1 || strings.Count(bound.SQL, "(") != count+1 {
				t.Fatalf("insert does not contain one statement and %d tuples: %s", count, bound.SQL)
			}
			executor := newMapperTestExecutor(t, mapperTestResponse{rowsAffected: int64(count)})
			rows, err := NewDSHTranscriptMapper(executor).InsertRecords(t.Context(), input)
			if err != nil || rows != int64(count) {
				t.Fatalf("rows=%d error=%v", rows, err)
			}
			assertMapperTestExecution(t, executor, "DSHTranscriptMapper.InsertRecords", yourbatis.StatementInsert, values)
		})
	}
}
