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

func TestHarnessThinkingCommitsWithoutEndingTurnPostgres(t *testing.T) {
	ctx, _, store, claim := newHarnessControlTurn(t)
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, nil, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	thinking := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_thinking_complete", EventType: "agent.thinking",
		CreatedAt: now, ProcessedAt: now, Payload: json.RawMessage(`{"type":"agent.thinking"}`)}
	for _, want := range []int{1, 0} {
		events, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelThinking, []SessionEvent{thinking}, nil)
		if err != nil || len(events) != want || want == 1 && events[0].ExternalID != thinking.ExternalID {
			t.Fatalf("thinking commit = %+v, %v, want %d events", events, err, want)
		}
	}
	state, err := store.HarnessTurnState(ctx, claim.Lease.Ref)
	if err != nil || state != "running" {
		t.Fatalf("thinking completion ended the turn: %q, %v", state, err)
	}
}

func TestHarnessModelHistoryPreservesThinkingBeforeFinalEventsPostgres(t *testing.T) {
	ctx, _, store, claim := newHarnessControlTurn(t)
	endedAt := time.Now().UTC().Truncate(time.Microsecond)
	startedAt := endedAt.Add(-time.Second)
	thinkingAt := endedAt.Add(time.Millisecond)
	start := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_order_model_start", EventType: "span.model_request_start",
		CreatedAt: startedAt, ProcessedAt: startedAt, Payload: json.RawMessage(`{"type":"span.model_request_start"}`)}
	thinking := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_order_thinking", EventType: "agent.thinking",
		CreatedAt: thinkingAt, ProcessedAt: thinkingAt, Payload: json.RawMessage(`{"type":"agent.thinking"}`)}
	reply := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_order_answer", EventType: "agent.message",
		CreatedAt: endedAt, ProcessedAt: thinkingAt, Payload: json.RawMessage(`{"content":[{"type":"text","text":"The answer"}]}`)}
	end := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_order_model_end", EventType: "span.model_request_end",
		CreatedAt: endedAt, ProcessedAt: thinkingAt, Payload: json.RawMessage(`{"type":"span.model_request_end"}`)}

	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, []SessionEvent{start}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelThinking, []SessionEvent{thinking}, nil); err != nil {
		t.Fatal(err)
	}
	completion := &HarnessModelCompletion{Message: harness.AssistantMessage{Text: "The answer"}, UserText: "Start"}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, []SessionEvent{reply, end}, completion); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name          string
		order         string
		useWriteOrder bool
		want          []SessionEvent
	}{
		{"write-order-asc", "asc", true, []SessionEvent{start, thinking, reply, end}},
		{"write-order-desc", "desc", true, []SessionEvent{end, reply, thinking, start}},
		{"processing-time-asc", "asc", false, []SessionEvent{start, thinking, reply, end}},
		{"processing-time-desc", "desc", false, []SessionEvent{end, reply, thinking, start}},
	} {
		t.Run(test.name, func(t *testing.T) {
			params := ListSessionEventsPageParams{
				WorkspaceUUID: claim.Lease.Ref.WorkspaceUUID, SessionExternalID: claim.CodeSession.SessionExternalID,
				Limit: 1, Order: test.order, UseEventWriteOrder: test.useWriteOrder,
				Types: []string{start.EventType, thinking.EventType, reply.EventType, end.EventType},
			}
			for i, expected := range test.want {
				events, more, err := store.ListSessionEventsPage(ctx, params)
				if err != nil || len(events) != 1 || more != (i < len(test.want)-1) {
					t.Fatalf("page %d = %+v, more=%t, err=%v", i, events, more, err)
				}
				event := events[0]
				if event.ExternalID != expected.ExternalID || !event.ProcessedAt.Equal(expected.ProcessedAt) {
					t.Fatalf("page %d = %s at %s, want %s at %s", i, event.ExternalID, event.ProcessedAt, expected.ExternalID, expected.ProcessedAt)
				}
				params.Cursor = &SessionEventPageCursor{ExternalID: event.ExternalID}
			}
		})
	}
}

