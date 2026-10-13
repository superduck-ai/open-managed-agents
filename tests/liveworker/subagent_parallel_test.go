package liveworker

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
)

func verifyParallelConfiguredSubagents(t *testing.T, e *liveEnv, childID string, provider *chatDockerProvider) {
	t.Helper()
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	model := parallelSubagentModelFixture(t, childID, ready, release)
	configureChatModel(t, e, model.URL)
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	events, stop := connectChatStream(t, ctx, f)
	defer stop()
	submitChat(t, f, "Start two configured reviewers in the background.")
	for range 2 {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("two background reviewers did not overlap")
		}
	}
	var threads struct {
		Data []struct {
			ID       string  `json:"id"`
			ParentID *string `json:"parent_thread_id"`
			Agent    struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
				Model   struct {
					ID string `json:"id"`
				} `json:"model"`
			} `json:"agent"`
		} `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", e.apiKey, nil, 200), &threads))
	children := make(map[string]bool)
	for _, thread := range threads.Data {
		if thread.ParentID == nil {
			continue
		}
		if thread.Agent.ID != childID || thread.Agent.Version != 1 || thread.Agent.Model.ID != "claude-sonnet-4-6" {
			t.Fatalf("parallel child snapshot incomplete: %+v", thread)
		}
		children[thread.ID] = true
	}
	if len(children) != 2 {
		t.Fatalf("parallel child instances=%d, want two", len(children))
	}
	var err error
	f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	registerPublicSandboxCleanup(t, e, f)
	finish()
	confirmed := make(map[string]bool)
	for len(confirmed) < 2 {
		select {
		case event := <-events:
			if event.Type != "agent.tool_use" || event.Name != "write" {
				continue
			}
			if !children[event.ThreadID] || event.EvaluatedPermission != "ask" || confirmed[event.ThreadID] {
				t.Fatalf("parallel child policy mismatch: %+v", event)
			}
			var history struct {
				Data []chatEvent `json:"data"`
			}
			requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+event.ThreadID+"/events?types[]=agent.tool_use", e.apiKey, nil, 200), &history))
			if len(history.Data) != 1 || history.Data[0].ID == event.ID {
				t.Fatalf("child history did not expose an independent tool ID: %+v", history.Data)
			}
			confirmationID := history.Data[0].ID
			if len(confirmed) == 1 {
				confirmationID = event.ID
			}
			e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []map[string]string{{"type": "user.tool_confirmation", "tool_use_id": confirmationID, "result": "deny", "session_thread_id": event.ThreadID}}}, 200)
			confirmed[event.ThreadID] = true
		case <-ctx.Done():
			t.Fatal("parallel child permissions were not independently requested")
		}
	}
	waitRealWorker(t, "parallel child replies persisted", func() bool {
		complete := true
		for threadID := range children {
			var history struct {
				Data []chatEvent `json:"data"`
			}
			requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+threadID+"/events?types[]=agent.message", e.apiKey, nil, 200), &history))
			if len(history.Data) == 0 {
				complete = false
				continue
			}
			if len(history.Data) != 1 || len(history.Data[0].Content) != 1 || !strings.HasPrefix(history.Data[0].Content[0].Text, "parallel reply ") {
				t.Fatalf("parallel reply duplicated: %+v", history.Data)
			}
		}
		return complete
	})
	waitSubagentIdle(t, f)
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(ctx, f.code.ExternalID)
	requireOK(t, err)
	result, err := provider.RunCommand(ctx, *sandbox.ProviderSandboxID, e2bruntime.CommandRequest{Command: "test ! -e /mnt/user-data/outputs/parallel-A.txt && test ! -e /mnt/user-data/outputs/parallel-B.txt", Timeout: 10 * time.Second})
	requireOK(t, err)
	if result.ExitCode != 0 {
		t.Fatal("denied parallel Write produced a file")
	}
}

func parallelSubagentModelFixture(t *testing.T, childID string, ready chan<- struct{}, release <-chan struct{}) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			http.Error(w, "read failed", 400)
			return
		}
		call := calls.Add(1)
		child := strings.Contains(string(body), childSystem)
		label := "B"
		if strings.Contains(string(body), "PARALLEL_A") {
			label = "A"
		}
		firstChild := child && !strings.Contains(string(body), "toolu_parallel_write_")
		if firstChild {
			select {
			case ready <- struct{}{}:
			case <-r.Context().Done():
				return
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(kind string, fields map[string]any) {
			fields["type"] = kind
			data, err := jsonv2.Marshal(fields)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_parallel_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		tool := func(index int, id, name string, input any) {
			write("content_block_start", map[string]any{"index": index, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}})
			encoded, err := jsonv2.Marshal(input)
			if err != nil {
				t.Error(err)
				return
			}
			write("content_block_delta", map[string]any{"index": index, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(encoded)}})
			write("content_block_stop", map[string]any{"index": index})
		}
		reason := "end_turn"
		switch {
		case call == 1:
			reason = "tool_use"
			for index, name := range []string{"A", "B"} {
				tool(index, "toolu_parallel_"+name, "Agent", map[string]any{"description": "Parallel reviewer " + name, "prompt": "PARALLEL_" + name, "subagent_type": childID, "run_in_background": true})
			}
		case firstChild:
			reason = "tool_use"
			tool(0, "toolu_parallel_write_"+label, "Write", map[string]string{"file_path": "/mnt/user-data/outputs/parallel-" + label + ".txt", "content": "must be denied"})
		default:
			text := "parallel parent done"
			if child {
				if !strings.Contains(string(body), `"is_error":true`) {
					t.Error("parallel denial was not returned to child")
				}
				text = "parallel reply " + label
			}
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
			write("content_block_stop", map[string]any{"index": 0})
		}
		write("message_delta", map[string]any{"delta": map[string]string{"stop_reason": reason}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server
}
