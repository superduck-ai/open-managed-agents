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
	hard := mapperBuilderContract{statement: codeSessionInternalEventMapperHardDeleteArchivedStatement, bound: buildCodeSessionInternalEventMapperHardDeleteArchived(yourbatis.DialectPostgres, batch), wantID: "CodeSessionInternalEventMapper.HardDeleteArchived", wantKind: yourbatis.StatementDelete, wantArgumentNames: append(append([]string{}, names...), "batch.Cutoff"), wantSQLFragments: []string{"DELETE FROM code_session_internal_events", "organization_uuid = $1 AND workspace_uuid = $2 AND code_session_uuid = $3", "sequence_num IN ( $4 , $5 )", "deleted_at < $6"}}
	t.Run("hard", func(t *testing.T) {
		assertMapperBuilderContract(t, hard)
		want := append(append([]any{}, values...), batch.Cutoff)
		if !reflect.DeepEqual(hard.bound.Values(), want) {
			t.Fatalf("bound values: %#v, want %#v", hard.bound.Values(), want)
		}
	})
	for _, terminal := range []bool{false, true} {
		name := "boundary"
		if terminal {
			name = "terminal"
		}
		t.Run(name, func(t *testing.T) {
			batch.Eligibility = TranscriptArchiveQuery{Terminal: terminal, Cutoff: batch.Cutoff.Add(-24 * time.Hour)}
			wantNames := append(append([]string{}, names...), "batch.Eligibility.Cutoff")
			wantValues := append(append([]any{}, values...), batch.Eligibility.Cutoff)
			fragments := []string{"UPDATE code_session_internal_events SET deleted_at = now()", "organization_uuid = $1 AND workspace_uuid = $2 AND code_session_uuid = $3", "sequence_num IN ( $4 , $5 )", "deleted_at IS NULL", "WHERE e.uuid = code_session_internal_events.uuid"}
			if terminal {
				wantNames = append(wantNames, "batch.Eligibility.Cutoff")
				wantValues = append(wantValues, batch.Eligibility.Cutoff)
				fragments = append(fragments, "s.archived_at < $6 OR s.deleted_at < $7", "active.worker_lease_expires_at >= now() OR active.worker_status <> 'idle'")
			} else {
				fragments = append(fragments, "e.created_at < $6", "boundary.agent_id IS NOT DISTINCT FROM e.agent_id", "boundary.is_compaction AND boundary.sequence_num > e.sequence_num")
			}
			contract := mapperBuilderContract{statement: codeSessionInternalEventMapperSoftDeleteArchivedStatement, bound: buildCodeSessionInternalEventMapperSoftDeleteArchived(yourbatis.DialectPostgres, batch), wantID: "CodeSessionInternalEventMapper.SoftDeleteArchived", wantKind: yourbatis.StatementUpdate, wantArgumentNames: wantNames, wantSQLFragments: fragments}
			assertMapperBuilderContract(t, contract)
			if !reflect.DeepEqual(contract.bound.Values(), wantValues) {
				t.Fatalf("bound values: %#v, want %#v", contract.bound.Values(), wantValues)
			}
		})
	}
}