func TestHarnessInputConfirmationPrecedesModelStartPostgres(t *testing.T) {
	ctx, database, store, claim := newHarnessControlTurn(t)
	receivedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	ref := claim.Lease.Ref
	queued := appendHarnessTestInput(t, store, ref.SessionUUID, harnessControlSessionID, "sevt_harness_order_queued")
	execClaimTest(t, ctx, database, `UPDATE session_events SET created_at = $2, thread_external_id = 'sthr_harness' WHERE uuid = $1`, ref.UUID, receivedAt)
	params := ListSessionEventsPageParams{
		WorkspaceUUID: ref.WorkspaceUUID, SessionExternalID: harnessControlSessionID, PrimaryOnly: true,
		Order: "asc", Limit: 1, UseEventWriteOrder: true, Types: []string{"user.message", "agent.message"},
	}
	before, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(before) != 0 {
		t.Fatalf("unexpected replay before confirmation: %+v, %v", before, err)
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
	if err != nil || len(started) != 2 || started[0].UUID != ref.UUID || started[0].ExternalID != "sevt_harness_input" ||
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
	params.Cursor = &SessionEventPageCursor{ExternalID: "sevt_harness_input"}
	after, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(after) != 1 || after[0].UUID != reply.UUID {
		t.Fatalf("replay after confirmed input: %+v, %v", after, err)
	}
	params.Cursor, params.Limit = nil, 10
	history, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(history) != 2 || history[0].UUID != ref.UUID || history[1].UUID != reply.UUID ||
		!history[0].ProcessedAt.Equal(startedAt) || !history[0].CreatedAt.Equal(receivedAt) {
		t.Fatalf("history changed the confirmed input time or position: %+v, %v", history, err)
	}
	params.Order = "desc"
	descending, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || !slices.EqualFunc(descending, []SessionEvent{history[1], history[0]}, func(left, right SessionEvent) bool {
		return left.UUID == right.UUID
	}) {
		t.Fatalf("descending order does not reverse the stable input order: %+v, %v", descending, err)
	}
	if _, err := store.ClaimHarnessTurn(ctx, queued, HarnessTurnDelivery{}, "queued-hash", time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("queued input was not claimable after turn completion: %v", err)
	}
}

func TestHarnessQueuedInputHistoryFollowsProcessingOrderPostgres(t *testing.T) {
	ctx, database, store, claim := newHarnessControlTurn(t)
	base := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	ref := claim.Lease.Ref
	start := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_history_start", EventType: "span.model_request_start",
		CreatedAt: base, ProcessedAt: base, Payload: json.RawMessage(`{"type":"span.model_request_start"}`)}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelStart, []SessionEvent{start}, nil); err != nil {
		t.Fatal(err)
	}
	queued := appendHarnessTestInput(t, store, ref.SessionUUID, harnessControlSessionID, "sevt_history_queued")
	receivedAt := base.Add(time.Second)
	execClaimTest(t, ctx, database, `UPDATE session_events SET created_at = $2 WHERE uuid = $1`, queued.UUID, receivedAt)
	toolResult := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_history_tool_result", EventType: "agent.tool_result",
		CreatedAt: base.Add(2 * time.Second), ProcessedAt: base.Add(2 * time.Second), Payload: json.RawMessage(`{"type":"agent.tool_result"}`)}
	reply := SessionEvent{UUID: uuid.NewString(), ExternalID: "sevt_history_reply", EventType: "agent.message",
		CreatedAt: base.Add(3 * time.Second), ProcessedAt: base.Add(3 * time.Second), Payload: json.RawMessage(`{"type":"agent.message"}`)}
	completion := &HarnessModelCompletion{Message: harness.AssistantMessage{Text: "A"}, UserText: "Start"}
	if _, err := store.RecordHarnessModelEvents(ctx, claim.Lease, claim.CodeSession, HarnessModelEnd, []SessionEvent{toolResult, reply}, completion); err != nil {
		t.Fatal(err)
	}

	params := ListSessionEventsPageParams{
		WorkspaceUUID: ref.WorkspaceUUID, SessionExternalID: harnessControlSessionID,
		Order: "asc", Limit: 2, Types: []string{"user.message", "agent.tool_result", "agent.message"},
	}
	ascending, more, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || !more || len(ascending) != 2 || ascending[0].UUID != ref.UUID || ascending[1].UUID != toolResult.UUID {
		t.Fatalf("first ascending page = %+v, more=%t, err=%v", ascending, more, err)
	}
	params.Cursor = &SessionEventPageCursor{ExternalID: toolResult.ExternalID}
	last, more, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || more || len(last) != 1 || last[0].UUID != reply.UUID {
		t.Fatalf("pending input in history: %+v, more=%t, err=%v", last, more, err)
	}
	params.Cursor = &SessionEventPageCursor{ExternalID: "sevt_history_queued"}
	legacy, _, err := store.ListSessionEventsPage(ctx, params)
	if err != nil || len(legacy) != 2 || legacy[0].UUID != toolResult.UUID || legacy[1].UUID != reply.UUID {
		t.Fatalf("page from legacy pending cursor = %+v, err=%v", legacy, err)
	}
	next, err := store.ClaimHarnessTurn(ctx, queued, HarnessTurnDelivery{}, "queued-hash", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := store.RecordHarnessModelEvents(ctx, next.Lease, next.CodeSession, HarnessModelStart, nil, nil)
	if err != nil || len(confirmed) != 1 || confirmed[0].UUID != queued.UUID || confirmed[0].ProcessedAt.IsZero() {
		t.Fatalf("confirmed input = %+v, err=%v", confirmed, err)
	}
	for _, order := range []string{"asc", "desc"} {
		params.Order, params.Cursor, params.Limit = order, nil, 1
		want := []string{ref.UUID, toolResult.UUID, reply.UUID, queued.UUID}
		if order == "desc" {
			slices.Reverse(want)
		}
		for i, id := range want {
			events, more, err := store.ListSessionEventsPage(ctx, params)
			if err != nil || len(events) != 1 || events[0].UUID != id || more != (i < len(want)-1) {
				t.Fatalf("%s page %d = %+v, want %s, more=%t, err=%v", order, i, events, id, more, err)
			}
			event := events[0]
			if id == queued.UUID && (!event.ProcessedAt.Equal(confirmed[0].ProcessedAt) || !event.CreatedAt.Equal(receivedAt)) {
				t.Fatalf("history changed the queued message times: %+v", event)
			}
			params.Cursor = &SessionEventPageCursor{ExternalID: event.ExternalID}
		}
	}
}
