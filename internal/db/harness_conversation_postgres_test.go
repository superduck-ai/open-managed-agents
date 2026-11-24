//go:build integration

package db

import (
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/yourbatis"
)

func TestHarnessConversationStorePostgres(t *testing.T) {
	ctx, _, database := newCodeSessionClaimTestStore(t)
	ref := HarnessTurnRef{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: uuid.NewString()}
	lease := HarnessTurnLease{Ref: ref}
	store := database.HarnessConversation(lease)
	root := harness.ThreadID(ref.SessionUUID)
	first := harness.UserMessage{Text: "first\x00message"}
	second := harness.AssistantMessage{Text: "second"}
	messages := []harness.MessageWithID{{ID: "first", Message: first}, {ID: "second", Message: second}}
	if err := store.AppendMessages(ctx, root, messages); err != nil {
		t.Fatal(err)
	}
	foreign := lease
	foreign.Ref.WorkspaceUUID = uuid.NewString()
	other := database.HarnessConversation(foreign)
	if found, err := other.ThreadExists(ctx, root); err != nil || found {
		t.Fatalf("tenant scope leaked thread: %v, %v", found, err)
	}
	if rows, err := other.LoadHistoryUpTo(ctx, root, nil, nil); err != nil || len(rows) != 0 {
		t.Fatalf("tenant scope leaked messages: %v, %v", rows, err)
	}
	if err := store.AppendMessages(ctx, root, messages); err != nil {
		t.Fatal(err)
	}
	store = database.HarnessConversation(lease)
	history, err := store.LoadHistoryUpTo(ctx, root, nil, new(int64(2)))
	if err != nil || !slices.Equal(history, []harness.Message{first, second}) {
		t.Fatalf("saved history = %+v, %v", history, err)
	}
	if found, err := store.HasMessageID(ctx, root, "first"); err != nil || !found {
		t.Fatalf("recorded input recovery = %t, %v", found, err)
	}
	if found, err := other.HasMessageID(ctx, root, "first"); err != nil || found {
		t.Fatalf("tenant scope leaked recorded input: %t, %v", found, err)
	}
	if err := store.SaveCompactionSummary(ctx, root, "prefix", 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCompactionSummary(ctx, root, "later summary", 2); err != nil {
		t.Fatal(err)
	}
	child, err := store.SpawnThread(ctx, root, "spawn", true, harness.SpawnContext{InheritUpTo: new(int64(1))})
	if err != nil {
		t.Fatal(err)
	}
	leaf := harness.UserMessage{Text: "child"}
	if err := store.AppendMessages(ctx, child, []harness.MessageWithID{{ID: "child", Message: leaf}}); err != nil {
		t.Fatal(err)
	}
	history, cutoff, prefix, err := store.LoadHistoryWithAncestors(ctx, child)
	if err != nil || cutoff != nil || prefix != 1 || !slices.Equal(history, []harness.Message{harnessSummaryMessage("prefix"), leaf}) {
		t.Fatalf("ancestor history = %+v, %v, %d, %v", history, cutoff, prefix, err)
	}
	info, err := store.GetThreadParentInfo(ctx, child)
	if err != nil || info == nil || info.ThreadID != root || info.ToolCallID != "spawn" {
		t.Fatalf("parent identity = %+v, %v", info, err)
	}
	fresh, err := store.SpawnThread(ctx, child, "fresh", false, harness.SpawnContext{})
	if err != nil {
		t.Fatal(err)
	}
	if got, _, prefix, err := store.LoadHistoryWithAncestors(ctx, fresh); err != nil || len(got) != 0 || prefix != 0 {
		t.Fatalf("fresh thread inherited messages: %v, %d, %v", got, prefix, err)
	}
}

func TestHarnessExecutionStorageMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 173); err != nil {
		t.Fatal(err)
	}
	sessionUUID := uuid.NewString()
	ref := seedHarnessToolInput(t, database, sessionUUID, "sesn_harness_migration", "sevt_harness_migration")
	execClaimTest(t, ctx, database, `INSERT INTO cloud_harness_turns (uuid, external_id, organization_uuid, workspace_uuid, session_uuid, input_event_uuid)
  VALUES ($1, 'hturn_old', $2, $3, $4, $1)`, ref.UUID, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID)
	execClaimTest(t, ctx, database, `INSERT INTO cloud_harness_tools (external_id, organization_uuid, workspace_uuid, session_uuid, turn_uuid, call_id, tool_name, arguments, assistant_text)
  VALUES ('old_tool', $1, $2, $3, $4, 'old_call', 'Read', '{}', 'reading')`, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID, ref.UUID)
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	thread := harness.ThreadID(sessionUUID)
	conversation := store.HarnessConversation(HarnessTurnLease{Ref: ref})
	existing := harness.MessageWithID{ID: "already_saved", Message: harness.UserMessage{Text: "existing history"}}
	if err := conversation.AppendMessages(ctx, thread, []harness.MessageWithID{existing}); err != nil {
		t.Fatal(err)
	}
	if _, err := migrations.UpTo(ctx, 174); err != nil {
		t.Fatal(err)
	}
	var oldTables int
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema()
  AND table_name IN ('cloud_harness_turns', 'cloud_harness_tools')`).Scan(&oldTables); err != nil || oldTables != 0 {
		t.Fatalf("obsolete harness tables = %d, %v", oldTables, err)
	}
	execClaimTest(t, ctx, database, `UPDATE session_events SET harness_execution =
		'{"state":"pending","worker_epoch":0,"attempts":0,"task_kind":""}' WHERE uuid = $1`, ref.UUID)
	if _, err := migrations.UpTo(ctx, 175); err != nil {
		t.Fatal(err)
	}
	var oldColumn bool
	if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'session_events' AND column_name = 'harness_execution')`).Scan(&oldColumn); err != nil || oldColumn {
		t.Fatalf("temporary execution column still exists: %t, %v", oldColumn, err)
	}
	if _, err := NewSessionEventMapper(store.mapperDB).FindByUUID(ctx, ref.WorkspaceUUID, ref.SessionUUID, ref.UUID); err != nil {
		t.Fatalf("migration removed a public session event: %v", err)
	}
	if _, _, err := store.HarnessTurnTask(ctx, ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old execution checkpoint was carried forward: %v", err)
	}
	history, err := conversation.LoadHistoryUpTo(ctx, thread, nil, nil)
	if err != nil || !slices.Equal(history, []harness.Message{existing.Message}) {
		t.Fatalf("existing history = %+v, %v", history, err)
	}
}

func TestHarnessConversationHistoryColumnsMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 169); err != nil {
		t.Fatal(err)
	}
	scope := harnessConversationScope{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: uuid.NewString()}
	execClaimTest(t, ctx, database, `INSERT INTO harness_conversation_history (
		external_id, organization_uuid, workspace_uuid, session_uuid, thread_uuid, message_order, message_id, message_data
	) VALUES ('old_second', $1, $2, $3, $3, 2, 'second', '{"kind":"assistant","text":"second"}'),
	         ('old_first', $1, $2, $3, $3, 1, 'first', '{"kind":"user","text":"first"}')`,
		scope.OrganizationUUID, scope.WorkspaceUUID, scope.SessionUUID)
	if _, err := migrations.UpTo(ctx, 170); err != nil {
		t.Fatal(err)
	}

	var oldColumns int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'harness_conversation_history'
		AND column_name IN ('uuid', 'external_id', 'message_order')`).Scan(&oldColumns)
	if err != nil || oldColumns != 0 {
		t.Fatalf("legacy history columns = %d, %v", oldColumns, err)
	}
	store := (&DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}).HarnessConversation(HarnessTurnLease{Ref: HarnessTurnRef{
		OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, SessionUUID: scope.SessionUUID,
	}})
	history, err := store.LoadHistoryUpTo(ctx, harness.ThreadID(scope.SessionUUID), nil, nil)
	expected := []harness.Message{harness.UserMessage{Text: "first"}, harness.AssistantMessage{Text: "second"}}
	if err != nil || !slices.Equal(history, expected) {
		t.Fatalf("history after migration = %+v, %v", history, err)
	}

	if _, err := migrations.DownTo(ctx, 169); err != nil {
		t.Fatal(err)
	}
	var restored int
	err = database.QueryRowContext(ctx, `SELECT COUNT(*) FROM harness_conversation_history
		WHERE uuid IS NOT NULL AND external_id LIKE 'hmsg_%' AND message_order IN (1, 2)`).Scan(&restored)
	if err != nil || restored != 2 {
		t.Fatalf("history after rollback = %d, %v", restored, err)
	}
}

func TestHarnessConversationThreadAndSummaryMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 170); err != nil {
		t.Fatal(err)
	}
	ref := HarnessTurnRef{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: uuid.NewString()}
	root, child := harness.ThreadID(ref.SessionUUID), harness.ThreadID(uuid.NewString())
	execClaimTest(t, ctx, database, `INSERT INTO harness_thread_hierarchy (
		uuid, external_id, organization_uuid, workspace_uuid, session_uuid, root_thread_uuid, parent_thread_uuid, spawn_message_order, spawn_tool_call_id
	) VALUES ($3, 'old_root', $1, $2, $3, $3, NULL, NULL, NULL),
	         ($4, 'old_child', $1, $2, $3, $3, $3, 2, 'spawn')`, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID, string(child))
	execClaimTest(t, ctx, database, `INSERT INTO harness_compaction_summaries (
		external_id, organization_uuid, workspace_uuid, session_uuid, thread_uuid, up_to_order, summary
	) VALUES ('old_summary', $1, $2, $3, $3, 2, 'prefix')`, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID)
	execClaimTest(t, ctx, database, `INSERT INTO harness_conversation_history (
		organization_uuid, workspace_uuid, session_uuid, thread_uuid, message_seq, message_id, message_data
	) VALUES ($1, $2, $3, $4, 1, 'leaf', '{"kind":"user","text":"leaf"}')`, ref.OrganizationUUID, ref.WorkspaceUUID, ref.SessionUUID, string(child))
	if _, err := migrations.UpTo(ctx, 171); err != nil {
		t.Fatal(err)
	}

	var oldColumns int
	err := database.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND (
			(table_name = 'harness_thread_hierarchy' AND column_name IN ('uuid', 'external_id', 'spawn_message_order')) OR
			(table_name = 'harness_compaction_summaries' AND column_name IN ('uuid', 'external_id', 'up_to_order'))
		)`).Scan(&oldColumns)
	if err != nil || oldColumns != 0 {
		t.Fatalf("legacy thread and summary columns = %d, %v", oldColumns, err)
	}
	store := (&DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}).HarnessConversation(HarnessTurnLease{Ref: ref})
	info, err := store.GetThreadParentInfo(ctx, child)
	if err != nil || info == nil || info.ThreadID != root || info.ToolCallID != "spawn" {
		t.Fatalf("parent after migration = %+v, %v", info, err)
	}
	history, _, prefix, err := store.LoadHistoryWithAncestors(ctx, child)
	want := []harness.Message{harnessSummaryMessage("prefix"), harness.UserMessage{Text: "leaf"}}
	if err != nil || prefix != 1 || !slices.Equal(history, want) {
		t.Fatalf("ancestor history after migration = %+v, %d, %v", history, prefix, err)
	}

	if _, err := migrations.DownTo(ctx, 170); err != nil {
		t.Fatal(err)
	}
	var threadRestored, summaryRestored bool
	err = database.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM harness_thread_hierarchy WHERE uuid = $1 AND root_thread_uuid = $2 AND parent_thread_uuid = $2
			AND external_id = 'hthr_' || uuid AND spawn_message_order = 2),
		EXISTS (SELECT 1 FROM harness_compaction_summaries WHERE uuid IS NOT NULL AND external_id LIKE 'hsum_%'
			AND thread_uuid = $2 AND up_to_order = 2 AND summary = 'prefix')`, string(child), string(root)).Scan(&threadRestored, &summaryRestored)
	if err != nil || !threadRestored || !summaryRestored {
		t.Fatalf("thread and summary after rollback = %v, %v, %v", threadRestored, summaryRestored, err)
	}
}
