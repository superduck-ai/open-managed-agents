package db

import (
	"reflect"
	"slices"
	"testing"

	"github.com/superduck-ai/yourbatis"
)

func TestHarnessConversationHistoryBindings(t *testing.T) {
	key := harnessThreadKey{Scope: harnessConversationScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}, UUID: "thread"}
	start, end := int64(2), int64(5)
	bound := buildHarnessConversationMapperLoadMessages(yourbatis.DialectPostgres, key, &start, &end)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: harnessConversationMapperLoadMessagesStatement, bound: bound,
		wantID: "HarnessConversationMapper.LoadMessages", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"key.Scope.OrganizationUUID", "key.Scope.WorkspaceUUID", "key.Scope.SessionUUID", "key.UUID", "startFrom", "upTo"},
		wantSQLFragments:  []string{"SELECT message_data", "thread_uuid = $4", "message_seq > $5", "message_seq <= $6", "ORDER BY message_seq"},
	})
	if want := []any{"org", "workspace", "session", "thread", &start, &end}; !reflect.DeepEqual(bound.Values(), want) {
		t.Fatalf("message bounds = %#v, want %#v", bound.Values(), want)
	}
	unbounded := buildHarnessConversationMapperLoadMessages(yourbatis.DialectPostgres, key, nil, nil)
	if len(unbounded.Values()) != 4 {
		t.Fatalf("unexpected unbounded argument count: %d", len(unbounded.Values()))
	}
}

func TestHarnessConversationAppendBindings(t *testing.T) {
	key := harnessThreadKey{Scope: harnessConversationScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}, UUID: "thread"}
	bound := buildHarnessConversationMapperAppendMessages(yourbatis.DialectPostgres, key, nil, "message", `{"kind":"user","text":"example"}`, 2)
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: harnessConversationMapperAppendMessagesStatement, bound: bound,
		wantID: "HarnessConversationMapper.AppendMessages", wantKind: yourbatis.StatementInsert,
		wantArgumentNames:          []string{"key.Scope.OrganizationUUID", "key.Scope.WorkspaceUUID", "key.Scope.SessionUUID", "key.UUID", "turnUUID", "messageID", "data", "messageSeq"},
		wantSensitiveArgumentNames: []string{"data"},
		wantSQLFragments: []string{
			"INSERT INTO harness_conversation_history",
			"(organization_uuid, workspace_uuid, session_uuid, thread_uuid, turn_uuid, message_id, message_data, message_seq)",
			"ON CONFLICT (organization_uuid, workspace_uuid, session_uuid, thread_uuid, message_id) DO NOTHING",
		},
	})
}

