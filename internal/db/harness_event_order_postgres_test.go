//go:build integration

package db

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

func TestHarnessInputConfirmationPrecedesModelStartPostgres(t *testing.T) {
	ctx, database, store, claim := newHarnessControlTurn(t)
	receivedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	ref := claim.Lease.Ref
	queued := appendHarnessTestInput(t, store, ref.SessionUUID, harnessControlSessionID, "sevt_harness_order_queued")
	execClaimTest(t, ctx, database, `UPDATE session_events SET created_at = $2, thread_external_id = 'sthr_harness' WHERE uuid = $1`, ref.UUID, receivedAt)
	params := ListSessionEventsPageParams{
		WorkspaceUUID: ref.WorkspaceUUID, SessionExternalID: harnessControlSessionID, PrimaryOnly: true,
		Order: "asc", Limit: 1, UseInputReceivedAt: true, Types: []string{"user.message", "agent.message"},
	}
	before, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(before) != 1 || before[0].UUID != ref.UUID || !before[0].ProcessedAt.IsZero() {
		t.Fatalf("expected unprocessed input before completion: %+v, %v", before, err)
	}
	stale := claim.Lease
	stale.Epoch++
	startedAt := receivedAt.Add(time.Second)
	start := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_harness_order_start", EventType: "span.model_request_start",
		CreatedAt: startedAt, ProcessedAt: startedAt, Payload: json.RawMessage(`{"model":"test"}`)}
	if _, err := store.RecordHarnessModelEvents(ctx, stale, claim.CodeSession, HarnessModelStart, []SessionEvent{start}, nil); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("stale lease started a model request: %v", err)
	}
	started, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, []SessionEvent{start}, nil)
	if err != nil || len(started) != 2 || started[0].UUID != ref.UUID || started[0].ExternalID != before[0].ExternalID ||
		!started[0].ProcessedAt.Equal(startedAt) || started[1].UUID != start.UUID || !started[1].ProcessedAt.Equal(startedAt) {
		t.Fatalf("model start did not follow input confirmation: %+v, %v", started, err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, queued, HarnessTurnDelivery{}, "queued-hash", time.Now().Add(time.Minute)); !errors.Is(err, ErrWorkerEpochMismatch) {
		t.Fatalf("queued input bypassed the active turn: %v", err)
	}
	answeredAt := startedAt.Add(time.Second)
	reply := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_harness_order_answer", EventType: "agent.message",
		CreatedAt: answeredAt, ProcessedAt: answeredAt, Payload: json.RawMessage(`{"content":[{"type":"text","text":"The answer"}]}`)}
	finished, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, []SessionEvent{reply}, &HarnessModelCompletion{Message: harness.AssistantMessage{Text: "The answer"}, UserText: "Start"})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(finished, func(event SessionEvent) bool { return event.UUID == ref.UUID }) {
		t.Fatalf("model completion confirmed the input again: %+v", finished)
	}
	params.Cursor = &SessionEventPageCursor{ExternalID: before[0].ExternalID}
	after, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(after) != 1 || after[0].UUID != reply.UUID {
		t.Fatalf("input cursor moved past the answer after settlement: %+v, %v", after, err)
	}
	params.Cursor, params.Limit = nil, 10
	history, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(history) != 3 || history[0].UUID != ref.UUID || history[1].UUID != reply.UUID || history[2].UUID != queued.UUID ||
		!history[0].ProcessedAt.Equal(startedAt) || !history[0].CreatedAt.Equal(receivedAt) {
		t.Fatalf("history changed the confirmed input time or position: %+v, %v", history, err)
	}
	params.Order = "desc"
	descending, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || !slices.EqualFunc(descending, []SessionEvent{history[2], history[1], history[0]}, func(left, right SessionEvent) bool {
		return left.UUID == right.UUID
	}) {
		t.Fatalf("descending order does not reverse the stable input order: %+v, %v", descending, err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, queued, HarnessTurnDelivery{}, "queued-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("queued input was not claimable after turn completion: %v", err)
	}
}
