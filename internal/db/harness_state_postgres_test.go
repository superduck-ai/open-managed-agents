//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/yourbatis"
)

func TestHarnessThreadStateRollbackPostgres(t *testing.T) {
	ctx, database, store, claim := newHarnessControlTurn(t)
	ref := claim.Lease.Ref
	thread := harness.ThreadID(ref.SessionUUID)
	conversation := store.HarnessConversation(claim.Lease)
	execClaimTest(t, ctx, database, `ALTER TABLE harness_thread_state ADD CHECK (version = 0)`)
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	var pgError *pgconn.PgError
	completion := &HarnessModelCompletion{UserText: "write then fail", Message: harness.AssistantMessage{Text: "not committed"}}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, nil, completion); !errors.As(err, &pgError) || pgError.Code != "23514" {
		t.Fatalf("expected a state constraint failure after appending history: %v", err)
	}
	state, err := store.HarnessTurnState(ctx, ref)
	if err != nil || state != "running" {
		t.Fatalf("execution state after rollback = %q, %v", state, err)
	}
	history, err := conversation.LoadHistoryUpTo(ctx, thread, nil, nil)
	if err != nil || len(history) != 0 {
		t.Fatalf("history after rollback = %+v, %v", history, err)
	}
}

func TestHarnessThreadStateRoundTripPostgres(t *testing.T) {
	ctx, database, store := newCodeSessionClaimTestStore(t)
	ref := HarnessTurnRef{OrganizationUUID: claimTestOrganization, WorkspaceUUID: claimTestWorkspace, SessionUUID: uuid.NewString()}
	lease := HarnessTurnLease{Ref: ref}
	thread := harness.ThreadID(ref.SessionUUID)
	stateStore := store.HarnessState(lease)
	metadata := json.RawMessage(`{"note":"escaped\u0000value"}`)
	choice := harness.UserChoice{ID: "choose", Prompt: "Pick", Choices: []string{"one", "two"}}
	if err := stateStore.SetMetadata(ctx, thread, metadata); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.AddActiveSubscription(ctx, thread, "call"); err != nil {
		t.Fatal(err)
	}
	if err := stateStore.AddPendingUserChoice(ctx, thread, choice); err != nil {
		t.Fatal(err)
	}
	reader := store.HarnessState(lease)
	if got, err := reader.GetMetadata(ctx, thread); err != nil || string(got) != string(metadata) {
		t.Fatalf("metadata = %s, %v", got, err)
	}
	if got, err := reader.GetActiveSubscriptions(ctx, thread); err != nil || !slices.Equal(got, []harness.ToolCallID{"call"}) {
		t.Fatalf("subscriptions = %v, %v", got, err)
	}
	if got, err := reader.GetPendingUserChoices(ctx, thread); err != nil || len(got) != 1 || got[0].ID != choice.ID || !slices.Equal(got[0].Choices, choice.Choices) {
		t.Fatalf("pending choices = %v, %v", got, err)
	}
	session := Session{UUID: ref.SessionUUID, OrganizationUUID: ref.OrganizationUUID, WorkspaceUUID: ref.WorkspaceUUID}
	if err := store.mapperDB.Transaction(ctx, func(executor yourbatis.Executor) error {
		return deleteHarnessConversation(ctx, executor, session)
	}); err != nil {
		t.Fatal(err)
	}
	var stateRows int
	err := database.QueryRowContext(ctx, "SELECT count(*) FROM harness_thread_state WHERE session_uuid = $1", ref.SessionUUID).Scan(&stateRows)
	if err != nil || stateRows != 0 {
		t.Fatalf("state rows after session removal = %d, %v", stateRows, err)
	}
}

func TestHarnessThreadStateSplitMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, migrations := newIsolatedMigrationTestDatabase(t, address)
	if _, err := migrations.UpTo(ctx, 171); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `INSERT INTO harness_thread_hierarchy
		(thread_uuid, organization_uuid, workspace_uuid, session_uuid, root_thread_uuid, state_data)
		VALUES ($3, $1, $2, $3, $3, '{"version":17,"metadata":{"discard":true},"processed_ids":{"old":{}}}')`,
		claimTestOrganization, claimTestWorkspace, uuid.NewString())

	if _, err := migrations.UpTo(ctx, 172); err != nil {
		t.Fatal(err)
	}
	var legacyColumn bool
	var rows int
	err := database.QueryRowContext(ctx, `SELECT
		EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema()
			AND table_name = 'harness_thread_hierarchy' AND column_name = 'state_data'),
		(SELECT count(*) FROM harness_thread_state)`).Scan(&legacyColumn, &rows)
	if err != nil || legacyColumn || rows != 0 {
		t.Fatalf("state split backfilled old state or kept the old column: %v, %d, %v", legacyColumn, rows, err)
	}

	session := uuid.NewString()
	execClaimTest(t, ctx, database, `INSERT INTO harness_thread_state
		(organization_uuid, workspace_uuid, session_uuid, thread_uuid, processed_message_ids, version, pending)
		VALUES ($1, $2, $3, $3, '{"old":{}}', 3, '{"kind":"model"}')`, claimTestOrganization, claimTestWorkspace, session)
	if _, err := migrations.UpTo(ctx, 177); err != nil {
		t.Fatal(err)
	}
	var oldIDs bool
	var version int
	err = database.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema()
			AND table_name = 'harness_thread_state' AND column_name IN ('processed_message_ids', 'pending')
	), version FROM harness_thread_state WHERE session_uuid = $1`, session).Scan(&oldIDs, &version)
	if err != nil || oldIDs || version != 3 {
		t.Fatalf("thread state migrations changed thread state: %t, %d, %v", oldIDs, version, err)
	}
}
