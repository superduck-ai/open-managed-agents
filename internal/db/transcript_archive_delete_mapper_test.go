package db

import (
	"reflect"
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestTranscriptArchiveDeleteBindings(t *testing.T) {
	batch := TranscriptDeleteBatch{Scope: TranscriptScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: "session"}, Sequences: []int64{7, 9001}, Cutoff: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	names := []string{"batch.Scope.OrganizationUUID", "batch.Scope.WorkspaceUUID", "batch.Scope.CodeSessionUUID", "sequence", "sequence"}
	values := []any{"org", "workspace", "session", int64(7), int64(9001)}
	for _, test := range []struct {
		contract mapperBuilderContract
		values   []any
	}{
		{mapperBuilderContract{statement: codeSessionInternalEventMapperSoftDeleteArchivedStatement, bound: buildCodeSessionInternalEventMapperSoftDeleteArchived(yourbatis.DialectPostgres, batch), wantID: "CodeSessionInternalEventMapper.SoftDeleteArchived", wantKind: yourbatis.StatementUpdate, wantArgumentNames: names, wantSQLFragments: []string{"UPDATE code_session_internal_events SET deleted_at = now()", "organization_uuid = $1 AND workspace_uuid = $2 AND code_session_uuid = $3", "sequence_num IN ( $4 , $5 )", "deleted_at IS NULL"}}, values},
		{mapperBuilderContract{statement: codeSessionInternalEventMapperHardDeleteArchivedStatement, bound: buildCodeSessionInternalEventMapperHardDeleteArchived(yourbatis.DialectPostgres, batch), wantID: "CodeSessionInternalEventMapper.HardDeleteArchived", wantKind: yourbatis.StatementDelete, wantArgumentNames: append(append([]string{}, names...), "batch.Cutoff"), wantSQLFragments: []string{"DELETE FROM code_session_internal_events", "organization_uuid = $1 AND workspace_uuid = $2 AND code_session_uuid = $3", "sequence_num IN ( $4 , $5 )", "deleted_at < $6"}}, append(append([]any{}, values...), batch.Cutoff)},
	} {
		t.Run(test.contract.wantID, func(t *testing.T) {
			assertMapperBuilderContract(t, test.contract)
			if !reflect.DeepEqual(test.contract.bound.Values(), test.values) {
				t.Fatalf("bound values: %#v, want %#v", test.contract.bound.Values(), test.values)
			}
		})
	}
}
