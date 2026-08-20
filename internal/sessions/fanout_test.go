package sessions

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
)

func TestPublishSessionEventsGroupsEnvelopesBySession(t *testing.T) {
	bus := &recordingEventBus{}
	handler := &Handler{
		eventBus: bus,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	handler.publishSessionEvents(context.Background(), []db.SessionEvent{
		{SessionExternalID: "session-a", ExternalID: "event-a1"},
		{SessionExternalID: "session-b", ExternalID: "event-b1"},
		{SessionExternalID: "session-a", ExternalID: "event-a2"},
	})

	if got := len(bus.publications); got != 2 {
		t.Fatalf("publication count = %d, want 2", got)
	}
	assertSessionPublication(t, bus.publications[0], "session-a", []string{"event-a1", "event-a2"})
	assertSessionPublication(t, bus.publications[1], "session-b", []string{"event-b1"})
}

func TestSessionEventsFanoutUsesMinimalSSEContract(t *testing.T) {
	bus := &recordingEventBus{}
	handler := &Handler{
		eventBus: bus,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	threadID := "thread-test"
	threadUUID := "thread-uuid-test"
	deletedAt := time.Date(2026, time.August, 20, 1, 2, 3, 0, time.UTC)
	createdAt := deletedAt.Add(-time.Minute)
	processedAt := deletedAt.Add(-30 * time.Second)

	handler.publishSessionEvents(context.Background(), []db.SessionEvent{{
		UUID:              "event-uuid-test",
		ExternalID:        "event-test",
		OrganizationUUID:  "organization-test",
		WorkspaceUUID:     "workspace-test",
		SessionUUID:       "session-uuid-test",
		SessionExternalID: "session-test",
		ThreadUUID:        &threadUUID,
		ThreadExternalID:  &threadID,
		EventType:         "agent.message",
		Payload:           json.RawMessage(`{"type":"agent.message"}`),
		ProcessedAt:       processedAt,
		CreatedAt:         createdAt,
		DeletedAt:         &deletedAt,
	}})

	if len(bus.publications) != 1 {
		t.Fatalf("publication count = %d, want 1", len(bus.publications))
	}
	var rawPayload struct {
		Events []map[string]json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(bus.publications[0].envelope.Payload, &rawPayload); err != nil {
		t.Fatalf("decode publication payload: %v", err)
	}
	if len(rawPayload.Events) != 1 {
		t.Fatalf("publication event count = %d, want 1", len(rawPayload.Events))
	}
	wantFields := map[string]struct{}{
		"external_id": {}, "workspace_uuid": {}, "session_id": {}, "thread_id": {},
		"event_type": {}, "payload": {}, "processed_at": {}, "created_at": {},
	}
	if len(rawPayload.Events[0]) != len(wantFields) {
		t.Fatalf("fanout fields = %v, want exactly %v", rawPayload.Events[0], wantFields)
	}
	for field := range rawPayload.Events[0] {
		if _, ok := wantFields[field]; !ok {
			t.Fatalf("unexpected fanout field %q", field)
		}
	}

	var payload sessionEventsFanout
	if err := json.Unmarshal(bus.publications[0].envelope.Payload, &payload); err != nil {
		t.Fatalf("decode typed publication payload: %v", err)
	}
	event := payload.Events[0]
	if event.ExternalID != "event-test" || event.WorkspaceUUID != "workspace-test" || event.SessionExternalID != "session-test" {
		t.Fatalf("fanout event routing = %+v", event)
	}
	if event.ThreadExternalID == nil || *event.ThreadExternalID != threadID || event.EventType != "agent.message" {
		t.Fatalf("fanout event delivery fields = %+v", event)
	}
	if !event.CreatedAt.Equal(createdAt) || !event.ProcessedAt.Equal(processedAt) {
		t.Fatalf("fanout event times = created:%v processed:%v", event.CreatedAt, event.ProcessedAt)
	}
}

func TestPublishCodeSessionStreamEventsUsesProvidedRouteWithoutDatabase(t *testing.T) {
	bus := &recordingEventBus{}
	handler := &Handler{
		eventBus: bus,
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	route := codesessions.CodeSessionStreamRoute{
		CodeSessionID:     "cse-test",
		WorkspaceUUID:     "workspace-test",
		SessionExternalID: "session-test",
	}
	payloads := []json.RawMessage{json.RawMessage(`{"type":"stream_event"}`)}

	if err := handler.PublishCodeSessionStreamEvents(context.Background(), route, 7, payloads); err != nil {
		t.Fatalf("PublishCodeSessionStreamEvents() error = %v", err)
	}
	if len(bus.publications) != 1 || bus.publications[0].sessionID != route.SessionExternalID {
		t.Fatalf("publications = %#v, want one publication for %q", bus.publications, route.SessionExternalID)
	}
	var fanout codeSessionStreamFanout
	if err := json.Unmarshal(bus.publications[0].envelope.Payload, &fanout); err != nil {
		t.Fatalf("decode stream fanout: %v", err)
	}
	if fanout.CodeSessionID != route.CodeSessionID || fanout.WorkspaceUUID != route.WorkspaceUUID || fanout.SessionExternalID != route.SessionExternalID || fanout.WorkerEpoch != 7 || len(fanout.Payloads) != 1 {
		t.Fatalf("stream fanout = %#v, want supplied immutable route", fanout)
	}
}

type recordedPublication struct {
	sessionID string
	envelope  sessionfanout.Envelope
}

type recordingEventBus struct {
	publications []recordedPublication
}

func (b *recordingEventBus) Publish(_ context.Context, sessionID string, envelope sessionfanout.Envelope) error {
	b.publications = append(b.publications, recordedPublication{sessionID: sessionID, envelope: envelope})
	return nil
}

func (b *recordingEventBus) Subscribe(context.Context, string) error {
	return nil
}

func (b *recordingEventBus) Register(sessionfanout.Handler, func()) {}

func (b *recordingEventBus) Close() error {
	return nil
}

func assertSessionPublication(t *testing.T, publication recordedPublication, sessionID string, eventIDs []string) {
	t.Helper()
	if publication.sessionID != sessionID {
		t.Fatalf("publication session ID = %q, want %q", publication.sessionID, sessionID)
	}
	if publication.envelope.Kind != sessionfanout.KindSessionEvents {
		t.Fatalf("publication kind = %q, want %q", publication.envelope.Kind, sessionfanout.KindSessionEvents)
	}
	var payload sessionEventsFanout
	if err := json.Unmarshal(publication.envelope.Payload, &payload); err != nil {
		t.Fatalf("decode publication payload: %v", err)
	}
	if len(payload.Events) != len(eventIDs) {
		t.Fatalf("publication event count = %d, want %d", len(payload.Events), len(eventIDs))
	}
	for i, eventID := range eventIDs {
		if payload.Events[i].ExternalID != eventID {
			t.Fatalf("publication event %d ID = %q, want %q", i, payload.Events[i].ExternalID, eventID)
		}
		if payload.Events[i].SessionExternalID != sessionID {
			t.Fatalf("publication event %d session ID = %q, want %q", i, payload.Events[i].SessionExternalID, sessionID)
		}
	}
}
