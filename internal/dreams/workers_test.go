package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestDreamInterruptEvent(t *testing.T) {
	event, err := dreamInterruptEvent(
		db.Dream{ExternalID: "drm_abc"},
		db.Session{ExternalID: "sesn_internal", OrganizationUUID: "org", WorkspaceUUID: "ws", UUID: "session-uuid"},
		time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("dreamInterruptEvent: %v", err)
	}
	if event.ExternalID != "sevt_cancel_abc" || event.EventType != "user.interrupt" {
		t.Fatalf("interrupt identity = (%q, %q)", event.ExternalID, event.EventType)
	}
	if !strings.Contains(string(event.Payload), `"type":"user.interrupt"`) {
		t.Fatalf("unexpected interrupt payload: %s", event.Payload)
	}
}

func TestDreamErrorTypeClassifiesTerminalFailures(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "retry exhausted transient error", err: errors.New("dial tcp: connection refused"), want: dreamErrorInternal},
		{name: "wrapped transient error", err: fmt.Errorf("renew claim: %w", errors.New("timeout")), want: dreamErrorInternal},
		{name: "untyped permanent error", err: permanentDreamError(errors.New("Dream output is incomplete")), want: dreamErrorInternal},
		{name: "input store unavailable", err: fmt.Errorf("prepare: %w", permanentDreamErrorOfType(dreamErrorInputMemoryStoreUnavailable, errors.New("archived"))), want: dreamErrorInputMemoryStoreUnavailable},
		{name: "input session unavailable", err: permanentDreamErrorOfType(dreamErrorInputSessionUnavailable, errors.New("deleted")), want: dreamErrorInputSessionUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := dreamErrorType(test.err); got != test.want {
				t.Fatalf("dreamErrorType() = %q, want %q", got, test.want)
			}
			if !isPermanentDreamError(test.err) && test.want != dreamErrorInternal {
				t.Fatal("typed Dream errors must be permanent")
			}
		})
	}
	raw, err := dreamErrorJSON(permanentDreamErrorOfType(dreamErrorInputSessionUnavailable, errors.New("input session sesn_x was deleted")))
	if err != nil {
		t.Fatal(err)
	}
	var persisted struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &persisted) != nil || persisted.Type != dreamErrorInputSessionUnavailable || persisted.Message != "input session sesn_x was deleted" {
		t.Fatalf("persisted Dream error = %s", raw)
	}
}

func TestDreamRunTimeoutExceeded(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	startedLongAgo := now.Add(-7 * time.Hour)
	startedRecently := now.Add(-5 * time.Hour)
	for _, test := range []struct {
		name    string
		dream   db.Dream
		timeout time.Duration
		want    bool
	}{
		{name: "not started yet cannot time out", dream: db.Dream{}, timeout: 6 * time.Hour},
		{name: "zero timeout disables the budget", dream: db.Dream{StartedAt: &startedLongAgo}, timeout: 0},
		{name: "within budget", dream: db.Dream{StartedAt: &startedRecently}, timeout: 6 * time.Hour},
		{name: "exactly at budget is not exceeded", dream: db.Dream{StartedAt: &startedRecently}, timeout: 5 * time.Hour},
		{name: "past budget", dream: db.Dream{StartedAt: &startedLongAgo}, timeout: 6 * time.Hour, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got, _ := dreamRunTimeoutExceeded(test.dream, test.timeout, now); got != test.want {
				t.Fatalf("dreamRunTimeoutExceeded() = %v, want %v", got, test.want)
			}
		})
	}
	if got := dreamErrorType(permanentDreamErrorOfType(dreamErrorTimeout, errors.New("budget"))); got != "timeout" {
		t.Fatalf("timeout error type = %q", got)
	}
}

