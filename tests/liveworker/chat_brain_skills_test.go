package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const brainSkillPath = "/root/.claude/skills/brain-proof/SKILL.md"

type brainSkillUpload struct {
	ID            string `json:"id"`
	LatestVersion string `json:"latest_version"`
	Version       string `json:"version"`
}

func TestChatBrainSkills(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	if os.Getenv("OMA_BRAIN_BINARY") == "" {
		t.Fatal("OMA_BRAIN_BINARY required")
	}
	e := newLiveEnv(t)
	var sessions []*liveSession
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, session := range sessions {
			work, err := e.database.GetLatestEnvironmentWorkForSession(ctx, e.key.WorkspaceUUID.String(), e.environment.ExternalID, session.session.UUID)
			if err != nil || work.State != "stopped" {
				t.Errorf("skill session cleanup failed: state=%s error=%v", work.State, err)
			}
		}
		chatProof(t, started, "brain_skills_cleanup")
	})
	firstVersion := uploadBrainSkill(t, e, "", "v1")
	t.Cleanup(func() {
		brainSkillAPI(t, e, http.MethodDelete, "/v1/skills/"+firstVersion.ID+"?beta=true", nil, "", 200)
	})
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"system":  "You are a research agent. Follow matching skills. Keep literal ${{ agent_example }}.",
		"model":   map[string]string{"id": "claude-sonnet-4-6"},
		"skills":  []any{map[string]string{"type": "custom", "skill_id": firstVersion.ID, "version": "latest"}},
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, 200)
	var err error
	e.agent, err = e.database.GetAgent(t.Context(), e.key.WorkspaceUUID.String(), e.agent.ExternalID)
	requireOK(t, err)
	cfg, err := config.Load()
	requireOK(t, err)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	first := startBrainNode(t, e, cfg, "skills-first")
	second := startBrainNode(t, e, cfg, "skills-second")
	defer first.stop(t)
	defer second.stop(t)
	f := createPublicChat(t, e)
	sessions = append(sessions, f)
	catalog := brainSkillsModel(t, false, firstVersion.LatestVersion, "v1")
	configureChatModel(t, e, catalog.URL)
	brainSkillTurn(t, f, catalog.preview, "请说明你能使用的 skill。", 1, 1)
	if provider.created.Load() != 0 || catalog.calls.Load() != 1 {
		t.Fatal("skill catalog allocated a sandbox or duplicated the model turn")
	}
	chatProof(t, started, "brain_skills_catalog")
	secondVersion := uploadBrainSkill(t, e, firstVersion.ID, "v2")
	if secondVersion.Version == firstVersion.LatestVersion {
		t.Fatal("new skill version reused the previous version")
	}
	read := brainSkillsModel(t, true, firstVersion.LatestVersion, "v1")
	configureChatModel(t, e, read.URL)
	brainSkillTurn(t, f, read.preview, "使用 brain-proof skill，并读取它要求的参考文件。", 2, 4)
	if read.calls.Load() != 3 || provider.created.Load() != 1 {
		t.Fatal("skill loading did not use two MCP reads in one sandbox")
	}
	chatProof(t, started, "brain_skills_read")
	container := fmt.Sprintf("%s-1", provider.name)
	err = exec.CommandContext(t.Context(), "docker", "exec", container, "sh", "-c", `printf altered > "$1"`, "sh", brainSkillPath).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("skill mount permitted mutation: %v", err)
	}
	content, err := exec.CommandContext(t.Context(), "docker", "exec", container, "cat", brainSkillPath).Output()
	requireOK(t, err)
	if !bytes.Contains(content, []byte("oma-skill-body-v1")) || bytes.Contains(content, []byte("altered")) {
		t.Fatal("skill bytes changed after a readonly write")
	}
	chatProof(t, started, "brain_skills_readonly")
	latest := createPublicChat(t, e)
	sessions = append(sessions, latest)
	newRead := brainSkillsModel(t, true, secondVersion.Version, "v2")
	configureChatModel(t, e, newRead.URL)
	brainSkillTurn(t, latest, newRead.preview, "使用 brain-proof skill，并读取它要求的参考文件。", 1, 3)
	if newRead.calls.Load() != 3 || provider.created.Load() != 2 {
		t.Fatal("new session did not mount the current skill version")
	}
	chatProof(t, started, "brain_skills_snapshot")
}

func uploadBrainSkill(t *testing.T, e *liveEnv, id, marker string) brainSkillUpload {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, file := range []struct{ path, content string }{
		{"brain-proof/SKILL.md", "---\nname: brain-proof\ndescription: Verify Brain skill loading\n---\n\noma-skill-body-" + marker + "\nRead references/proof.md before answering.\n"},
		{"brain-proof/references/proof.md", "oma-skill-reference-" + marker},
	} {
		part, err := writer.CreateFormFile("files[]", file.path)
		requireOK(t, err)
		_, err = io.WriteString(part, file.content)
		requireOK(t, err)
	}
	requireOK(t, writer.Close())
	path := "/v1/skills?beta=true"
	if id != "" {
		path = "/v1/skills/" + id + "/versions?beta=true"
	}
	var result brainSkillUpload
	requireOK(t, json.Unmarshal(brainSkillAPI(t, e, http.MethodPost, path, &body, writer.FormDataContentType(), 200), &result))
	return result
}

