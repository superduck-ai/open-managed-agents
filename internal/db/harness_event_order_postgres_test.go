//go:build integration

package db

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	harness "github.com/superduck-ai/open-managed-agents/internal/cloudharness"
)

func TestHarnessEventOrderSurvivesSettlementPostgres(t *testing.T) {
	ctx, database, store, claim := newHarnessControlTurn(t)
	receivedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	answeredAt := receivedAt.Add(2 * time.Second)
	ref := claim.Lease.Ref
	execClaimTest(t, ctx, database, `UPDATE session_events SET created_at = $2, thread_external_id = 'sthr_harness' WHERE uuid = $1`, ref.UUID, receivedAt)
	params := ListSessionEventsPageParams{
		WorkspaceUUID: ref.WorkspaceUUID, SessionExternalID: harnessControlSessionID, PrimaryOnly: true,
		Order: "asc", Limit: 1, UseInputReceivedAt: true, Types: []string{"user.message", "agent.message"},
	}
	before, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(before) != 1 || before[0].UUID != ref.UUID || !before[0].ProcessedAt.IsZero() {
		t.Fatalf("expected unprocessed input before completion: %+v, %v", before, err)
	}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	reply := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_harness_order_answer", EventType: "agent.message",
		CreatedAt: answeredAt, ProcessedAt: answeredAt, Payload: json.RawMessage(`{"content":[{"type":"text","text":"The answer"}]}`)}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, []SessionEvent{reply}, &HarnessModelCompletion{Message: harness.AssistantMessage{Text: "The answer"}, UserText: "Start"}); err != nil {
		t.Fatal(err)
	}
	params.Cursor = &SessionEventPageCursor{ExternalID: before[0].ExternalID}
	after, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(after) != 1 || after[0].UUID != reply.UUID {
		t.Fatalf("input cursor moved past the answer after settlement: %+v, %v", after, err)
	}
	params.Cursor, params.Limit = nil, 10
	history, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(history) != 2 || history[0].UUID != ref.UUID || history[1].UUID != reply.UUID ||
		!history[0].ProcessedAt.After(history[1].ProcessedAt) || !history[0].CreatedAt.Equal(receivedAt) {
		t.Fatalf("refreshed history must use the original input position: %+v, %v", history, err)
	}
	params.Order = "desc"
	descending, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || !slices.EqualFunc(descending, []SessionEvent{history[1], history[0]}, func(left, right SessionEvent) bool {
		return left.UUID == right.UUID
	}) {
		t.Fatalf("descending order does not reverse the stable input order: %+v, %v", descending, err)
	}
}
