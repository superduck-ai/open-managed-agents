package messages

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type modelCompletionSink struct {
	codesessions.PublicEventSink
	publish func([]json.RawMessage) error
}

func (s modelCompletionSink) PublishCodeSessionEvents(_ context.Context, _ db.CodeSession, events []json.RawMessage) error {
	return s.publish(events)
}

func TestModelProxyPublishesReplyAndEndTogether(t *testing.T) {
	for _, tc := range []struct {
		name, path, contentType, body string
	}{
		{"incomplete", "/v1/messages", "text/event-stream", "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\"}}\n\n"},
		{"messages", "/v1/messages", "text/event-stream", "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\"}}\n\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"answer\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_stop\"}\n\n"},
		{"chat", "/v1/chat/completions", "text/event-stream", "data: {\"id\":\"msg\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"},
		{"json", "/v1/messages", "application/json", `{"id":"msg","type":"message","content":[{"type":"text","text":"answer"}]}`},
	} {
		for _, purpose := range []string{"compaction", "session-title", ""} {
			t.Run(tc.name+"/"+purpose, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Content-Type", tc.contentType)
					_, _ = io.WriteString(w, tc.body)
				}))
				defer server.Close()
				service := &codesessions.Service{}
				response := httptest.NewRecorder()
				batches := 0
				service.SetPublicEventSink(modelCompletionSink{publish: func(events []json.RawMessage) error {
					batches++
					var types []string
					for _, raw := range events {
						var event struct {
							Type     string   `json:"type"`
							StartID  string   `json:"model_request_start_id"`
							EventIDs []string `json:"event_ids"`
						}
						if err := json.Unmarshal(raw, &event); err != nil {
							t.Fatal(err)
						}
						if event.StartID != "start" {
							t.Fatalf("lost request identity: %s", raw)
						}
						if purpose != "" && len(event.EventIDs) != 0 {
							t.Fatalf("auxiliary request exposed preview IDs: %s", raw)
						}
						types = append(types, event.Type)
					}
					want := []string{"span.model_request_end"}
					if purpose == "" && tc.name != "incomplete" {
						want = []string{"agent.message", "span.model_request_end"}
					}
					if !slices.Equal(types, want) {
						t.Fatalf("completion = %v, want %v", types, want)
					}
					if (tc.name == "messages" || tc.name == "chat") && response.Body.Len() != 0 {
						t.Fatal("same-read completion was published after forwarding to Brain")
					}
					return nil
				}})
				call := startModelCall(t.Context(), auth.Principal{}, "test")
				defer call.end()
				incoming := httptest.NewRequest(http.MethodPost, tc.path, nil).WithContext(call.ctx)
				incoming.Header.Set("X-OMA-Model-Purpose", purpose)
				upstream, err := http.NewRequestWithContext(call.ctx, http.MethodPost, server.URL, strings.NewReader("{}"))
				if err != nil {
					t.Fatal(err)
				}
				handler := &Handler{client: server.Client(), codeSessions: service, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
				handler.proxyModelRequest(response, incoming, upstream, &codesessions.ModelRequest{CodeSessionID: "cse_test", StartID: "start", ThreadID: "thread"}, call)
				if batches != 1 || response.Body.String() != tc.body {
					t.Fatalf("batches = %d, response changed = %v", batches, response.Body.String() != tc.body)
				}
			})
		}
	}
}

func TestObservationLimitPreservesUpstreamReply(t *testing.T) {
	text := strings.Repeat("x", 4*1024*1024+1)
	for _, streaming := range []bool{false, true} {
		body := `{"id":"large","type":"message","content":[{"type":"text","text":"` + text + `"}]}`
		if streaming {
			body = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"large\"}}\n\n" +
				"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" + text + "\"}}\n\n" +
				"data: {\"type\":\"message_stop\"}\n\n"
		}
		observation := &responseObservation{streaming: streaming, request: &codesessions.ModelRequest{CodeSessionID: "cse_large"}}
		forwarded, err := io.ReadAll(io.TeeReader(strings.NewReader(body), observation))
		if err != nil {
			t.Fatal(err)
		}
		observation.finish()
		if string(forwarded) != body || observation.result.ErrorType != "observation_limit" || len(observation.result.Messages) != 0 {
			t.Fatalf("streaming=%v: changed body=%v, error=%s, messages=%d", streaming, string(forwarded) != body, observation.result.ErrorType, len(observation.result.Messages))
		}
	}
}

func TestWorkspacePurposeHeaderHasNoCodeSessionAuditToSuppress(t *testing.T) {
	for _, path := range []string{"/v1/messages", "/v1/chat/completions"} {
		for _, purpose := range []string{"", "compaction", "session-title"} {
			t.Run(path+"/"+purpose, func(t *testing.T) {
				principal := auth.Principal{CredentialType: auth.CredentialTypeAPIKey, WorkspaceUUID: "workspace", OrganizationUUID: "organization", APIKeyUUID: "key"}
				incoming := httptest.NewRequest(http.MethodPost, path, nil)
				incoming.Header.Set("X-OMA-Model-Purpose", purpose)
				incoming.Header.Set("X-Claude-Code-Agent-Id", "foreign-thread")
				request, err := (&Handler{}).beginModelRequest(incoming, principal, "model")
				if err != nil || request != nil {
					t.Fatalf("workspace key created code-session audit: %v, %v", request, err)
				}
			})
		}
	}
}