func TestDreamCommandEventIsStableAndKeepsInstructions(t *testing.T) {
	instructions := "保留中文说明"
	event, err := dreamCommandEvent(
		db.Dream{ExternalID: "drm_abc", Instructions: &instructions},
		db.Session{ExternalID: "sesn_internal", OrganizationUUID: "org", WorkspaceUUID: "ws", UUID: "session-uuid"},
		time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("dreamCommandEvent: %v", err)
	}
	if event.ExternalID != "sevt_abc" || event.EventType != "user.message" {
		t.Fatalf("event identity = (%q, %q)", event.ExternalID, event.EventType)
	}
	for _, want := range []string{"/dream", "保留中文说明"} {
		if strings.Contains(string(event.Payload), want) {
			continue
		}
		t.Fatalf("unexpected command payload: %s", event.Payload)
	}
	for _, unwanted := range []string{"/mnt/memory", "/mnt/transcripts", "MEMORY.md"} {
		if strings.Contains(string(event.Payload), unwanted) {
			t.Fatalf("runtime path leaked into Dream command payload: %s", event.Payload)
		}
	}
}

func TestStartDreamCommitsRunningAndCommandBeforeQueueing(t *testing.T) {
	stub := newStartDreamStub(t)
	stub.runningWon = true
	if err := startDream(t.Context(), stub, stub, "worker-1", stub.dream); err != nil {
		t.Fatalf("startDream() = %v", err)
	}
	if got, want := strings.Join(stub.order, ","), "commit,queue"; got != want {
		t.Fatalf("startDream order = %s, want %s", got, want)
	}
}

func TestStartDreamDoesNotQueueWhenRunningCASLoses(t *testing.T) {
	stub := newStartDreamStub(t)
	stub.runningWon = false
	if err := startDream(t.Context(), stub, stub, "worker-1", stub.dream); err != nil {
		t.Fatalf("startDream() = %v, want nil after losing the running CAS", err)
	}
	if stub.queueCalls != 0 {
		t.Fatalf("QueuePublicSessionEvents calls = %d, want 0", stub.queueCalls)
	}
}

func TestStartDreamKeepsDurableRunningStateWhenDispatchFails(t *testing.T) {
	stub := newStartDreamStub(t)
	stub.runningWon = true
	stub.queueError = errors.New("queue unavailable")
	err := startDream(t.Context(), stub, stub, "worker-1", stub.dream)
	var dispatchErr *dreamDispatchError
	if !errors.As(err, &dispatchErr) {
		t.Fatalf("startDream() error = %v, want dreamDispatchError", err)
	}
	if got, want := strings.Join(stub.order, ","), "commit,queue"; got != want {
		t.Fatalf("startDream order = %s, want %s", got, want)
	}
}

func TestDreamRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 5 * time.Second},
		{attempt: 1, want: 5 * time.Second},
		{attempt: 2, want: 10 * time.Second},
		{attempt: 8, want: 10 * time.Minute},
		{attempt: 20, want: 10 * time.Minute},
	}
	for _, test := range tests {
		if got := dreamRetryDelay(test.attempt); got != test.want {
			t.Fatalf("dreamRetryDelay(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}

func newStartDreamStub(t *testing.T) *startDreamStub {
	t.Helper()
	now := time.Date(2026, time.September, 16, 0, 0, 0, 0, time.UTC)
	return &startDreamStub{
		dream: db.Dream{
			ExternalID:    "drm_abc",
			WorkspaceUUID: "ws-dream",
			Status:        "pending",
			Outputs:       json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
			CreatedAt:     now,
		},
		session: db.Session{
			UUID:             "session-uuid",
			ExternalID:       "sesn_internal",
			OrganizationUUID: "org",
			WorkspaceUUID:    "ws-dream",
			Status:           "idle",
		},
		store: db.MemoryStore{ExternalID: "memstore_out", WorkspaceUUID: "ws-dream"},
	}
}

type startDreamStub struct {
	dream      db.Dream
	session    db.Session
	store      db.MemoryStore
	runningWon bool
	order      []string
	queueCalls int
	queueError error
}

func (s *startDreamStub) GetSession(_ context.Context, _, _ string) (db.Session, bool, error) {
	return s.session, true, nil
}

func (s *startDreamStub) GetMemoryStore(_ context.Context, _, _ string) (db.MemoryStore, error) {
	return s.store, nil
}

func (s *startDreamStub) AppendSessionEventsIfAbsent(_ context.Context, _, _ string, events []db.SessionEvent) ([]db.SessionEvent, error) {
	return events, nil
}

func (s *startDreamStub) GetSessionEvent(_ context.Context, _, _, _ string) (db.SessionEvent, error) {
	return db.SessionEvent{}, db.ErrNotFound
}

func (s *startDreamStub) StartDream(_ context.Context, _ db.Dream, _, _ string, _ db.Session, event db.SessionEvent, _ time.Time) (db.Dream, db.SessionEvent, bool, error) {
	s.order = append(s.order, "commit")
	if !s.runningWon {
		return db.Dream{}, db.SessionEvent{}, false, nil
	}
	s.dream.Status = "running"
	return s.dream, event, true, nil
}

func (s *startDreamStub) QueuePublicSessionEvents(_ context.Context, _ db.Session, _ []db.SessionEvent) error {
	s.order = append(s.order, "queue")
	s.queueCalls++
	return s.queueError
}

func TestArchiveIfStoppedSkipsRunningSession(t *testing.T) {
	stub := &archiveStoreStub{session: db.Session{ExternalID: "sesn_internal", Status: "running"}}
	archiver := InternalSessionArchiver{store: stub}
	dream := db.Dream{
		WorkspaceUUID: "ws-dream",
		ExternalID:    "drm_archive",
		Status:        "canceled",
		Outputs:       json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
	}
	if err := archiver.ArchiveIfStopped(t.Context(), dream); err != nil {
		t.Fatalf("ArchiveIfStopped() = %v", err)
	}
	if stub.archiveCalls != 0 {
		t.Fatalf("ArchiveSession calls = %d, want 0", stub.archiveCalls)
	}
}

func TestArchiveIfStoppedArchivesIdleSession(t *testing.T) {
	stub := &archiveStoreStub{session: db.Session{ExternalID: "sesn_internal", Status: "idle", WorkspaceUUID: "ws-dream"}}
	archiver := InternalSessionArchiver{store: stub}
	dream := db.Dream{
		WorkspaceUUID: "ws-dream",
		ExternalID:    "drm_archive",
		Status:        "completed",
		Outputs:       json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
	}
	if err := archiver.ArchiveIfStopped(t.Context(), dream); err != nil {
		t.Fatalf("ArchiveIfStopped() = %v", err)
	}
	if stub.archiveCalls != 1 || stub.externalID != "sesn_internal" {
		t.Fatalf("ArchiveSession = (%d, %q)", stub.archiveCalls, stub.externalID)
	}
}

func TestArchiverRunOnceArchivesStoppedTerminalDreams(t *testing.T) {
	stub := &archiveStoreStub{
		dreams: []db.Dream{{
			WorkspaceUUID: "ws-dream",
			ExternalID:    "drm_canceled",
			Status:        "canceled",
			Outputs:       json.RawMessage(`[{"memory_store_id":"memstore_out","internal_session_id":"sesn_internal"}]`),
		}},
		session: db.Session{ExternalID: "sesn_internal", Status: "idle", WorkspaceUUID: "ws-dream"},
	}
	archiver := InternalSessionArchiver{store: stub}
	if err := archiver.RunOnce(t.Context()); err != nil {
		t.Fatalf("RunOnce() = %v", err)
	}
	if stub.archiveCalls != 1 {
		t.Fatalf("ArchiveSession calls = %d, want 1", stub.archiveCalls)
	}
}

type archiveStoreStub struct {
	dreams       []db.Dream
	session      db.Session
	missing      bool
	archiveCalls int
	externalID   string
}

func (s *archiveStoreStub) ListDreamsAwaitingInternalSessionArchive(context.Context, int) ([]db.Dream, error) {
	return s.dreams, nil
}

func (s *archiveStoreStub) GetSession(_ context.Context, _, _ string) (db.Session, bool, error) {
	if s.missing {
		return db.Session{}, false, nil
	}
	return s.session, true, nil
}

func (s *archiveStoreStub) ArchiveSession(_ context.Context, _, externalID string) (db.Session, error) {
	s.archiveCalls++
	s.externalID = externalID
	return s.session, nil
}

func TestTerminalSessionFailure(t *testing.T) {
	payload := func(value string) json.RawMessage { return json.RawMessage(value) }
	if got := terminalSessionFailure([]db.SessionEvent{{EventType: "span.model_request_end", Payload: payload(`{"is_error":true,"api_error_status":500}`)}}); got != "Dream model request failed with HTTP 500" {
		t.Fatalf("failed model request = %q", got)
	}
	if got := terminalSessionFailure([]db.SessionEvent{{EventType: "system.message", Payload: payload(`{"subtype":"api_retry","error":"server_error","attempt":9,"max_retries":10}`)}}); got != "" {
		t.Fatalf("retry before budget = %q", got)
	}
	if got := terminalSessionFailure([]db.SessionEvent{{EventType: "system.message", Payload: payload(`{"subtype":"api_retry","error":"server_error","attempt":10,"max_retries":10}`)}}); got != "Dream model request exhausted retries: server_error" {
		t.Fatalf("exhausted retry = %q", got)
	}
	if got := terminalSessionFailure([]db.SessionEvent{{EventType: "system.message", Payload: payload(`{"subtype":"api_error","error":"authentication_error"}`)}}); got != "Dream model request failed: authentication_error" {
		t.Fatalf("api error = %q", got)
	}
}

func TestLoadLifecycleEventsReadsPastFirstThousandEvents(t *testing.T) {
	base := time.Date(2026, time.September, 17, 0, 0, 0, 0, time.UTC)
	events := make([]db.SessionEvent, 0, 1001)
	for index := range 1000 {
		events = append(events, db.SessionEvent{
			UUID:      fmt.Sprintf("event-%04d", index),
			EventType: "span.model_request_start",
			CreatedAt: base.Add(time.Duration(index) * time.Millisecond),
		})
	}
	events = append(events, db.SessionEvent{
		UUID:      "event-1000",
		EventType: "system.message",
		Payload:   json.RawMessage(`{"subtype":"api_error","error":"late failure"}`),
		CreatedAt: base.Add(1000 * time.Millisecond),
	})
	store := &lifecycleEventStoreStub{events: events}
	loaded, err := loadLifecycleEvents(t.Context(), store, db.Dream{WorkspaceUUID: "ws"}, db.Session{ExternalID: "sesn_internal"})
	if err != nil {
		t.Fatalf("loadLifecycleEvents() = %v", err)
	}
	if len(loaded) != 1001 || store.calls != 3 {
		t.Fatalf("loaded events/calls = (%d, %d), want (1001, 3)", len(loaded), store.calls)
	}
	if got := terminalSessionFailure(loaded); got != "Dream model request failed: late failure" {
		t.Fatalf("terminalSessionFailure() = %q", got)
	}
}

type lifecycleEventStoreStub struct {
	events []db.SessionEvent
	calls  int
}

func TestKeepRuntimeOmitsTerminalCleanup(t *testing.T) {
	pending := NewPendingWorker(nil, nil, nil, nil, true, nil)
	if pending.archiver != nil {
		t.Fatal("keep_runtime pending worker must not archive internal Sessions")
	}
	running := NewRunningWorker(nil, nil, nil, time.Hour, true, nil)
	if running.reclaimer != nil || running.archiver != nil {
		t.Fatal("keep_runtime running worker must not reclaim or archive")
	}
}

func (s *lifecycleEventStoreStub) ListSessionEventsPage(_ context.Context, params db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	s.calls++
	start := 0
	if params.Cursor != nil {
		for index := range s.events {
			if s.events[index].UUID == params.Cursor.UUID {
				start = index + 1
				break
			}
		}
	}
	end := min(start+params.Limit, len(s.events))
	return s.events[start:end], end < len(s.events), nil
}
