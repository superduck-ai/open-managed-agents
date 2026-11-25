package db

import (
	"testing"
	"time"

	"github.com/superduck-ai/yourbatis"
)

func TestHarnessInputMapperBindings(t *testing.T) {
	ref := HarnessTurnRef{UUID: "input", OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}
	dialect := yourbatis.DialectPostgres
	scope := []string{"ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"}
	scopedInput := []string{"ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID", "ref.UUID"}
	for _, contract := range []mapperBuilderContract{
		{
			statement: harnessInputMapperInsertStatement, bound: buildHarnessInputMapperInsert(dialect, ref, `{"text":"test"}`, time.Time{}),
			wantID: "HarnessInputMapper.Insert", wantKind: yourbatis.StatementInsert,
			wantArgumentNames:          []string{"ref.UUID", "inputMessage", "createdAt", "ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID"},
			wantSensitiveArgumentNames: []string{"inputMessage"},
			wantSQLFragments:           []string{"INSERT INTO harness_inputs", "FROM sessions s", "s.workspace_uuid = $5"},
		},
		{
			statement: harnessInputMapperFindStatement, bound: buildHarnessInputMapperFind(dialect, ref),
			wantID: "HarnessInputMapper.Find", wantKind: yourbatis.StatementSelect, wantArgumentNames: scopedInput,
			wantSQLFragments: []string{"JOIN sessions s ON s.uuid = i.session_uuid", "i.input_uuid = $4", "i.input_message"},
		},
		{
			statement: harnessInputMapperFindFirstPendingInputStatement, bound: buildHarnessInputMapperFindFirstPendingInput(dialect, ref),
			wantID: "HarnessInputMapper.FindFirstPendingInput", wantKind: yourbatis.StatementSelect, wantArgumentNames: scope,
			wantSQLFragments: []string{"i.state = 'queued'", "ORDER BY i.id LIMIT 1"},
		},
		{
			statement: harnessInputMapperListUnpublishedStatement, bound: buildHarnessInputMapperListUnpublished(dialect, 64),
			wantID: "HarnessInputMapper.ListUnpublished", wantKind: yourbatis.StatementSelect, wantArgumentNames: []string{"limit"},
			wantSQLFragments: []string{"i.state = 'queued'", "i.published_at IS NULL", "c.workspace_uuid = s.workspace_uuid", "= 'active'"},
		},
		{
			statement: harnessInputMapperListUnpublishedForSessionStatement, bound: buildHarnessInputMapperListUnpublishedForSession(dialect, ref),
			wantID: "HarnessInputMapper.ListUnpublishedForSession", wantKind: yourbatis.StatementSelect, wantArgumentNames: scope,
			wantSQLFragments: []string{"i.state = 'queued'", "i.published_at IS NULL", "ORDER BY i.id"},
		},
		{
			statement: harnessInputMapperMarkPublishedStatement, bound: buildHarnessInputMapperMarkPublished(dialect, ref),
			wantID: "HarnessInputMapper.MarkPublished", wantKind: yourbatis.StatementUpdate, wantArgumentNames: scopedInput,
			wantSQLFragments: []string{"SET published_at = now()", "i.published_at IS NULL"},
		},
		{
			statement: harnessInputMapperSetQueuedStateStatement, bound: buildHarnessInputMapperSetQueuedState(dialect, ref, harnessInputClaimed),
			wantID: "HarnessInputMapper.SetQueuedState", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: []string{"state", "ref.OrganizationUUID", "ref.WorkspaceUUID", "ref.SessionUUID", "ref.UUID"},
			wantSQLFragments:  []string{"SET state = $1", "i.state = 'queued'"},
		},
		{
			statement: harnessInputMapperDeleteBySessionStatement, bound: buildHarnessInputMapperDeleteBySession(dialect, ref),
			wantID: "HarnessInputMapper.DeleteBySession", wantKind: yourbatis.StatementDelete, wantArgumentNames: scope,
			wantSQLFragments: []string{"DELETE FROM harness_inputs", "s.uuid = i.session_uuid"},
		},
	} {
		t.Run(contract.wantID, func(t *testing.T) { assertMapperBuilderContract(t, contract) })
	}
}
