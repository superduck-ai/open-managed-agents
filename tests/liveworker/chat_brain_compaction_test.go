//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const compactionFact = "OMA_COMPACTION_DURABLE_FACT"
const compactionChildPath = "/tmp/oma-compaction-child.txt"

func TestChatBrainCompaction(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	for _, child := range []bool{false, true} {
		name := "root"
		if child {
			name = "child"
		}
		t.Run(name, func(t *testing.T) { verifyBrainCompaction(t, started, name, child) })
	}
}

type compactionModel struct {
	t          *testing.T
	mu         sync.Mutex
	child      bool
	childID    string
	rootCalls  int
	actorCalls int
	summaries  int
}

type compactionRequest struct {
	Model     string `json:"model"`
	MaxTokens int    `json:"max_tokens"`
	Messages  []struct {
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

func (m *compactionModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var request compactionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		m.t.Error(err)
		w.WriteHeader(400)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var prompt []byte
	for _, message := range request.Messages {
		prompt = append(prompt, message.Content...)
	}
	if request.MaxTokens > 8192 {
		m.t.Errorf("wire output budget exceeds model limit: %d", request.MaxTokens)
		w.WriteHeader(400)
		return
	}
	if len(request.Messages) != 0 && bytes.Contains(request.Messages[len(request.Messages)-1].Content, []byte("You are now acting as a compaction engine")) {
		if !bytes.Contains(prompt, []byte(compactionFact)) {
			m.t.Error("summary input lost the original fact")
		}
		m.summaries++
		writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "Checkpoint: "+compactionFact+". Child wrote "+compactionChildPath+". Continue the requested task.", "", nil)
		return
	}
	if !m.child || request.Model == "claude-opus-4-6" {
		m.actorCalls++
		step := m.actorCalls
		if m.child && step == 1 {
			writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "", "Write", map[string]string{"file_path": compactionChildPath, "content": compactionFact})
			return
		}
		if m.child && step == 2 {
			writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "child file written", "", nil)
			return
		}
		first := 2
		if m.child {
			first = 3
		}
		if step <= first {
			writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "first turn done", "", nil)
			return
		}
		if !bytes.Contains(prompt, []byte("compacted-summary")) || !bytes.Contains(prompt, []byte(compactionFact)) {
			m.t.Error("ordinary request lost the compressed context")
		}
		if step > first+1 {
			writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "restored", "", nil)
			return
		}
		writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "compacted", "", nil)
		return
	}
	m.rootCalls++
	if m.rootCalls%2 == 0 {
		writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "coordinator complete", "", nil)
		return
	}
	message := "continue"
	if m.rootCalls == 3 {
		message = strings.Repeat("user-padding-", 12000)
	}
	args := map[string]string{"thread_id": m.childID, "message": message}
	if m.rootCalls == 1 {
		args = map[string]string{"agent_name": "compaction-child", "message": compactionFact + strings.Repeat("user-padding-", 12000)}
	}
	writeCompactionResponse(w, request.Model, max(1, len(prompt)/4), "", "send_to_agent", args)
}

func writeCompactionResponse(w http.ResponseWriter, model string, inputTokens int, text, tool string, args map[string]string) {
	w.Header().Set("Content-Type", "text/event-stream")
	write := func(kind string, fields map[string]any) {
		fields["type"] = kind
		encoded, _ := json.Marshal(fields)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, encoded)
		w.(http.Flusher).Flush()
	}
	write("message_start", map[string]any{"message": map[string]any{"id": "msg_" + uuid.NewString(), "type": "message", "role": "assistant", "model": model, "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": inputTokens, "output_tokens": 0}}})
	reason := "end_turn"
	if tool == "" {
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
	} else {
		reason = "tool_use"
		write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "call_" + uuid.NewString(), "name": tool, "input": map[string]any{}}})
		encoded, _ := json.Marshal(args)
		write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(encoded)}})
	}
	write("content_block_stop", map[string]any{"index": 0})
	write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
	write("message_stop", map[string]any{})
}

