//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/sessioncontract"
	"github.com/superduck-ai/yourbatis"
)

func TestHarnessInputQueuePublicationPostgres(t *testing.T) {
	ctx, database, store, ref := newHarnessControlInput(t)
	wrongTenant := ref
	wrongTenant.WorkspaceUUID = uuid.NewString()
	if _, found, err := store.FindHarnessInput(ctx, wrongTenant); err != nil || found {
		t.Fatalf("lookup crossed the workspace boundary: %t, %v", found, err)
	}
	if err := store.MarkHarnessUserInputPublished(ctx, wrongTenant); err != nil {
		t.Fatal(err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET status = 'initializing' WHERE session_uuid = $1`, ref.SessionUUID)
	if pending, err := store.ListUnpublishedHarnessInputs(ctx, 64); err != nil || len(pending) != 0 {
		t.Fatalf("background publisher selected an initializing session: %d, %v", len(pending), err)
	}
	execClaimTest(t, ctx, database, `UPDATE code_sessions SET status = 'active' WHERE session_uuid = $1`, ref.SessionUUID)
	pending, err := store.ListUnpublishedHarnessInputs(ctx, 64)
	if err != nil || len(pending) != 1 || pending[0].Ref != ref || pending[0].Message.Text != "read" || !pending[0].PendingPublication() {
		t.Fatalf("committed input cannot be republished: %+v, %v", pending, err)
	}
	if err := store.MarkHarnessUserInputPublished(ctx, ref); err != nil {
		t.Fatal(err)
	}
	input, found, err := store.FindHarnessInput(ctx, ref)
	if err != nil || !found || input.State != harnessInputQueued || input.PublishedAt == nil {
		t.Fatalf("publication changed queue membership: %+v, %t, %v", input, found, err)
	}
	if pending, err := store.ListUnpublishedHarnessInputs(ctx, 64); err != nil || len(pending) != 0 {
		t.Fatalf("acknowledged input is still eligible for publication: %d, %v", len(pending), err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, ref, HarnessTurnDelivery{}, "claimed", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	input, found, err = store.FindHarnessInput(ctx, ref)
	if err != nil || !found || input.State != harnessInputClaimed {
		t.Fatalf("claim did not advance queue state: %q, %t, %v", input.State, found, err)
	}
}

func TestHarnessInputQueueRollbackPostgres(t *testing.T) {
	ctx, _, store, baseline := newHarnessControlInput(t)
	ref := baseline
	ref.UUID = uuid.NewString()
	event := SessionEvent{UUID: ref.UUID, ExternalID: "sevt_rolled_back", EventType: "user.message", CreatedAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"content":[{"type":"text","text":"rollback"}]}`)}
	if _, err := store.AppendSessionUserEvents(ctx, ref.WorkspaceUUID, harnessControlSessionID, []SessionEvent{event, event}, nil, nil, nil); err == nil {
		t.Fatal("duplicate event did not fail its transaction")
	}
	if _, found, err := store.FindHarnessInput(ctx, ref); err != nil || found {
		t.Fatalf("rolled-back event left an internal input: %t, %v", found, err)
	}
	if _, err := store.GetSessionEvent(ctx, ref.WorkspaceUUID, harnessControlSessionID, event.ExternalID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back event appeared in history: %v", err)
	}
}

func TestHarnessInputQueueMigrationPostgres(t *testing.T) {
	url := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_MIGRATION_DATABASE_URL is not set")
	}
	ctx, database, provider := newIsolatedMigrationTestDatabase(t, url)
	if _, err := provider.UpTo(ctx, 177); err != nil {
		t.Fatal(err)
	}
	seedClaimCodeSession(t, ctx, database, 1, sessioncontract.CloudHarnessRuntime, "running")
	sessionUUID := "53000000-0000-0000-0001-000000000001"
	execClaimTest(t, ctx, database, `UPDATE sessions SET execution_runtime = 'cloud_harness' WHERE uuid = $1`, sessionUUID)
	active := seedHarnessToolInput(t, database, sessionUUID, harnessControlSessionID, "sevt_old_active")
	queued := seedHarnessToolInput(t, database, sessionUUID, harnessControlSessionID, "sevt_old_queued")
	execClaimTest(t, ctx, database, `UPDATE session_events SET processed_at = now() WHERE uuid = $1`, active.UUID)
	execClaimTest(t, ctx, database, `INSERT INTO harness_thread_state (organization_uuid, workspace_uuid, session_uuid, thread_uuid, execution_input_uuid, execution)
	    VALUES ($1, $2, $3, $3, $4, '{"state":"pending","worker_epoch":0,"attempts":0,"task_kind":""}')`,
		active.OrganizationUUID, active.WorkspaceUUID, sessionUUID, active.UUID)
	if _, err := provider.UpTo(ctx, 178); err != nil {
		t.Fatal(err)
	}
	store := &DB{mapperDB: yourbatis.NewDB(database, yourbatis.DialectPostgres)}
	for _, check := range []struct {
		ref   HarnessTurnRef
		state string
	}{{active, harnessInputClaimed}, {queued, harnessInputQueued}} {
		input, found, err := store.FindHarnessInput(ctx, check.ref)
		if err != nil || !found || input.State != check.state {
			t.Fatalf("migration input %s = %q, %t, %v", check.ref.UUID, input.State, found, err)
		}
		message, err := sessioncontract.ParseHarnessUserInput(input.Message.LegacyEvent)
		if err != nil || message.Text != "read" {
			t.Fatalf("migration lost input text: %v", err)
		}
	}
}
