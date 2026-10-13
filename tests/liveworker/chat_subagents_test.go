package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/runtime/e2bruntime"
	skillsapi "github.com/superduck-ai/open-managed-agents/internal/skills"
)

const childSystem = "OMA_SUBAGENT_REVIEWER_V1"
const childAnswer = "子 Agent 已完成独立线程验证。"
const childSkillMarker = "OMA_CHILD_SKILL_PRELOADED"
const childToolPath = "/mnt/user-data/outputs/subagent-proof.txt"
const childFollowup = "请继续同一子 Agent，并保留之前的上下文。"
const childResumedAnswer = "子 Agent 在原线程继续完成了任务。"

func TestChatSubagents(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	verifySubagentLifecycleOrdering(t, e)
	chatProof(t, started, "snapshot_and_echo_order_verified")
	verifyUpdatedSelfReference(t, e)
	var mcpCalls atomic.Int32
	mcpURL := subagentMCPFixture(t, &mcpCalls)
	skillID, skillVersion := uploadSubagentSkill(t, e)
	for _, versions := range [][2]string{{skillVersion, "latest"}, {"latest", skillVersion}} {
		snapshot, err := jsonv2.Marshal(map[string]any{"skills": []map[string]string{
			{"type": "custom", "skill_id": skillID, "version": versions[0]},
			{"type": "custom", "skill_id": skillID, "version": versions[1]},
		}})
		requireOK(t, err)
		resolved, err := skillsapi.NewRuntimeResolver(e.database).ResolveAgentSnapshot(t.Context(), e.key.WorkspaceUUID.String(), snapshot)
		requireOK(t, err)
		if len(resolved) != 1 || resolved[0].Version != skillVersion || resolved[0].RequestedVersion != "latest" {
			t.Fatalf("explicit/latest aliases lost during deduplication: %+v", resolved)
		}
	}
	var child struct {
		ID string `json:"id"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{
		"name": "configured-reviewer", "description": "Review delegated work", "system": childSystem,
		"model":       map[string]string{"id": "claude-sonnet-4-6"},
		"skills":      []map[string]string{{"type": "custom", "skill_id": skillID, "version": skillVersion}},
		"mcp_servers": []map[string]string{{"name": "child_mcp", "type": "url", "url": mcpURL}},
		"tools":       []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"permission_policy": map[string]string{"type": "always_allow"}}, "configs": []any{map[string]any{"name": "write", "permission_policy": map[string]string{"type": "always_ask"}}}}, map[string]any{"type": "mcp_toolset", "mcp_server_name": "child_mcp", "configs": []any{map[string]any{"name": "verify", "permission_policy": map[string]string{"type": "always_allow"}}}}},
	}, 200), &child))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "POST", "/v1/agents/"+child.ID+"/archive", e.apiKey, nil, 200)
	})
	configureSubagentReferences(t, e, child.ID)
	f := createPublicChat(t, e)
	if !strings.Contains(string(f.session.AgentSnapshot), childSystem) {
		t.Fatal("public Session did not freeze the configured child definition")
	}
	childReady := make(chan []byte, 1)
	childRelease := make(chan struct{})
	release := sync.OnceFunc(func() { close(childRelease) })
	defer release()
	var calls atomic.Int32
	model := subagentModelFixture(t, child.ID, childReady, childRelease, &calls)
	configureChatModel(t, e, model.URL)
	provider, stopRunner := startPublicRunner(t, e)
	defer stopRunner()
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 90*time.Second))
	defer cancel()
	rootEvents, closeRoot := connectChatStream(t, ctx, f)
	defer closeRoot()
	submitChat(t, f, "请把任务委派给已配置的 reviewer。")
	var body []byte
	select {
	case body = <-childReady:
	case <-ctx.Done():
		t.Fatal("real Worker never dispatched the configured subagent")
	}
	if !strings.Contains(string(body), childSystem) || strings.Contains(string(body), "OMA_SUBAGENT_REVIEWER_V2") || strings.Contains(string(body), "OMA_SUBAGENT_REVIEWER_V3") {
		t.Fatal("child did not use the Session-pinned system prompt")
	}
	var modelRequest struct {
		Model string `json:"model"`
	}
	requireOK(t, jsonv2.Unmarshal(body, &modelRequest))
	if modelRequest.Model != "claude-sonnet-4-6" {
		t.Fatalf("child model=%s, want claude-sonnet-4-6", modelRequest.Model)
	}
	if !strings.Contains(string(body), childSkillMarker) {
		t.Fatal("child skill was not preloaded by the real Worker")
	}
	chatProof(t, started, "child_skill_preloaded")
	var err error
	f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	registerPublicSandboxCleanup(t, e, f)
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(ctx, f.code.ExternalID)
	requireOK(t, err)
	var threads struct {
		Data []struct {
			ID    string `json:"id"`
			Agent struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
				System  string `json:"system"`
			} `json:"agent"`
		} `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", e.apiKey, nil, 200), &threads))
	childThread := ""
	for _, thread := range threads.Data {
		if thread.Agent.ID == child.ID && thread.Agent.Version == 1 && thread.Agent.System == childSystem {
			childThread = thread.ID
		}
	}
	if childThread == "" || len(threads.Data) != 2 {
		t.Fatalf("threads do not identify the pinned child: %+v", threads)
	}
	chatProof(t, started, "configured_child_dispatched")
	childEvents, closeChild := connectChatThreadStream(t, ctx, f, childThread)
	defer closeChild()
	release()
	for _, decision := range []string{"deny", "allow"} {
		tool := waitSubagentTool(t, ctx, rootEvents, childThread)
		result, err := provider.RunCommand(ctx, *sandbox.ProviderSandboxID, e2bruntime.CommandRequest{Command: "test ! -e " + childToolPath, Timeout: 10 * time.Second})
		requireOK(t, err)
		if result.ExitCode != 0 {
			t.Fatal("child tool executed before approval")
		}
		var history struct {
			Data []chatEvent `json:"data"`
		}
		requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+childThread+"/events?types[]=agent.tool_use", e.apiKey, nil, 200), &history))
		confirmationID := ""
		for _, event := range history.Data {
			if event.Name == "write" {
				confirmationID = event.ID
			}
		}
		if confirmationID == "" || confirmationID == tool.ID {
			t.Fatal("child Write history did not expose its own tool event ID")
		}
		e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []any{map[string]string{"type": "user.tool_confirmation", "tool_use_id": confirmationID, "result": decision, "session_thread_id": childThread}}}, 200)
		e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []any{map[string]string{"type": "user.tool_confirmation", "tool_use_id": confirmationID, "result": decision, "session_thread_id": childThread}}}, 409)
	}
	childFinal := waitSubagentText(t, ctx, childEvents, childAnswer)
	if childFinal.ThreadID != childThread {
		t.Fatal("child stream routed to wrong thread")
	}
	chatProof(t, started, "child_stream_received")
	waitSubagentText(t, ctx, rootEvents, chatAnswer)
	result, err := provider.RunCommand(ctx, *sandbox.ProviderSandboxID, e2bruntime.CommandRequest{Command: "cat " + childToolPath, Timeout: 10 * time.Second})
	requireOK(t, err)
	if result.ExitCode != 0 || string(result.Stdout) != "child tool verified" {
		t.Fatal("approved child Write did not produce the expected file")
	}
	chatProof(t, started, "child_tool_policy_verified")
	if mcpCalls.Load() != 1 {
		t.Fatalf("child MCP executions=%d, want one", mcpCalls.Load())
	}
	chatProof(t, started, "child_mcp_executed")
	waitSubagentIdle(t, f)
	var history struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+childThread+"/events", e.apiKey, nil, 200), &history))
	found := false
	toolResults := 0
	for _, event := range history.Data {
		if event.ID == childFinal.ID && event.ThreadID == childThread {
			found = true
		}
		if event.Type == "agent.tool_result" {
			if event.ThreadID != childThread || event.ToolUseID == "" {
				t.Fatal("child tool result is missing its thread or tool identity")
			}
			toolResults++
		}
		if event.Type == "user.message" {
			for _, content := range event.Content {
				if content.Type == "tool_result" {
					t.Fatal("child history exposed a provider tool result as a user message")
				}
			}
		}
	}
	if !found {
		t.Fatal("child SSE message missing from thread history")
	}
	if toolResults < 3 {
		t.Fatalf("child tool results=%d, want denied Write, approved Write and MCP", toolResults)
	}
	assertSubagentSingleReply(t, e, f, childThread, childAnswer)
	if calls.Load() < 6 {
		t.Fatalf("model calls=%d, want at least parent, four child calls, parent", calls.Load())
	}
	chatProof(t, started, "child_thread_history_verified")
	submitChat(t, f, childFollowup)
	resumed := waitSubagentText(t, ctx, childEvents, childResumedAnswer)
	if resumed.ThreadID != childThread {
		t.Fatal("resumed subagent created a different public thread")
	}
	waitSubagentIdle(t, f)
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", e.apiKey, nil, 200), &threads))
	if len(threads.Data) != 2 {
		t.Fatalf("follow-up created %d threads, want two", len(threads.Data))
	}
	chatProof(t, started, "child_thread_resumed")
	assertSubagentSingleReply(t, e, f, childThread, childResumedAnswer)
	verifyParallelConfiguredSubagents(t, e, child.ID, provider)
	chatProof(t, started, "parallel_instances_verified")
}