func verifyBrainCompaction(t *testing.T, started time.Time, name string, child bool) {
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	model := &compactionModel{t: t, child: child}
	upstream := httptest.NewServer(model)
	defer upstream.Close()
	providers := isolatedWorkerProviders(t, e)
	if len(providers) != 1 {
		t.Fatal("expected run-owned model provider")
	}
	input := isolatedProviderInput(upstream.URL)
	delete(input, "model_ids")
	rootWindow := 128000
	if child {
		rootWindow = 1000000
	}
	input["models"] = []map[string]any{
		{"id": "claude-sonnet-4-6", "context_window": rootWindow, "max_output_tokens": 8192},
		{"id": "claude-opus-4-6", "context_window": 128000, "max_output_tokens": 8192},
	}
	e.request(t, http.MethodPut, isolatedProviderPath(e)+"/"+providers[0].ID, "", input, 200, e.cookies...)
	parent := map[string]any{"version": e.agent.CurrentVersion, "model": map[string]string{"id": "claude-sonnet-4-6"},
		"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}}}
	if child {
		var agent struct {
			ID string `json:"id"`
		}
		requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{"name": "compaction-child", "description": "Compaction child", "system": "Preserve the task fact and use Write.", "model": map[string]string{"id": "claude-opus-4-6"}, "tools": parent["tools"]}, 200), &agent))
		t.Cleanup(func() {
			e.requestContext(context.Background(), t, "POST", "/v1/agents/"+agent.ID+"/archive", e.apiKey, nil, 200)
		})
		parent["multiagent"] = map[string]any{"type": "coordinator", "agents": []map[string]any{{"type": "agent", "id": agent.ID, "version": 1}}}
	}
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, parent, 200)
	_, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	node := startBrainNode(t, e, cfg, "compaction-"+name)
	defer node.stop(t)
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 5*time.Minute))
	defer cancel()
	rootEvents, closeRoot := connectChatStream(t, ctx, f)
	defer closeRoot()
	initial := compactionFact + strings.Repeat("user-padding-", 12000)
	if child {
		initial = "delegate to the configured child"
	}
	submitChat(t, f, initial)
	waitRealWorker(t, "Brain compaction session registered", func() bool {
		state, err := e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
		if err != nil || state.WorkerRegisteredAt == nil {
			return false
		}
		f.code = state
		return true
	})
	waitChatIdle(t, f, 1)
	drainCompactionIdle(t, ctx, rootEvents)
	threadID := ""
	events := rootEvents
	if child {
		threadID = compactionThread(t, f)
		model.mu.Lock()
		model.childID = threadID
		model.mu.Unlock()
		childEvents, closeChild := connectChatThreadStream(t, ctx, f, threadID)
		defer closeChild()
		events = childEvents
		sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(t.Context(), f.code.ExternalID)
		requireOK(t, err)
		output, err := exec.CommandContext(ctx, "docker", "exec", *sandbox.ProviderSandboxID, "cat", compactionChildPath).Output()
		requireOK(t, err)
		if string(output) != compactionFact {
			t.Fatalf("child file mismatch: %q", output)
		}
		chatProof(t, started, "child_mcp_verified")
	}
	answers := 3
	submitChat(t, f, strings.Repeat("user-padding-", 12000))
	waitChatIdle(t, f, 2)
	drainCompactionIdle(t, ctx, rootEvents)
	submitChat(t, f, "continue the existing conversation")
	notification := nextChatEvent(t, ctx, events, "agent.thread_context_compacted")
	waitChatIdle(t, f, answers)
	if child {
		drainCompactionIdle(t, ctx, rootEvents)
	}
	assertCompactionHistory(t, f, threadID, notification.ID)
	assertCompactionJournal(t, f, child)
	chatProof(t, started, name+"_compaction_notified")
	before, _, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	node.stop(t)
	submitChat(t, f, "resume after Brain restart, preserving the same child")
	waitRealWorker(t, "compaction owner lease expired", func() bool {
		state, _, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
		requireOK(t, err)
		return state.WorkerLeaseExpiresAt == nil || !state.WorkerLeaseExpiresAt.After(time.Now())
	}, 75*time.Second)
	replacement := startBrainNode(t, e, cfg, "compaction-"+name+"-replacement")
	defer replacement.stop(t)
	waitChatIdle(t, f, answers+1)
	after, _, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	if after.CurrentWorkerEpoch <= before.CurrentWorkerEpoch {
		t.Fatal("Brain replacement did not advance epoch")
	}
	assertCompactionHistory(t, f, threadID, notification.ID)
	if child && compactionThread(t, f) != threadID {
		t.Fatal("replacement created a different child thread")
	}
	model.mu.Lock()
	summaries := model.summaries
	actorCalls := model.actorCalls
	model.mu.Unlock()
	wantCalls := 4
	if child {
		wantCalls = 5
	}
	if summaries != 1 || actorCalls != wantCalls {
		t.Fatalf("unexpected summary/replay calls: summaries=%d actor=%d", summaries, actorCalls)
	}
	chatProof(t, started, name+"_compaction_recovered")
}

