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

func TestModelReplyPublicationOwnership(t *testing.T) {
	for _, transport := range []string{"claim", "nats"} {
		for _, errorType := range []string{"incomplete_response", ""} {
			t.Run(transport+"/"+errorType, func(t *testing.T) {
				service := newTestService(t, nil)
				sink := &brainModelEventSink{}
				service.SetPublicEventSink(sink)
				request := &ModelRequest{StartID: "start", ThreadID: "thread", codeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"worker_transport":"` + transport + `"}}`)}}
				result := ModelRequestResult{EndedAt: time.Now(), ErrorType: errorType, EventIDs: []string{"thinking", "answer"}, ToolUseIDs: []string{"tool"},
					Usage: ModelRequestUsage{InputTokens: new(int64(17)), OutputTokens: new(int64(7))},
					Messages: []ModelRequestMessage{
						{ID: "thinking", Type: "agent.thinking", Content: []ModelRequestContent{{Type: "thinking", Text: new("private reasoning")}}},
						{ID: "answer", Type: "agent.message", Content: []ModelRequestContent{{Type: "text", Text: new("answer")}}},
					}}
				if err := service.EndModelRequest(t.Context(), request, result); err != nil {
					t.Fatal(err)
				}
				var types []string
				for _, raw := range sink.events {
					var event struct {
						Type       string             `json:"type"`
						ThreadID   string             `json:"_owner_session_thread_id"`
						StartID    string             `json:"model_request_start_id"`
						EventIDs   []string           `json:"event_ids"`
						ToolUseIDs []string           `json:"tool_use_ids"`
						Usage      *ModelRequestUsage `json:"model_usage"`
					}
					if err := json.Unmarshal(raw, &event); err != nil {
						t.Fatal(err)
					}
					if event.ThreadID != request.ThreadID || event.StartID != request.StartID {
						t.Fatalf("lost request or thread identity: %+v", event)
					}
					if event.Type == "span.model_request_end" {
						if event.Usage == nil || event.Usage.InputTokens == nil || *event.Usage.InputTokens != 17 || event.Usage.OutputTokens == nil || *event.Usage.OutputTokens != 7 {
							t.Fatalf("lost model usage: %+v", event)
						}
						if !slices.Equal(event.ToolUseIDs, []string{"tool"}) {
							t.Fatalf("lost tool correlation: %+v", event)
						}
						if transport == "claim" && len(event.EventIDs) != 0 {
							t.Fatalf("proxy claimed Brain reply identities: %+v", event)
						}
					}
					types = append(types, event.Type)
				}
				want := []string{"span.model_request_end"}
				if transport != "claim" && errorType == "" {
					want = []string{"agent.thinking", "agent.message", "span.model_request_end"}
				}
				if !slices.Equal(types, want) {
					t.Fatalf("events = %v, want %v", types, want)
				}
			})
		}
	}
}

func TestBrainModelThinkingIsPublishedOnlyByWorker(t *testing.T) {
	service := newTestService(t, nil)
	sink := &brainModelEventSink{}
	service.SetPublicEventSink(sink)
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test", ThreadID: "thread", codeSession: db.CodeSession{Metadata: json.RawMessage(`{"config":{"worker_transport":"claim"}}`)}}
	if err := service.StartModelThinking(t.Context(), request, "summary_or_reply", 0); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 0 || len(sink.publications) != 0 {
		t.Fatalf("proxy published Brain thinking: %+v", sink)
	}
}

func TestModelThinkingStartHasStableIdentityAndNoContent(t *testing.T) {
	service := newTestService(t, nil)
	sink := &brainModelEventSink{}
	service.SetPublicEventSink(sink)
	request := &ModelRequest{StartID: "start", CodeSessionID: "cse_test", ThreadID: "thread"}
	for range 2 {
		if err := service.StartModelThinking(t.Context(), request, "msg_test", 0); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range sink.events {
		var event struct {
			ID       string          `json:"id"`
			Type     string          `json:"type"`
			StartID  string          `json:"model_request_start_id"`
			ThreadID string          `json:"_owner_session_thread_id"`
			Content  json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event.ID != "sevt_c271d655f0f2c7ab2979e46eea473cba" || event.Type != "agent.thinking" || event.StartID != "start" || event.ThreadID != "thread" || len(event.Content) != 0 {
			t.Fatalf("invalid thinking status: %s", raw)
		}
	}
}
