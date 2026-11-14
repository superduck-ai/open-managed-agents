package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type publicationCounters struct {
	cursorReads    int
	cursorAdvances int
	mappingReads   int
	pageReads      int
	rowsRead       int
	publishCalls   int
	payloads       int
}

type fakeSubagentStore struct {
	advanceBlocks bool
	counters      publicationCounters
	cursor        int64
	events        []db.CodeSessionInternalEvent
	mappings      map[string]string
	publishOK     bool
	published     map[string]int
}

func newFakeSubagentStore() *fakeSubagentStore {
	return &fakeSubagentStore{mappings: map[string]string{}, publishOK: true, published: map[string]int{}}
}

func (f *fakeSubagentStore) SubagentPublicationProgress(context.Context, string, string) (db.SubagentPublicationProgress, error) {
	f.counters.cursorReads++
	return db.SubagentPublicationProgress{PublishedSequenceNum: f.cursor, CommittedSequenceNum: int64(len(f.events))}, nil
}

func (f *fakeSubagentStore) AdvanceSubagentPublishedSequence(ctx context.Context, _, _ string, sequenceNum int64) error {
	if f.advanceBlocks {
		<-ctx.Done()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.counters.cursorAdvances++
	f.cursor = max(f.cursor, sequenceNum)
	return nil
}

func (f *fakeSubagentStore) ListCodeSessionInternalEventsPage(_ context.Context, params db.ListCodeSessionInternalEventsPageParams) ([]db.CodeSessionInternalEvent, bool, error) {
	f.counters.pageReads++
	page := make([]db.CodeSessionInternalEvent, 0, params.Limit)
	for _, event := range f.events {
		if event.SequenceNum <= params.AfterSequence {
			continue
		}
		if len(page) == params.Limit {
			f.counters.rowsRead += len(page)
			return page, true, nil
		}
		page = append(page, event)
	}
	f.counters.rowsRead += len(page)
	return page, false, nil
}

func (f *fakeSubagentStore) append(agentID string, count int) {
	for range count {
		sequence := int64(len(f.events) + 1)
		agent := agentID
		f.events = append(f.events, db.CodeSessionInternalEvent{
			ExternalID:     fmt.Sprintf("csie_%d", sequence),
			SequenceNum:    sequence,
			EventType:      "assistant",
			IdempotencyKey: fmt.Sprintf("key_%d", sequence),
			AgentID:        &agent,
			CreatedAt:      time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
			Payload: json.RawMessage(fmt.Sprintf(
				`{"type":"assistant","uuid":"assistant-%d","message":{"role":"assistant","content":[{"type":"text","text":"step %d"}]}}`,
				sequence, sequence)),
		})
	}
}

func (f *fakeSubagentStore) publication() subagentPublication {
	return subagentPublication{
		cursor: f,
		events: f,
		mappings: func(context.Context, db.CodeSession) (map[string]string, error) {
			f.counters.mappingReads++
			return f.mappings, nil
		},
		publish: func(_ context.Context, _ db.CodeSession, payloads []json.RawMessage) error {
			f.counters.publishCalls++
			f.counters.payloads += len(payloads)
			if !f.publishOK {
				return errors.New("publish failed")
			}
			for _, raw := range payloads {
				var event struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &event); err != nil {
					return err
				}
				f.published[event.ID]++
			}
			return nil
		},
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		advanceTimeout: subagentCursorAdvanceTimeout,
	}
}

func (f *fakeSubagentStore) resetCounters() {
	f.counters = publicationCounters{}
}

func (f *fakeSubagentStore) catchUp(t *testing.T) {
	t.Helper()
	if err := f.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if f.cursor != int64(len(f.events)) {
		t.Fatalf("one reconcile did not catch up: cursor=%d committed=%d", f.cursor, len(f.events))
	}
}

var publicationCodeSession = db.CodeSession{ExternalID: "cse_publication", WorkspaceUUID: "workspace-publication"}

func TestSubagentPublicationKeepsCursorWhenPublishFails(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_a", 3)
	store.publishOK = false

	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err == nil {
		t.Fatal("reconcile succeeded although publish failed")
	}
	if store.cursor != 0 || store.counters.cursorAdvances != 0 {
		t.Fatalf("cursor advanced after failed publish: cursor=%d advances=%d", store.cursor, store.counters.cursorAdvances)
	}

	store.publishOK = true
	store.resetCounters()
	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if store.cursor != 3 || store.counters.payloads != 3 || len(store.published) != 3 {
		t.Fatalf("retry did not republish the failed batch: cursor=%d counters=%+v published=%d", store.cursor, store.counters, len(store.published))
	}
}