func assertSubagentSingleReply(t *testing.T, e *liveEnv, f *liveSession, threadID, text string) {
	t.Helper()
	var history struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads/"+threadID+"/events?types[]=agent.message", e.apiKey, nil, 200), &history))
	count := 0
	for _, event := range history.Data {
		if len(event.Content) == 1 && event.Content[0].Text == text {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("child reply count=%d, want one", count)
	}
}

func verifyUpdatedSelfReference(t *testing.T, e *liveEnv) {
	t.Helper()
	var parent struct {
		ID string `json:"id"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{
		"name": "self-v1", "system": "self-v1", "model": "claude-sonnet-4-6",
		"multiagent": map[string]any{"type": "coordinator", "agents": []map[string]string{{"type": "self"}}},
	}, 200), &parent))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "POST", "/v1/agents/"+parent.ID+"/archive", e.apiKey, nil, 200)
	})
	e.request(t, "POST", "/v1/agents/"+parent.ID, e.apiKey, map[string]any{"version": 1, "name": "self-v2", "system": "self-v2"}, 200)
	var nested struct {
		ID string `json:"id"`
	}
	requireOK(t, jsonv2.Unmarshal(e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{
		"name": "nested-self-review", "model": "claude-sonnet-4-6", "multiagent": map[string]any{"type": "coordinator", "agents": []map[string]any{{"type": "agent", "id": parent.ID, "version": 1}}},
	}, 200), &nested))
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "POST", "/v1/agents/"+nested.ID+"/archive", e.apiKey, nil, 200)
	})
	for _, kind := range []string{"sessions", "deployments"} {
		body := map[string]any{"agent": nested.ID, "environment_id": e.environment.ExternalID}
		if kind == "deployments" {
			body["name"] = "self-review"
			body["initial_events"] = []map[string]any{{"type": "user.message", "content": []map[string]string{{"type": "text", "text": "self review"}}}}
		}
		e.request(t, "POST", "/v1/"+kind, e.apiKey, body, 400)
		body["agent"] = parent.ID
		var resource struct {
			ID    string `json:"id"`
			Agent struct {
				Version    int `json:"version"`
				Multiagent struct {
					Agents []struct {
						ID         string          `json:"id"`
						Version    int             `json:"version"`
						System     string          `json:"system"`
						Multiagent json.RawMessage `json:"multiagent"`
					} `json:"agents"`
				} `json:"multiagent"`
			} `json:"agent"`
		}
		requireOK(t, jsonv2.Unmarshal(e.request(t, "POST", "/v1/"+kind, e.apiKey, body, 200), &resource))
		cleanup := sync.OnceFunc(func() {
			e.requestContext(context.Background(), t, "POST", "/v1/"+kind+"/"+resource.ID+"/archive", e.apiKey, nil, 200)
			if kind == "sessions" {
				e.requestContext(context.Background(), t, "DELETE", "/v1/sessions/"+resource.ID, e.apiKey, nil, 200)
			}
		})
		t.Cleanup(cleanup)
		if kind == "deployments" {
			stored, err := e.database.GetDeployment(t.Context(), e.key.WorkspaceUUID.String(), resource.ID)
			requireOK(t, err)
			requireOK(t, jsonv2.Unmarshal(stored.AgentSnapshot, &resource.Agent))
		}
		children := resource.Agent.Multiagent.Agents
		if resource.Agent.Version != 2 || len(children) != 1 || children[0].ID != parent.ID || children[0].Version != 1 || children[0].System != "self-v1" || string(children[0].Multiagent) != "null" {
			t.Fatalf("%s did not preserve historical self configuration: %+v", kind, resource.Agent)
		}
		cleanup()
	}
}

func configureSubagentReferences(t *testing.T, e *liveEnv, childID string) {
	t.Helper()
	latest := map[string]any{"type": "coordinator", "agents": []map[string]any{{"type": "agent", "id": childID}}}
	pinned := map[string]any{"type": "coordinator", "agents": []map[string]any{{"type": "agent", "id": childID, "version": 1}}}
	for _, ref := range []map[string]any{
		{"type": "agent", "id": "agent_missing"},
		{"type": "agent", "id": childID, "version": 0},
		{"type": "agent", "id": childID, "version": 999},
	} {
		e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{"name": "invalid-reference", "model": "claude-sonnet-4-6", "multiagent": map[string]any{"type": "coordinator", "agents": []map[string]any{ref}}}, 400)
	}
	latestID := requireSubagentReference(t, e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{"name": "reference-latest", "model": "claude-sonnet-4-6", "multiagent": latest}, 200), childID, 1)
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "POST", "/v1/agents/"+latestID+"/archive", e.apiKey, nil, 200)
	})
	e.request(t, "POST", "/v1/agents/"+childID, e.apiKey, map[string]any{"version": 1, "system": "OMA_SUBAGENT_REVIEWER_V2"}, 200)
	pinnedID := requireSubagentReference(t, e.request(t, "POST", "/v1/agents", e.apiKey, map[string]any{"name": "reference-pinned", "model": "claude-sonnet-4-6", "multiagent": pinned}, 200), childID, 1)
	t.Cleanup(func() {
		e.requestContext(context.Background(), t, "POST", "/v1/agents/"+pinnedID+"/archive", e.apiKey, nil, 200)
	})
	requireSubagentReference(t, e.request(t, "GET", "/v1/agents/"+latestID, e.apiKey, nil, 200), childID, 1)
	requireSubagentReference(t, e.request(t, "POST", "/v1/agents/"+latestID, e.apiKey, map[string]any{"version": 1, "multiagent": latest}, 200), childID, 2)
	requireSubagentReference(t, e.request(t, "POST", "/v1/agents/"+pinnedID, e.apiKey, map[string]any{"version": 1, "multiagent": pinned}, 200), childID, 1)
	e.request(t, "POST", "/v1/agents/"+childID, e.apiKey, map[string]any{"version": 2, "system": "OMA_SUBAGENT_REVIEWER_V3"}, 200)
	requireSubagentReference(t, e.request(t, "GET", "/v1/agents/"+latestID, e.apiKey, nil, 200), childID, 2)
	requireSubagentReference(t, e.request(t, "GET", "/v1/agents/"+pinnedID, e.apiKey, nil, 200), childID, 1)
	requireSubagentReference(t, e.request(t, "POST", "/v1/agents/"+latestID, e.apiKey, map[string]any{"version": 2, "name": "reference-renamed"}, 200), childID, 2)
	requireSubagentReference(t, e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": 1, "multiagent": pinned,
		"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200), childID, 1)
}

func requireSubagentReference(t *testing.T, raw []byte, childID string, version int) string {
	t.Helper()
	var agent struct {
		ID         string `json:"id"`
		Multiagent struct {
			Type   string `json:"type"`
			Agents []struct {
				Type    string `json:"type"`
				ID      string `json:"id"`
				Version int    `json:"version"`
			} `json:"agents"`
		} `json:"multiagent"`
	}
	requireOK(t, jsonv2.Unmarshal(raw, &agent))
	if agent.Multiagent.Type != "coordinator" || len(agent.Multiagent.Agents) != 1 {
		t.Fatalf("unexpected subagent roster: %+v", agent.Multiagent)
	}
	ref := agent.Multiagent.Agents[0]
	if ref.Type != "agent" || ref.ID != childID || ref.Version != version {
		t.Fatalf("subagent reference=%+v, want agent %s@%d", ref, childID, version)
	}
	return agent.ID
}

func waitSubagentIdle(t *testing.T, f *liveSession) {
	t.Helper()
	waitRealWorker(t, "all subagent threads idle and delivery drained", func() bool {
		threads, err := f.env.database.ListSessionThreads(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
		requireOK(t, err)
		for _, thread := range threads {
			if thread.Status != "idle" {
				return false
			}
		}
		code, found, err := f.env.database.GetCodeSession(t.Context(), f.code.ExternalID)
		requireOK(t, err)
		if !found || code.WorkerStatus == "running" || code.WorkerStatus == "requires_action" {
			return false
		}
		var session struct {
			Status string `json:"status"`
		}
		requireOK(t, jsonv2.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID, f.env.apiKey, nil, 200), &session))
		if session.Status != "idle" {
			return false
		}
		queue := f.consumer(t)
		return queue.NumPending == 0 && queue.NumAckPending == 0
	})
}

func waitSubagentTool(t *testing.T, ctx context.Context, events <-chan chatEvent, threadID string) chatEvent {
	t.Helper()
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("SSE closed before child permission request")
			}
			if event.Type == "agent.tool_use" && event.Name == "write" {
				if event.ThreadID != threadID || event.EvaluatedPermission != "ask" {
					t.Fatalf("child permission used the parent policy: %+v", event)
				}
				return event
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for child permission request")
		}
	}
}

func uploadSubagentSkill(t *testing.T, e *liveEnv) (string, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files[]", "subagent-proof/SKILL.md")
	requireOK(t, err)
	_, err = io.WriteString(part, "---\nname: subagent-proof\ndescription: A child verification skill\n---\n\n"+childSkillMarker)
	requireOK(t, err)
	requireOK(t, writer.Close())
	request, err := http.NewRequestWithContext(t.Context(), "POST", e.url+"/v1/skills?beta=true", &body)
	requireOK(t, err)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+e.apiKey)
	request.Header.Set("anthropic-beta", "skills-2025-10-02")
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	requireOK(t, err)
	if response.StatusCode != 200 {
		t.Fatalf("skill upload status=%d: %s", response.StatusCode, data)
	}
	var skill struct {
		ID            string `json:"id"`
		LatestVersion string `json:"latest_version"`
	}
	requireOK(t, jsonv2.Unmarshal(data, &skill))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, "DELETE", e.url+"/v1/skills/"+skill.ID+"?beta=true", nil)
		if err != nil {
			t.Error(err)
			return
		}
		request.Header.Set("Authorization", "Bearer "+e.apiKey)
		request.Header.Set("anthropic-beta", "skills-2025-10-02")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Error(err)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Errorf("skill cleanup status=%d", response.StatusCode)
		}
	})
	return skill.ID, skill.LatestVersion
}

func waitSubagentText(t *testing.T, ctx context.Context, events <-chan chatEvent, text string) chatEvent {
	t.Helper()
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("SSE closed before subagent response")
			}
			if event.Type == "agent.message" && len(event.Content) == 1 && event.Content[0].Text == text {
				return event
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for thread response")
		}
	}
}

func subagentModelFixture(t *testing.T, childID string, ready chan<- []byte, release <-chan struct{}, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	var childCalls atomic.Int32
	runtimeID := regexp.MustCompile(`agentId: ([a-zA-Z0-9_-]+)`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			http.Error(w, "read failed", 400)
			return
		}
		call := calls.Add(1)
		isChild := strings.Contains(string(body), childSystem)
		childCall := int32(0)
		if isChild {
			childCall = childCalls.Add(1)
			if childCall == 1 {
				select {
				case ready <- body:
				case <-r.Context().Done():
					return
				}
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			if childCall > 4 && !strings.Contains(string(body), childAnswer) {
				t.Error("resumed child model lost prior context")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(kind string, fields map[string]any) {
			fields["type"] = kind
			data, _ := jsonv2.Marshal(fields)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_subagent_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		reason := "end_turn"
		if isChild && childCall <= 2 {
			reason = "tool_use"
			if childCall == 2 && !strings.Contains(string(body), `"is_error":true`) {
				t.Error("denied child tool was not returned to its model as an error")
			}
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("toolu_child_write_%d", childCall), "name": "Write", "input": map[string]any{}}})
			input, _ := jsonv2.Marshal(map[string]string{"file_path": childToolPath, "content": "child tool verified"})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
		} else if isChild && childCall == 3 {
			reason = "tool_use"
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_child_mcp", "name": "mcp__child_mcp__verify", "input": map[string]any{}}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": "{}"}})
		} else if !isChild && strings.Contains(string(body), childFollowup) && !strings.Contains(string(body), "toolu_resume_child") {
			reason = "tool_use"
			matches := runtimeID.FindSubmatch(body)
			if len(matches) != 2 {
				t.Error("parent model did not receive a resumable runtime Agent ID")
				http.Error(w, "missing runtime identity", 500)
				return
			}
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_resume_child", "name": "SendMessage", "input": map[string]any{}}})
			input, _ := jsonv2.Marshal(map[string]string{"to": string(matches[1]), "message": childFollowup, "summary": "Resume configured child"})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "input_json_delta", "partial_json": string(input)}})
		} else if call == 1 {
			reason = "tool_use"
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_configured_child", "name": "Agent", "input": map[string]any{}}})
			input, _ := jsonv2.Marshal(map[string]any{"description": "Review configured child", "prompt": "Reply with the child verification text", "subagent_type": childID})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
		} else {
			text := chatAnswer
			if isChild {
				text = childAnswer
				if childCall > 4 {
					text = childResumedAnswer
				}
			}
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": text}})
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server
}
