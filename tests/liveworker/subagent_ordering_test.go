package liveworker

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func verifySubagentLifecycleOrdering(t *testing.T, e *liveEnv) {
	t.Helper()
	f := e.newSessionWithSnapshot(t, json.RawMessage(`{"model":{"id":"claude-opus-4-6"},"multiagent":{"type":"coordinator","agents":[{"id":"agent_ordering","name":"ordering-reviewer","version":1,"model":{"id":"claude-sonnet-4-6"},"tools":[{"type":"agent_toolset_20260401","default_config":{"permission_policy":{"type":"always_ask"}},"configs":[{"name":"write","enabled":false}]}]}]}}`))
	post := func(payloads ...any) {
		t.Helper()
		events := make([]map[string]any, len(payloads))
		for i, payload := range payloads {
			events[i] = map[string]any{"payload": payload}
		}
		e.request(t, "POST", f.path("/worker/events"), f.token, map[string]any{"worker_epoch": "1", "events": events}, 200)
	}
	for _, task := range []string{"a", "b"} {
		post(map[string]string{"type": "system", "subtype": "task_started", "uuid": "start-" + task, "task_id": task, "tool_use_id": "agent-" + task, "description": "reviewer"})
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
	load := func() {
		t.Helper()
		requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", e.apiKey, nil, 200), &threads))
	}
	load()
	if len(threads.Data) != 3 {
		t.Fatalf("parallel lifecycle threads=%d, want three", len(threads.Data))
	}
	var forwarded atomic.Int32
	blockedModel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { forwarded.Add(1); w.WriteHeader(500) }))
	configureChatModel(t, e, blockedModel.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	request, err := http.NewRequestWithContext(ctx, "POST", e.url+"/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","max_tokens":100,"messages":[{"role":"user","content":"reply"}]}`))
	requireOK(t, err)
	request.Header.Set("Authorization", "Bearer "+f.modelToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Claude-Code-Agent-Id", "a")
	response, err := http.DefaultClient.Do(request)
	cancel()
	blockedModel.Close()
	if response != nil {
		response.Body.Close()
	}
	if !errors.Is(err, context.DeadlineExceeded) || forwarded.Load() != 0 {
		t.Fatalf("unresolved model dispatched: response=%v error=%v forwarded=%d", response, err, forwarded.Load())
	}
	for _, thread := range threads.Data {
		if thread.ParentID == nil {
			continue
		}
		post(map[string]any{"type": "control_request", "uuid": "pending-" + thread.ID, "request_id": "pending-" + thread.ID, "session_thread_id": thread.ID,
			"request": map[string]any{"subtype": "can_use_tool", "tool_name": "Write", "tool_use_id": "pending-write-" + thread.ID, "input": map[string]string{"file_path": "/tmp/must-not-write", "content": "denied"}}})
		var history struct {
			Data []chatEvent `json:"data"`
		}
		requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+thread.ID+"/events?types[]=agent.tool_use", e.apiKey, nil, 200), &history))
		if len(history.Data) != 1 || history.Data[0].EvaluatedPermission != "deny" {
			t.Fatalf("unresolved child policy must deny: %+v", history.Data)
		}
	}
	for _, task := range []string{"b", "a"} {
		post(map[string]any{"type": "control_request", "uuid": "tool-" + task, "request_id": "tool-" + task,
			"request": map[string]any{"subtype": "can_use_tool", "tool_name": "Agent", "tool_use_id": "agent-" + task, "input": map[string]string{"subagent_type": "ordering-reviewer"}}})
	}
	load()
	for _, thread := range threads.Data {
		if thread.ParentID != nil && (thread.Agent.ID != "agent_ordering" || thread.Agent.Version != 1 || thread.Agent.Model.ID != "claude-sonnet-4-6") {
			t.Fatalf("late tool did not repair child snapshot: %+v", thread)
		}
		if thread.ParentID == nil {
			continue
		}
		for _, tool := range []string{"Write", "Read"} {
			post(map[string]any{"type": "control_request", "uuid": tool + thread.ID, "request_id": tool + thread.ID, "session_thread_id": thread.ID,
				"request": map[string]any{"subtype": "can_use_tool", "tool_name": tool, "tool_use_id": tool + thread.ID, "input": map[string]string{"file_path": "/tmp/must-not-write"}}})
		}
		var history struct {
			Data []chatEvent `json:"data"`
		}
		requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+thread.ID+"/events?types[]=agent.tool_use", e.apiKey, nil, 200), &history))
		if len(history.Data) != 3 || history.Data[1].EvaluatedPermission != "deny" || history.Data[2].EvaluatedPermission != "ask" {
			t.Fatalf("repaired child lost deny/ask policy: %+v", history.Data)
		}
		for _, confirmation := range []map[string]string{
			{"type": "user.tool_confirmation", "tool_use_id": "sevt_unknown", "result": "deny", "session_thread_id": thread.ID},
			{"type": "user.tool_confirmation", "tool_use_id": history.Data[2].ID, "result": "deny"},
		} {
			e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []map[string]string{confirmation}}, 409)
		}
		for _, other := range threads.Data {
			if other.ParentID != nil && other.ID != thread.ID {
				e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []map[string]string{{"type": "user.tool_confirmation", "tool_use_id": history.Data[2].ID, "result": "deny", "session_thread_id": other.ID}}}, 409)
			}
		}
		var confirmations struct {
			Data []chatEvent `json:"data"`
		}
		requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?types[]=user.tool_confirmation", e.apiKey, nil, 200), &confirmations))
		if len(confirmations.Data) != 0 {
			t.Fatal("invalid confirmation was persisted")
		}
		requireOK(t, e.database.FillSessionThreadAgentSnapshot(t.Context(), f.code.WorkspaceUUID, f.session.ExternalID, thread.ID, json.RawMessage(`{"id":"wrong","version":2}`)))
		verifyLegacyToolProjection(t, e, f, thread.ID)
	}
	load()
	for _, thread := range threads.Data {
		if thread.ParentID != nil && (thread.Agent.ID != "agent_ordering" || thread.Agent.Version != 1) {
			t.Fatalf("repair overwrote pinned snapshot: %+v", thread)
		}
	}
	verifySubagentTranscriptDedup(t, f)
}

func verifyLegacyToolProjection(t *testing.T, e *liveEnv, f *liveSession, threadID string) {
	t.Helper()
	originalID := "sevt_legacy_" + threadID
	payload := map[string]any{"id": originalID, "type": "agent.tool_use", "name": "write", "evaluated_permission": "ask", "input": map[string]string{"file_path": "/tmp/legacy"}}
	appendEvent := func(id string, owner *string, object map[string]any) {
		t.Helper()
		raw, err := jsonv2.Marshal(object)
		requireOK(t, err)
		now := time.Now().UTC()
		_, err = e.database.AppendSessionEvents(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID, []db.SessionEvent{{UUID: uuid.NewV4().String(), ExternalID: id, EventType: "agent.tool_use", ThreadExternalID: owner, Payload: raw, CreatedAt: now, ProcessedAt: now}}, nil)
		requireOK(t, err)
	}
	appendEvent(originalID, &threadID, payload)
	var childHistory struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+threadID+"/events?types[]=agent.tool_use", e.apiKey, nil, 200), &childHistory))
	legacy := childHistory.Data[len(childHistory.Data)-1]
	if legacy.ID != originalID || legacy.PrimaryEventID == "" || legacy.PrimaryEventID == originalID {
		t.Fatalf("legacy history lost its identity: %+v", legacy)
	}
	payload["id"] = legacy.PrimaryEventID
	payload["session_thread_id"] = threadID
	appendEvent(legacy.PrimaryEventID, nil, payload)
	var rootHistory struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?types[]=agent.tool_use", e.apiKey, nil, 200), &rootHistory))
	if rootHistory.Data[len(rootHistory.Data)-1].ID != legacy.PrimaryEventID {
		t.Fatal("legacy history did not retain the original public mirror ID")
	}
}

func verifySubagentTranscriptDedup(t *testing.T, f *liveSession) {
	t.Helper()
	for _, workerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("worker-first=%t", workerFirst), func(t *testing.T) {
			release := make(chan struct{})
			finish := sync.OnceFunc(func() { close(release) })
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_order_%t\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n", workerFirst)
				w.(http.Flusher).Flush()
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ordering reply\"}}\n\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer func() { finish(); model.Close() }()
			configureChatModel(t, f.env, model.URL)
			if !workerFirst {
				finish()
			}
			request, err := http.NewRequestWithContext(t.Context(), "POST", f.env.url+"/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6","stream":true,"max_tokens":100,"messages":[{"role":"user","content":"reply"}]}`))
			requireOK(t, err)
			request.Header.Set("Authorization", "Bearer "+f.modelToken)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-Claude-Code-Agent-Id", "a")
			response, err := http.DefaultClient.Do(request)
			requireOK(t, err)
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("child proxy status=%d", response.StatusCode)
			}
			requestID := response.Header.Get("Request-Id")
			if !strings.HasPrefix(requestID, "sevt_") {
				t.Fatal("proxy response omitted request correlation")
			}
			echo := func() {
				t.Helper()
				f.env.request(t, "POST", f.path("/worker/internal-events"), f.token, map[string]any{"worker_epoch": "1", "events": []map[string]any{{"agent_id": "a", "payload": map[string]any{"type": "assistant", "uuid": fmt.Sprintf("echo-%t", workerFirst), "requestId": requestID, "message": map[string]any{"id": fmt.Sprintf("local-message-%t", workerFirst), "role": "assistant", "content": []map[string]string{{"type": "text", "text": "ordering reply"}}}}}}}, 200)
			}
			if workerFirst {
				echo()
				finish()
			}
			_, err = io.Copy(io.Discard, response.Body)
			requireOK(t, err)
			echo()
		})
	}
	var history struct {
		Data []chatEvent `json:"data"`
	}
	var primary struct {
		Data []struct {
			ThreadID string `json:"session_thread_id"`
			TaskID   string `json:"task_id"`
		} `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?types[]=session.thread_created", f.env.apiKey, nil, 200), &primary))
	threadID := ""
	for _, event := range primary.Data {
		if event.TaskID == "a" {
			threadID = event.ThreadID
		}
	}
	if threadID == "" {
		t.Fatal("child task mapping missing")
	}
	requireOK(t, jsonv2.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+threadID+"/events?types[]=agent.message", f.env.apiKey, nil, 200), &history))
	if len(history.Data) != 2 {
		t.Fatalf("two independent requests should have two replies, got %d: %+v", len(history.Data), history.Data)
	}
}
