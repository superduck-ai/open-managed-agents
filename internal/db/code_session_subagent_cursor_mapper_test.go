package db

import (
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestCodeSessionMapperSubagentPublicationCursorContracts(t *testing.T) {
	tests := []struct {
		name     string
		contract mapperBuilderContract
	}{
		{"advance only moves forward", mapperBuilderContract{
			statement: codeSessionMapperAdvanceSubagentPublishedSequenceStatement,
			bound: buildCodeSessionMapperAdvanceSubagentPublishedSequence(
				yourbatis.DialectPostgres, "workspace-uuid", "cse_test", 42,
			),
			wantID: "CodeSessionMapper.AdvanceSubagentPublishedSequence", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"sequenceNum", "workspaceUUID", "codeSessionExternalID", "sequenceNum"},
			wantSQLFragments: []string{
				"SET subagent_published_sequence_num = $1", "workspace_uuid = $2", "external_id = $3",
				"deleted_at IS NULL", "subagent_published_sequence_num < $4",
			},
		}},
		{"progress read is workspace scoped", mapperBuilderContract{
			statement: codeSessionMapperFindSubagentPublicationProgressStatement,
			bound: buildCodeSessionMapperFindSubagentPublicationProgress(
				yourbatis.DialectPostgres, "workspace-uuid", "cse_test",
			),
			wantID: "CodeSessionMapper.FindSubagentPublicationProgress", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: []string{"workspaceUUID", "codeSessionExternalID"},
			wantSQLFragments:  []string{"SELECT subagent_published_sequence_num, last_internal_sequence_num", "workspace_uuid = $1", "external_id = $2", "deleted_at IS NULL"},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) { assertMapperBuilderContract(t, test.contract) })
	}
}
