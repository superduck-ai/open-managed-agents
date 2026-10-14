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
)

func verifyParallelConfiguredSubagents(t *testing.T, e *liveEnv, childID, childName string) {
	t.Helper()
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	defer finish()
	model := parallelSubagentModelFixture(t, childID, childName, ready, release)
	configureChatModel(t, e, model.URL)
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
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
		} `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", e.apiKey, nil, 200), &threads))
	children := make(map[string]bool)
	for _, thread := range threads.Data {
		if thread.ParentID == nil {
			continue
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
	e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/archive", e.apiKey, nil, 200)
}

func parallelSubagentModelFixture(t *testing.T, childID, childName string, ready chan<- struct{}, release <-chan struct{}) *httptest.Server {
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
		if child {
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
		write := subagentEventWriter(t, w)
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
			if !strings.Contains(string(body), childName) || strings.Contains(string(body), childID) {
				t.Error("parent model must receive child name without OMA Agent ID")
			}
			reason = "tool_use"
			for index, name := range []string{"A", "B"} {
				tool(index, "toolu_parallel_"+name, "Agent", map[string]any{"description": "Parallel reviewer " + name, "prompt": "PARALLEL_" + name, "subagent_type": childName, "run_in_background": true})
			}
		default:
			text := "parallel parent done"
			if child {
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
