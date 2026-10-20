package codesessions

import (
	"context"
	"encoding/json"
	"testing"

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
			if len(sink.order) != 2 || sink.order[0] != "preview" || sink.order[1] != "buffered" || len(sink.publications) != 1 {
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
			var buffered map[string]any
			if err := json.Unmarshal(sink.events[0], &buffered); err != nil {
				t.Fatal(err)
			}
			if preview.Type != "event_start" || preview.Event.Type != "agent.thinking" || preview.Event.ID != buffered["id"] || preview.Thread != child || len(preview.Content) != 0 || p.workerEpoch != 7 || p.route.WorkspaceUUID != "workspace" || p.route.SessionExternalID != "session" || p.route.CodeSessionID != "cse_test" {
				t.Fatalf("invalid routed nonsecret preview: %s", p.payload)
			}
		})
	}
}
