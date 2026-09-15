package sessions

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	server "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"
)

// TEST_NATS_URL optionally runs this same pipeline against the local Compose
// service. Unique session subjects keep it isolated from other applications.
func TestNATSFanoutDeliversPreviewAndFinalAcrossInstances(t *testing.T) {
	serverURL := os.Getenv("TEST_NATS_URL")
	if serverURL == "" {
		srv, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: -1, NoSigs: true})
		if err != nil {
			t.Fatal(err)
		}
		srv.Start()
		t.Cleanup(srv.Shutdown)
		if !srv.ReadyForConnections(3 * time.Second) {
			t.Fatal("NATS server not ready")
		}
		serverURL = srv.ClientURL()
	}
	publisher := newNATSFanoutHandler(t, serverURL)
	first := newNATSFanoutHandler(t, serverURL)
	second := newNATSFanoutHandler(t, serverURL)
	sessionID := "session_" + uuid.NewV4().String()
	var streams []<-chan streamDelivery
	for _, handler := range []*Handler{first, first, second} {
		_, stream := handler.streams.subscribe("workspace-test", sessionID)
		streams = append(streams, stream)
		if err := handler.eventBus.Subscribe(t.Context(), sessionID); err != nil {
			t.Fatal(err)
		}
	}
	_, otherWorkspace := first.streams.subscribe("workspace-other", sessionID)
	_, otherSession := first.streams.subscribe("workspace-test", sessionID+"-other")
	batch := previewTestBatch()
	payloads := []json.RawMessage{
		json.RawMessage(`{"type":"stream_event","event":{"type":"message_start","message":{"id":"msg_test"}},"session_id":"raw-session","uuid":"raw-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}},"session_id":"raw-session","uuid":"block-start"}`),
		json.RawMessage(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}},"session_id":"raw-session","uuid":"block-delta"}`),
	}
	batch.SessionExternalID = sessionID
	for _, payload := range payloads {
		batch.Payload = payload
		if err := publisher.publishFanout(t.Context(), sessionID, sessionfanout.KindCodeSessionStream, batch); err != nil {
			t.Fatal(err)
		}
	}
	threadID := "thread-test"
	publisher.publishSessionEvents(t.Context(), []db.SessionEvent{{
		ExternalID:        "event-final",
		WorkspaceUUID:     "workspace-test",
		SessionExternalID: sessionID,
		ThreadExternalID:  &threadID,
		EventType:         "agent.message",
		Payload:           json.RawMessage(`{"type":"agent.message","content":[{"type":"text","text":"hello"}]}`),
	}, {
		ExternalID:        "event-terminal",
		WorkspaceUUID:     "workspace-test",
		SessionExternalID: sessionID,
		ThreadExternalID:  &threadID,
		EventType:         "session.status_terminated",
		Payload:           json.RawMessage(`{"type":"session.status_terminated"}`),
	}})
	for i, stream := range streams {
		t.Run(fmt.Sprintf("connection-%d", i), func(t *testing.T) {
			connection := newStreamConnection("thread-test", true, map[string]struct{}{"agent.message": {}})
			response := httptest.NewRecorder()
			for _, eventType := range []string{previewEventStart, previewEventDelta, "agent.message", "session.status_terminated"} {
				select {
				case delivery := <-stream:
					event, accepted := connection.event(delivery)
					if !accepted || event.EventType != eventType {
						t.Fatalf("event = %s, accepted = %t, want %s", event.EventType, accepted, eventType)
					}
					writeSSE(response, event, "thread-test")
				case <-time.After(3 * time.Second):
					t.Fatalf("missing %s", eventType)
				}
			}
			if body := response.Body.String(); !strings.Contains(body, "event: event_delta\n") || !strings.Contains(body, `"text":"hello"`) {
				t.Fatalf("unexpected SSE response: %s", body)
			}
		})
	}
	for _, stream := range []<-chan streamDelivery{otherWorkspace, otherSession} {
		select {
		case delivery := <-stream:
			t.Fatalf("event leaked across workspace/session boundary: %#v", delivery)
		default:
		}
	}
}

func newNATSFanoutHandler(t *testing.T, serverURL string) *Handler {
	t.Helper()
	connection, err := nats.Connect(serverURL, nats.ReconnectBufSize(-1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	bus, err := sessionfanout.NewNATS(t.Context(), connection, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bus.Close() })
	return newFanoutTestHandler(bus)
}
