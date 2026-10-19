package codesessions

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type brainModelEventSink struct {
	recordingWorkerStreamSink
	events []json.RawMessage
}

func (s *brainModelEventSink) PublishCodeSessionEvents(_ context.Context, _ db.CodeSession, events []json.RawMessage) error {
	s.events = append(s.events, events...)
	return nil
}

func TestBrainModelMessagesPrecedeRequestEnd(t *testing.T) {
	for _, errorType := range []string{"incomplete_response", ""} {
		t.Run(errorType, func(t *testing.T) {
			service := newTestService(t, nil)
			sink := &brainModelEventSink{}
			service.SetPublicEventSink(sink)
			request := &ModelRequest{StartID: "start", ThreadID: "thread", codeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"agent_runtime_mode":"brain"}}`)}}
			result := ModelRequestResult{EndedAt: time.Now(), ErrorType: errorType, EventIDs: []string{"thinking", "answer"}, Messages: []ModelRequestMessage{
				{ID: "thinking", Type: "agent.thinking", Content: []ModelRequestContent{{Type: "thinking", Thinking: new("reasoning")}}},
				{ID: "answer", Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("answer")}}},
			}}
			if err := service.EndModelRequest(t.Context(), request, result); err != nil {
				t.Fatal(err)
			}
			var types []string
			for _, raw := range sink.events {
				var event struct {
					Type     string `json:"type"`
					ThreadID string `json:"_owner_session_thread_id"`
					StartID  string `json:"model_request_start_id"`
				}
				if err := json.Unmarshal(raw, &event); err != nil {
					t.Fatal(err)
				}
				if event.ThreadID != request.ThreadID || event.StartID != request.StartID {
					t.Fatalf("lost request or thread identity: %+v", event)
				}
				types = append(types, event.Type)
			}
			want := []string{"span.model_request_end"}
			if errorType == "" {
				want = []string{"agent.thinking", "agent.message", "span.model_request_end"}
			}
			if !slices.Equal(types, want) {
				t.Fatalf("events = %v, want %v", types, want)
			}
		})
	}
}