func TestSubagentPublicationBlockedCursorAdvanceTimesOut(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_a", 3)
	store.advanceBlocks = true
	publication := store.publication()
	publication.advanceTimeout = 20 * time.Millisecond

	done := make(chan error, 1)
	go func() { done <- publication.reconcile(t.Context(), publicationCodeSession) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("blocked advance error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("blocked cursor advance did not time out")
	}
	if store.cursor != 0 {
		t.Fatalf("cursor moved although advance timed out: %d", store.cursor)
	}

	store.advanceBlocks = false
	store.catchUp(t)
	if len(store.published) != 3 {
		t.Fatalf("retry after advance timeout published %d events, want 3", len(store.published))
	}
}

func TestSubagentPublicationHoldsUnmappedEventsUntilThreadMapping(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_a", 2)
	store.append("agent_late", 2)
	store.append("agent_a", 2)

	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if store.cursor != 2 || len(store.published) != 4 {
		t.Fatalf("unmapped events did not hold the cursor: cursor=%d published=%d", store.cursor, len(store.published))
	}

	store.mappings["agent_late"] = "sthr_late"
	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if store.cursor != 6 || len(store.published) != 6 {
		t.Fatalf("late mapping did not publish held events: cursor=%d published=%d", store.cursor, len(store.published))
	}
	for id, count := range store.published {
		if count > 2 {
			t.Fatalf("event %s published %d times", id, count)
		}
	}
}

func TestSubagentPublicationSkipsUnmappedEventsOutsideGraceWindow(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_orphan", 1)
	store.append("agent_a", int(subagentMappingGraceSequences))

	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	last := int64(len(store.events))
	if store.cursor != last {
		t.Fatalf("orphaned event outside grace window still holds cursor: cursor=%d want=%d", store.cursor, last)
	}

	store.resetCounters()
	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if store.counters.rowsRead != 0 || store.counters.publishCalls != 0 {
		t.Fatalf("skipped event is reread: %+v", store.counters)
	}
}

func TestSubagentPublicationStaleMappingDoesNotSkipLaterCommittedEvents(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_a", 1)
	publication := store.publication()
	publication.mappings = func(context.Context, db.CodeSession) (map[string]string, error) {
		staleSnapshot := map[string]string{"agent_a": "sthr_a"}
		store.mappings["late"] = "sthr_late"
		store.append("late", int(subagentMappingGraceSequences)+1)
		return staleSnapshot, nil
	}
	if err := publication.reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if store.cursor != 1 {
		t.Fatalf("stale snapshot scanned past its committed high-water mark: cursor=%d", store.cursor)
	}
	store.catchUp(t)
	want := int(subagentMappingGraceSequences) + 2
	if len(store.published) != want {
		t.Fatalf("mapped committed events lost: published=%d want=%d cursor=%d", len(store.published), want, store.cursor)
	}
}

func TestSubagentPublicationPublishesTailBeyondFirstPageInOneCall(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["child"] = "sthr_child"
	store.append("child", 1000)
	store.catchUp(t)
	store.cursor = 0
	store.append("child", 1)

	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if store.cursor != 1001 || len(store.published) != 1001 {
		t.Fatalf("tail beyond first page left unprojected: cursor=%d published=%d", store.cursor, len(store.published))
	}
}

func TestSubagentPublicationCancellationKeepsCommittedPageProgress(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["child"] = "sthr_child"
	store.append("child", 3*internalEventsPageSize)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	publication := store.publication()
	publish := publication.publish
	publication.publish = func(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) error {
		err := publish(ctx, codeSession, payloads)
		if store.counters.publishCalls == 2 {
			cancel()
		}
		return err
	}

	if err := publication.reconcile(ctx, publicationCodeSession); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled reconcile error = %v, want context.Canceled", err)
	}
	if store.cursor != 2*internalEventsPageSize {
		t.Fatalf("cursor after cancellation = %d, want end of second page %d", store.cursor, 2*internalEventsPageSize)
	}

	store.resetCounters()
	store.catchUp(t)
	if store.counters.pageReads != 1 || store.counters.rowsRead != internalEventsPageSize {
		t.Fatalf("resumed reconcile restarted before saved progress: %+v", store.counters)
	}
	if len(store.published) != 3*internalEventsPageSize {
		t.Fatalf("published=%d want=%d", len(store.published), 3*internalEventsPageSize)
	}
}

func TestSubagentPublicationUpgradeRepublishesUnprojectedTail(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_a", 700)
	store.catchUp(t)
	projectedPrefix := len(store.published)
	store.append("agent_a", 600)
	store.cursor = 0
	store.resetCounters()

	store.catchUp(t)
	if store.counters.pageReads != 3 || store.counters.cursorAdvances != 3 {
		t.Fatalf("catch-up from 0 counters = %+v, want 3 pages each followed by a cursor advance", store.counters)
	}
	if projectedPrefix != 700 || len(store.published) != 1300 {
		t.Fatalf("unprojected tail missing after upgrade: prefix=%d published=%d", projectedPrefix, len(store.published))
	}
	for id, count := range store.published {
		if count > 2 {
			t.Fatalf("event %s published %d times", id, count)
		}
	}
}

func TestSubagentPublicationFreshMappingPublishesAllCommittedEvents(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["late"] = "sthr_late"
	store.append("late", int(subagentMappingGraceSequences)+1)
	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	if len(store.published) != int(subagentMappingGraceSequences)+1 {
		t.Fatalf("published=%d want=%d", len(store.published), subagentMappingGraceSequences+1)
	}
}

