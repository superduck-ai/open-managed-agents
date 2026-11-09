package codesessions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type orderedThinkingSink struct {
	brainModelEventSink
	order []string
}

func (s *orderedThinkingSink) PublishCodeSessionStreamEvent(ctx context.Context, route CodeSessionStreamRoute, epoch int64, payload json.RawMessage) error {
	s.order = append(s.order, "preview")
	return s.recordingWorkerStreamSink.PublishCodeSessionStreamEvent(ctx, route, epoch, payload)
}
func (s *orderedThinkingSink) PublishCodeSessionEvents(ctx context.Context, session db.CodeSession, events []json.RawMessage) error {
	s.order = append(s.order, "buffered")
	return s.brainModelEventSink.PublishCodeSessionEvents(ctx, session, events)
}
func TestModelThinkingPreviewPrecedesBufferedRetirement(t *testing.T) {
	for _, child := range []string{"", "child-thread"} {
		t.Run("thread="+child, func(t *testing.T) {
			service := newTestService(t, nil)
			sink := &orderedThinkingSink{}
			service.SetPublicEventSink(sink)
			request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test", ThreadID: "owner-thread", toolThreadID: child, codeSession: db.CodeSession{ExternalID: "cse_test", WorkspaceUUID: "workspace", SessionExternalID: "session", CurrentWorkerEpoch: 7}}
			if err := service.StartModelThinking(t.Context(), request, "msg_test", 0); err != nil {
				t.Fatal(err)
			}
			if len(sink.order) != 1 || sink.order[0] != "preview" || len(sink.events) != 0 || len(sink.publications) != 1 {
				t.Fatalf("thinking preview was retired before publication: %v", sink.order)
			}
			p := sink.publications[0]
			var preview struct {
				Type   string `json:"type"`
				Thread string `json:"session_thread_id"`
				Event  struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"event"`
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(p.payload, &preview); err != nil {
				t.Fatal(err)
			}
			if preview.Type != "event_start" || preview.Event.Type != "agent.thinking" || preview.Event.ID == "" || preview.Thread != child || len(preview.Content) != 0 || p.workerEpoch != 7 || p.route.WorkspaceUUID != "workspace" || p.route.SessionExternalID != "session" || p.route.CodeSessionID != "cse_test" {
				t.Fatalf("invalid routed nonsecret preview: %s", p.payload)
			}
			if err := service.EndModelRequest(t.Context(), request, ModelRequestResult{EndedAt: time.Now().UTC(), Messages: []ModelRequestMessage{{ID: preview.Event.ID, Type: "agent.thinking"}}}); err != nil {
				t.Fatal(err)
			}
			var buffered struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			}
			if len(sink.events) != 2 {
				t.Fatalf("completion events=%d", len(sink.events))
			}
			if err := json.Unmarshal(sink.events[0], &buffered); err != nil {
				t.Fatal(err)
			}
			if buffered.ID != preview.Event.ID || buffered.Type != "agent.thinking" {
				t.Fatalf("completion=%+v", buffered)
			}
		})
	}
}

func TestDSRModelCompletionLeavesFinalMessagesToBrain(t *testing.T) {
	service := newTestService(t, nil)
	sink := &brainModelEventSink{}
	service.SetPublicEventSink(sink)
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test", ThreadID: "owner", codeSession: db.CodeSession{ExternalID: "cse_test", Metadata: json.RawMessage(`{"config":{"worker_protocol":"dsr1"}}`)}}
	result := ModelRequestResult{EndedAt: time.Now().UTC(), EventIDs: []string{"message", "thinking"}, Messages: []ModelRequestMessage{{ID: "message", Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("proxy answer")}}}, {ID: "thinking", Type: "agent.thinking"}}}
	if err := service.EndModelRequest(t.Context(), request, result); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("expected only span, got %d events", len(sink.events))
	}
	var span struct {
		Type     string   `json:"type"`
		EventIDs []string `json:"event_ids"`
	}
	if err := json.Unmarshal(sink.events[0], &span); err != nil {
		t.Fatal(err)
	}
	if span.Type != "span.model_request_end" || len(span.EventIDs) != 2 {
		t.Fatalf("span lost output references: %+v", span)
	}
}
