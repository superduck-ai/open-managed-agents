package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestDSHExecutionMapperContracts(t *testing.T) {
	scope := DSHScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "code"}
	dialect := yourbatis.DialectPostgres
	scopeNames := []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.CodeSessionUUID"}
	names := func(tail ...string) []string { return append(append([]string{}, scopeNames...), tail...) }
	tests := []mapperBuilderContract{
		{statement: dSHExecutionMapperFindCodeStatement, bound: buildDSHExecutionMapperFindCode(dialect, "org", "workspace", "session"), wantID: "DSHExecutionMapper.FindCode", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"organizationUUID", "workspaceUUID", "sessionUUID"}, wantSQLFragments: []string{"worker_protocol}' = 'dsr1'"}},
		{statement: dSHExecutionMapperInsertExecutionStatement, bound: buildDSHExecutionMapperInsertExecution(dialect, scope, "input", "thread", time.Time{}), wantID: "DSHExecutionMapper.InsertExecution", wantKind: yourbatis.StatementInsert, wantArgumentNames: names("inputID", "threadID", "acceptedAt"), wantSQLFragments: []string{"ON CONFLICT"}},
		{statement: dSHExecutionMapperExecutionStatement, bound: buildDSHExecutionMapperExecution(dialect, scope, "input"), wantID: "DSHExecutionMapper.Execution", wantKind: yourbatis.StatementSelect, wantArgumentNames: names("inputID")},
		{statement: dSHExecutionMapperSummaryStatement, bound: buildDSHExecutionMapperSummary(dialect, scope), wantID: "DSHExecutionMapper.Summary", wantKind: yourbatis.StatementSelect, wantArgumentNames: names(append(scopeNames, scopeNames...)...), wantSQLFragments: []string{"ORDER BY accepted_at DESC, input_id DESC LIMIT 1", "AS has_eligible_accepted"}},
		{statement: dSHExecutionMapperStartExecutionStatement, bound: buildDSHExecutionMapperStartExecution(dialect, scope, "input"), wantID: "DSHExecutionMapper.StartExecution", wantKind: yourbatis.StatementUpdate, wantArgumentNames: names("inputID"), wantSQLFragments: []string{"status = 'accepted'"}},
		{statement: dSHExecutionMapperFinishExecutionStatement, bound: buildDSHExecutionMapperFinishExecution(dialect, scope, "input", "success", nil), wantID: "DSHExecutionMapper.FinishExecution", wantKind: yourbatis.StatementUpdate, wantArgumentNames: append([]string{"result", "failureCode"}, names("inputID")...), wantSQLFragments: []string{"status = 'running'"}},
		{statement: dSHExecutionMapperPermissionStatement, bound: buildDSHExecutionMapperPermission(dialect, scope, "request"), wantID: "DSHExecutionMapper.Permission", wantKind: yourbatis.StatementSelect, wantArgumentNames: names("requestID")},
		{statement: dSHExecutionMapperPermissionsStatement, bound: buildDSHExecutionMapperPermissions(dialect, scope), wantID: "DSHExecutionMapper.Permissions", wantKind: yourbatis.StatementSelect, wantArgumentNames: names()},
		{statement: dSHExecutionMapperInsertPermissionStatement, bound: buildDSHExecutionMapperInsertPermission(dialect, dshPermissionInsert{Scope: scope}), wantID: "DSHExecutionMapper.InsertPermission", wantKind: yourbatis.StatementInsert, wantArgumentNames: []string{"input.Scope.OrganizationUUID", "input.Scope.WorkspaceUUID", "input.Scope.CodeSessionUUID", "input.RequestID", "input.InputID", "input.DSHSessionID", "input.ThreadID", "input.ToolUseID", "input.Turn", "input.Tool", "input.Input", "input.Available", "input.Scope.Epoch", "input.PublicEventID", "input.State", "input.Decision"}, wantSensitiveArgumentNames: []string{"input.Tool", "input.Input", "input.Decision"}},
		{statement: dSHExecutionMapperSetPermissionStatement, bound: buildDSHExecutionMapperSetPermission(dialect, scope, "request", "resolved", nil), wantID: "DSHExecutionMapper.SetPermission", wantKind: yourbatis.StatementUpdate, wantArgumentNames: append([]string{"state", "decision"}, names("requestID")...), wantSensitiveArgumentNames: []string{"decision"}, wantSQLFragments: []string{"state = 'pending'"}},
		{statement: dSHExecutionMapperReceiptStatement, bound: buildDSHExecutionMapperReceipt(dialect, scope, "event"), wantID: "DSHExecutionMapper.Receipt", wantKind: yourbatis.StatementSelect, wantArgumentNames: names("eventID")},
		{statement: dSHExecutionMapperInsertReceiptStatement, bound: buildDSHExecutionMapperInsertReceipt(dialect, scope, "event", "digest"), wantID: "DSHExecutionMapper.InsertReceipt", wantKind: yourbatis.StatementInsert, wantArgumentNames: names("eventID", "payloadDigest"), wantSensitiveArgumentNames: []string{"payloadDigest"}},
		{statement: dSHExecutionMapperBindTranscriptStatement, bound: buildDSHExecutionMapperBindTranscript(dialect, scope, "native", "thread"), wantID: "DSHExecutionMapper.BindTranscript", wantKind: yourbatis.StatementUpdate, wantArgumentNames: append([]string{"threadID"}, names("dshSessionID", "threadID")...), wantSQLFragments: []string{"session_thread_id IS NULL OR session_thread_id = $6"}},
	}
	for _, test := range tests {
		t.Run(test.wantID, func(t *testing.T) { assertMapperBuilderContract(t, test) })
	}
	for _, thread := range []string{"", "thread"} {
		expected := names(scopeNames...)
		if thread != "" {
			expected = append(expected, "threadID")
		}
		assertMapperBuilderContract(t, mapperBuilderContract{statement: dSHExecutionMapperInterruptExecutionStatement, bound: buildDSHExecutionMapperInterruptExecution(dialect, scope, thread), wantID: "DSHExecutionMapper.InterruptExecution", wantKind: yourbatis.StatementUpdate, wantArgumentNames: expected, wantSQLFragments: []string{"cancel_requested_at = COALESCE(cancel_requested_at, NOW())", "status IN ('accepted','running')"}})
	}
	for _, input := range []string{"", "input"} {
		for _, thread := range []string{"", "thread"} {
			expected := names()
			if input != "" {
				expected = append(expected, "inputID")
			}
			if thread != "" {
				expected = append(expected, "threadID")
			}
			assertMapperBuilderContract(t, mapperBuilderContract{statement: dSHExecutionMapperClosePermissionsStatement, bound: buildDSHExecutionMapperClosePermissions(dialect, scope, input, thread), wantID: "DSHExecutionMapper.ClosePermissions", wantKind: yourbatis.StatementUpdate, wantArgumentNames: expected, wantSQLFragments: []string{"state = 'pending'"}})
		}
	}
}

func TestDSHExecutionMapperRejectsQueryFailureAndScansNullableJSON(t *testing.T) {
	failure := errors.New("query unavailable")
	executor := newMapperTestExecutor(t, mapperTestResponse{queryErr: failure})
	if _, _, err := NewDSHExecutionMapper(executor).Permission(context.Background(), DSHScope{}, "request"); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	columns := []string{"request_id", "input_id", "dsh_session_id", "session_thread_id", "tool_use_id", "turn", "tool", "input", "available", "public_event_id", "state", "decision"}
	executor = newMapperTestExecutor(t, mapperTestResponse{columns: columns, rows: [][]driver.Value{{"request", nil, "native", "thread", "call", nil, nil, nil, nil, "public", "cancelled", nil}}})
	row, found, err := NewDSHExecutionMapper(executor).Permission(context.Background(), DSHScope{}, "request")
	if err != nil || !found || row.InputID != nil || row.Available != nil || row.State != "cancelled" {
		t.Fatalf("row=%+v,%v", row, err)
	}
}
