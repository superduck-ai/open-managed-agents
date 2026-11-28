package db

import (
	"slices"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestHarnessStateMapperBindings(t *testing.T) {
	dialect := yourbatis.DialectPostgres
	key := harnessThreadKey{Scope: harnessConversationScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}, UUID: "thread"}
	scopeNames := []string{"key.Scope.OrganizationUUID", "key.Scope.WorkspaceUUID", "key.Scope.SessionUUID", "key.UUID"}
	stateNames := []string{"state.Metadata", "state.PendingUserChoices"}
	state := harnessStateRow{Version: 4}
	for _, contract := range []mapperBuilderContract{
		{
			statement: harnessStateMapperFindStateStatement, bound: buildHarnessStateMapperFindState(dialect, key),
			wantID: "HarnessStateMapper.FindState", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: scopeNames,
			wantSQLFragments:  []string{"LEFT JOIN harness_thread_state s", "s.workspace_uuid = h.workspace_uuid", "s.thread_uuid = h.thread_uuid", "h.thread_uuid = $4"},
		},
		{
			statement: harnessStateMapperSaveStateStatement, bound: buildHarnessStateMapperSaveState(dialect, key, state),
			wantID: "HarnessStateMapper.SaveState", wantKind: yourbatis.StatementInsert,
			wantArgumentNames:          slices.Concat(scopeNames, []string{"state.Version"}, stateNames),
			wantSensitiveArgumentNames: stateNames,
			wantSQLFragments:           []string{"INSERT INTO harness_thread_state", "metadata, pending_user_choices", "ON CONFLICT (organization_uuid, workspace_uuid, session_uuid, thread_uuid)"},
		},
		{
			statement: harnessStateMapperDeleteStatesStatement, bound: buildHarnessStateMapperDeleteStates(dialect, key.Scope),
			wantID: "HarnessStateMapper.DeleteStates", wantKind: yourbatis.StatementDelete,
			wantArgumentNames: []string{"scope.OrganizationUUID", "scope.WorkspaceUUID", "scope.SessionUUID"},
			wantSQLFragments:  []string{"DELETE FROM harness_thread_state", "organization_uuid = $1", "workspace_uuid = $2", "session_uuid = $3"},
		},
		{
			statement: harnessConversationMapperUpdateMessageCountStatement, bound: buildHarnessConversationMapperUpdateMessageCount(dialect, key, 7),
			wantID: "HarnessConversationMapper.UpdateMessageCount", wantKind: yourbatis.StatementUpdate,
			wantArgumentNames: slices.Concat([]string{"messageCount"}, scopeNames),
			wantSQLFragments:  []string{"UPDATE harness_thread_hierarchy SET message_count = $1", "thread_uuid = $5"},
		},
	} {
		assertMapperBuilderContract(t, contract)
	}
}