func TestSubagentPublicationWorkIndependentOfHistory(t *testing.T) {
	const batch = 100
	results := map[int]publicationCounters{}
	for _, history := range []int{100, 1000} {
		store := newFakeSubagentStore()
		store.mappings["agent_a"] = "sthr_a"
		store.append("agent_a", history)
		store.catchUp(t)
		store.append("agent_a", batch)
		store.resetCounters()
		if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
			t.Fatal(err)
		}
		results[history] = store.counters
		t.Logf("incremental history=%d batch=%d counters=%+v", history, batch, store.counters)

		store.resetCounters()
		if err := store.publication().rebuild(t.Context(), publicationCodeSession); err != nil {
			t.Fatal(err)
		}
		t.Logf("full rebuild history=%d batch=%d counters=%+v", history, batch, store.counters)
		if store.counters.rowsRead != history+batch {
			t.Fatalf("full rebuild read %d rows, want %d", store.counters.rowsRead, history+batch)
		}
	}
	if results[100] != results[1000] {
		t.Fatalf("incremental reconcile work grows with history: %+v vs %+v", results[100], results[1000])
	}
	if got := results[1000]; got.rowsRead != batch || got.payloads != batch || got.pageReads != 1 || got.cursorAdvances != 1 {
		t.Fatalf("incremental reconcile work = %+v, want one page of %d rows", got, batch)
	}
}

func TestSubagentPublicationCaughtUpReconcileIsConstant(t *testing.T) {
	store := newFakeSubagentStore()
	store.mappings["agent_a"] = "sthr_a"
	store.append("agent_a", 1000)
	store.catchUp(t)
	store.resetCounters()
	if err := store.publication().reconcile(t.Context(), publicationCodeSession); err != nil {
		t.Fatal(err)
	}
	want := publicationCounters{cursorReads: 1}
	if store.counters != want {
		t.Fatalf("caught-up reconcile counters = %+v, want %+v", store.counters, want)
	}
}

func TestCoalescePublicActionsKeepsControlBoundaries(t *testing.T) {
	control := preparedControlAction{}
	stream := preparedStreamAction{payload: json.RawMessage(`{"stream":1}`)}
	actions := []preparedWorkerOutputEvent{
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"n":1}`)}},
		preparedNoopAction{},
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"n":2}`), json.RawMessage(`{"n":3}`)}},
		control,
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"n":4}`)}},
		stream,
		preparedKeepAliveAction{},
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"n":5}`)}},
	}
	got := coalescePublicActions(actions)
	if len(got) != 5 {
		t.Fatalf("coalesced actions = %#v", got)
	}
	sizes := []int{3, -1, 1, -1, 1}
	for index, size := range sizes {
		public, ok := got[index].(preparedPublicAction)
		if size < 0 {
			if ok {
				t.Fatalf("action %d merged across a boundary: %#v", index, got[index])
			}
			continue
		}
		if !ok || len(public.payloads) != size {
			t.Fatalf("action %d = %#v, want %d public payloads", index, got[index], size)
		}
	}
	first := actions[0].(preparedPublicAction)
	if len(first.payloads) != 1 {
		t.Fatalf("coalescing mutated the input action: %#v", first)
	}
}

func TestCoalescePublicActionsCommitsThreadCreatedBeforeLaterPayloads(t *testing.T) {
	created := json.RawMessage(`{"type":"session.thread_created","session_thread_id":"sthr_child","agent_id":"child"}`)
	actions := []preparedWorkerOutputEvent{
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"type":"agent.message","n":1}`)}},
		preparedPublicAction{payloads: []json.RawMessage{created}},
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"type":"agent.message","agent_id":"child"}`)}},
		preparedPublicAction{payloads: []json.RawMessage{json.RawMessage(`{"type":"agent.message","n":3}`)}},
	}
	got := coalescePublicActions(actions)
	sizes := []int{2, 2}
	if len(got) != len(sizes) {
		t.Fatalf("coalesced actions = %#v, want thread_created to end its publish", got)
	}
	for index, size := range sizes {
		public, ok := got[index].(preparedPublicAction)
		if !ok || len(public.payloads) != size {
			t.Fatalf("action %d = %#v, want %d public payloads", index, got[index], size)
		}
	}
	if string(got[0].(preparedPublicAction).payloads[1]) != string(created) {
		t.Fatalf("thread_created is not the last payload of its publish: %#v", got[0])
	}
}

func TestContainsThreadCreated(t *testing.T) {
	if containsThreadCreated([]json.RawMessage{json.RawMessage(`{"type":"agent.message"}`), json.RawMessage(`invalid`)}) {
		t.Fatal("payloads without thread_created reported a mapping change")
	}
	if !containsThreadCreated([]json.RawMessage{json.RawMessage(`{"type":"agent.message"}`), json.RawMessage(`{"type":"session.thread_created","session_thread_id":"sthr_a"}`)}) {
		t.Fatal("thread_created not detected")
	}
}