func TestHarnessConversationThreadAndSummaryBindings(t *testing.T) {
	key := harnessThreadKey{Scope: harnessConversationScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}, UUID: "thread"}
	dialect := yourbatis.DialectPostgres
	spawnSeq := int64(3)
	thread := harnessConversationThread{RootThreadUUID: "root", ParentThreadUUID: new("parent"), SpawnMessageSeq: &spawnSeq}
	keyArguments := []string{"key.Scope.OrganizationUUID", "key.Scope.WorkspaceUUID", "key.Scope.SessionUUID", "key.UUID"}
	for _, contract := range []mapperBuilderContract{
		{
			statement: harnessConversationMapperEnsureRootStatement, bound: buildHarnessConversationMapperEnsureRoot(dialect, key),
			wantID: "HarnessConversationMapper.EnsureRoot", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: append(slices.Clone(keyArguments), "key.UUID"),
			wantSQLFragments:  []string{"(organization_uuid, workspace_uuid, session_uuid, thread_uuid, root_thread_uuid)", "ON CONFLICT (thread_uuid) DO NOTHING"},
		},
		{
			statement: harnessConversationMapperInsertThreadStatement, bound: buildHarnessConversationMapperInsertThread(dialect, key, thread),
			wantID: "HarnessConversationMapper.InsertThread", wantKind: yourbatis.StatementInsert,
			wantArgumentNames: append(slices.Clone(keyArguments), "thread.RootThreadUUID", "thread.ParentThreadUUID", "thread.SpawnMessageSeq", "thread.SpawnToolCallID", "thread.Fresh"),
			wantSQLFragments:  []string{"thread_uuid, root_thread_uuid, parent_thread_uuid", "spawn_message_seq, spawn_tool_call_id"},
		},
		{
			statement: harnessConversationMapperLockThreadStatement, bound: buildHarnessConversationMapperLockThread(dialect, key),
			wantID: "HarnessConversationMapper.LockThread", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: keyArguments,
			wantSQLFragments:  []string{"SELECT thread_uuid, root_thread_uuid, parent_thread_uuid, spawn_message_seq", "thread_uuid = $4 FOR UPDATE"},
		},
		{
			statement: harnessConversationMapperSaveSummaryStatement, bound: buildHarnessConversationMapperSaveSummary(dialect, key, "summary", 3),
			wantID: "HarnessConversationMapper.SaveSummary", wantKind: yourbatis.StatementInsert,
			wantArgumentNames:          append(slices.Clone(keyArguments), "upToSeq", "summary"),
			wantSensitiveArgumentNames: []string{"summary"},
			wantSQLFragments:           []string{"thread_uuid, up_to_seq, summary)", "ON CONFLICT (organization_uuid, workspace_uuid, session_uuid, thread_uuid, up_to_seq)"},
		},
		{
			statement: harnessConversationMapperFindSummaryStatement, bound: buildHarnessConversationMapperFindSummary(dialect, key, &spawnSeq),
			wantID: "HarnessConversationMapper.FindSummary", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: append(slices.Clone(keyArguments), "upToSeq"),
			wantSQLFragments:  []string{"SELECT summary, up_to_seq", "thread_uuid = $4", "up_to_seq <= $5", "ORDER BY up_to_seq DESC LIMIT 1"},
		},
		{
			statement: harnessConversationMapperFindSummaryStatement, bound: buildHarnessConversationMapperFindSummary(dialect, key, nil),
			wantID: "HarnessConversationMapper.FindSummary", wantKind: yourbatis.StatementSelect,
			wantArgumentNames: keyArguments,
			wantSQLFragments:  []string{"thread_uuid = $4 ORDER BY up_to_seq DESC LIMIT 1"},
		},
	} {
		assertMapperBuilderContract(t, contract)
	}
}

func TestHarnessRecordedMessageBindings(t *testing.T) {
	key := harnessThreadKey{Scope: harnessConversationScope{OrganizationUUID: "org", WorkspaceUUID: "workspace", SessionUUID: "session"}, UUID: "thread"}
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: harnessConversationMapperHasMessageIDStatement,
		bound:     buildHarnessConversationMapperHasMessageID(yourbatis.DialectPostgres, key, "message"),
		wantID:    "HarnessConversationMapper.HasMessageID", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"key.Scope.OrganizationUUID", "key.Scope.WorkspaceUUID", "key.Scope.SessionUUID", "key.UUID", "messageID"},
		wantSQLFragments:  []string{"FROM harness_conversation_history", "thread_uuid = $4", "message_id = $5"},
	})
	assertMapperBuilderContract(t, mapperBuilderContract{
		statement: harnessConversationMapperListTurnMessagesStatement,
		bound:     buildHarnessConversationMapperListTurnMessages(yourbatis.DialectPostgres, key, "turn"),
		wantID:    "HarnessConversationMapper.ListTurnMessages", wantKind: yourbatis.StatementSelect,
		wantArgumentNames: []string{"key.Scope.OrganizationUUID", "key.Scope.WorkspaceUUID", "key.Scope.SessionUUID", "key.UUID", "turnUUID"},
		wantSQLFragments:  []string{"SELECT message_data FROM harness_conversation_history", "thread_uuid = $4", "turn_uuid = $5", "ORDER BY message_seq"},
	})
}