func drainCompactionIdle(t *testing.T, ctx context.Context, events <-chan chatEvent) {
	t.Helper()
	nextChatEvent(t, ctx, events, "session.status_idle")
}

func compactionThread(t *testing.T, f *liveSession) string {
	t.Helper()
	var threads struct {
		Data []struct {
			ID     string  `json:"id"`
			Parent *string `json:"parent_thread_id"`
		} `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", f.env.apiKey, nil, 200), &threads))
	var child string
	for _, thread := range threads.Data {
		if thread.Parent != nil {
			if child != "" {
				t.Fatal("duplicate child threads")
			}
			child = thread.ID
		}
	}
	if child == "" {
		t.Fatal("child thread missing")
	}
	return child
}

func assertCompactionHistory(t *testing.T, f *liveSession, threadID, notificationID string) {
	t.Helper()
	path := "/v1/sessions/" + f.session.ExternalID
	if threadID != "" {
		path += "/threads/" + threadID
	}
	var history struct {
		Data []struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Tokens *int   `json:"pre_compaction_tokens"`
		} `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", path+"/events?order=asc&limit=1000", f.env.apiKey, nil, 200), &history))
	count := 0
	for _, event := range history.Data {
		if event.Type == "agent.thread_context_compacted" {
			count++
			if event.ID != notificationID || event.Tokens == nil || *event.Tokens <= 0 {
				t.Fatalf("notification identity/pressure mismatch: %+v", event)
			}
		}
	}
	if count != 1 {
		t.Fatalf("compaction notifications=%d, want 1", count)
	}
}

func assertCompactionJournal(t *testing.T, f *liveSession, child bool) {
	t.Helper()
	events, _, err := f.env.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionExternalID: f.code.ExternalID, Subagents: child, Limit: 500})
	requireOK(t, err)
	var start, summary, end bool
	for _, event := range events {
		start = start || bytes.Contains(event.Payload, []byte(`"compaction/start"`))
		summary = summary || bytes.Contains(event.Payload, []byte(`"compaction/summary"`))
		end = end || bytes.Contains(event.Payload, []byte(`"compaction/end"`))
	}
	if !start || !summary || !end {
		t.Fatalf("durable compaction journal incomplete: start=%t summary=%t end=%t", start, summary, end)
	}
}

func TestChatThreadStreamSendsManagedBeta(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("anthropic-beta") != "managed-agents-2026-04-01" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": connected\n\n"))
		w.(http.Flusher).Flush()
	}))
	defer server.Close()
	f := &liveSession{env: &liveEnv{url: server.URL, apiKey: "test"}, session: db.Session{ExternalID: "sesn_test"}}
	_, closeStream := connectChatThreadStream(t, t.Context(), f, "sthr_test")
	closeStream()
}