func brainSkillAPI(t *testing.T, e *liveEnv, method, path string, body io.Reader, contentType string, want int) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, e.url+path, body)
	requireOK(t, err)
	request.Header.Set("Authorization", "Bearer "+e.apiKey)
	request.Header.Set("Anthropic-Beta", "skills-2025-10-02")
	request.Header.Set("Content-Type", contentType)
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	requireOK(t, err)
	if response.StatusCode != want {
		t.Fatalf("skill API %s %s: status %d want %d: %s", method, path, response.StatusCode, want, data)
	}
	return data
}

func brainSkillTurn(t *testing.T, f *liveSession, preview chan struct{}, text string, turns, models int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	input := submitChat(t, f, text)
	final := receiveChatAnswer(t, ctx, events, func() { preview <- struct{}{} })
	var err error
	f.code, err = f.env.database.GetCodeSessionBySessionExternalID(ctx, f.env.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	waitChatIdle(t, f, turns)
	if models > 1 {
		registerPublicSandboxCleanup(t, f.env, f)
	}
	var history struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?order=asc&limit=100", f.env.apiKey, nil, 200), &history))
	var inputs, answers, starts, ends int
	var inputSeen, finalSeen bool
	for _, event := range history.Data {
		switch event.Type {
		case "user.message":
			inputs++
			if event.Processed == nil {
				t.Fatal("skill input was not acknowledged")
			}
			inputSeen = inputSeen || event.ID == input
		case "agent.message":
			answers++
			if len(event.Content) != 1 || event.Content[0].Text != chatAnswer {
				t.Fatal("skill final history differs from SSE")
			}
			finalSeen = finalSeen || event.ID == final.ID
		case "span.model_request_start":
			starts++
		case "span.model_request_end":
			ends++
		case "event_start", "event_delta":
			t.Fatal("skill preview leaked into persistent history")
		}
	}
	if !inputSeen || !finalSeen || inputs != turns || answers != turns || starts != models || ends != models {
		t.Fatalf("skill history: input=%d final=%d start=%d end=%d expected turns=%d models=%d", inputs, answers, starts, ends, turns, models)
	}
}

type brainSkillModel struct {
	*httptest.Server
	calls   atomic.Int32
	preview chan struct{}
}

func brainSkillsModel(t *testing.T, read bool, version, marker string) *brainSkillModel {
	t.Helper()
	model := &brainSkillModel{preview: make(chan struct{}, 4)}
	model.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			System   json.RawMessage `json:"system"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		requireOK(t, json.NewDecoder(r.Body).Decode(&request))
		step := model.calls.Add(1)
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Error("missing Anthropic-Version")
		}
		for _, fragment := range []string{"All filesystem and command tools execute in the remote sandbox.", "OMA environment:", "Available skills, read SKILL.md through Read before use:", "Configured agent instructions:", "You are a research agent", "${{ agent_example }}", "Verify Brain skill loading", brainSkillPath, version} {
			if !bytes.Contains(request.System, []byte(fragment)) {
				t.Errorf("missing Brain prompt fragment %q", fragment)
			}
		}
		if bytes.Contains(request.System, []byte("oma-skill-body-")) {
			t.Error("skill body was eagerly inserted into the system prompt")
		}
		name := ""
		input := map[string]string{}
		if read && step < 3 {
			name = "Read"
			input["file_path"] = brainSkillPath
			if step == 2 {
				input["file_path"] = strings.TrimSuffix(brainSkillPath, "SKILL.md") + "references/proof.md"
			}
		}
		if read && step > 1 {
			fragment := "oma-skill-body-" + marker
			if step == 3 {
				fragment = "oma-skill-reference-" + marker
			}
			if !bytes.Contains(request.Messages[len(request.Messages)-1].Content, []byte(fragment)) {
				t.Errorf("fresh MCP result did not load %q", fragment)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(event map[string]any) {
			data, err := json.Marshal(event)
			requireOK(t, err)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
			w.(http.Flusher).Flush()
		}
		write(map[string]any{"type": "message_start", "message": map[string]any{"id": fmt.Sprintf("msg_skills_%d", step), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		stop := "end_turn"
		if name != "" {
			write(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": fmt.Sprintf("call_skill_%d", step), "name": name, "input": map[string]any{}}})
			data, err := json.Marshal(input)
			requireOK(t, err)
			write(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(data)}})
			stop = "tool_use"
		} else {
			write(map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			write(map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": chatAnswer}})
			select {
			case <-model.preview:
			case <-r.Context().Done():
				return
			}
		}
		write(map[string]any{"type": "content_block_stop", "index": 0})
		write(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write(map[string]any{"type": "message_stop"})
	}))
	t.Cleanup(model.Close)
	return model
}
