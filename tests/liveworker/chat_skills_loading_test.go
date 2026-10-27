//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestChatSkillsLoading(t *testing.T) {
	isolatedChat(t)
	for _, mode := range []string{"brain", "sandbox"} {
		t.Run(mode, func(t *testing.T) {
			started := time.Now()
			verifySkillLoading(t, mode)
			chatProof(t, started, "skill_loading_"+mode)
		})
	}
}

func verifySkillLoading(t *testing.T, mode string) {
	t.Helper()
	e := newLiveEnv(t)
	skill := uploadBrainSkill(t, e, "", "v1")
	t.Cleanup(func() { brainSkillAPI(t, e, http.MethodDelete, "/v1/skills/"+skill.ID+"?beta=true", nil, "", 200) })
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion,
		"system":  "Follow matching skills and read their referenced files before answering.",
		"model":   map[string]string{"id": "claude-sonnet-4-6"},
		"skills":  []any{map[string]string{"type": "custom", "skill_id": skill.ID, "version": skill.LatestVersion}},
		"tools":   []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_ask"}}}},
	}, 200)
	cfg, err := config.Load()
	requireOK(t, err)
	var provider *chatDockerProvider
	var stop func()
	if mode == "brain" {
		provider, stop = startLazyBrainRunner(t, e, cfg)
		node := startBrainNode(t, e, cfg, "skill-loading")
		defer node.stop(t)
	} else {
		provider, stop = startPublicRunner(t, e)
	}
	defer stop()
	f := createPublicChat(t, e)
	client := chatSDK(e)
	previousToolID := ""
	cleanupRegistered := false
	for turn, step := range []struct{ decision, location string }{
		{"deny", brainSkillPath},
		{"text", brainSkillPath},
		{"allow", brainSkillPath},
		{"allow", strings.TrimSuffix(brainSkillPath, "SKILL.md") + "references/proof.md"},
		{"allow", "/etc/hostname"},
	} {
		model := skillLoadingModel(t, step.decision, step.location)
		configureChatModel(t, e, model.URL)
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		events, closeStream := connectChatStream(t, ctx, f)
		submitChat(t, f, "读取绑定的 Skill，并按需使用文件工具。")
		toolID := ""
		if step.decision != "text" {
			toolID = waitNewFilePermission(t, &client, f.session.ExternalID, previousToolID)
			previousToolID = toolID
			if mode == "brain" && turn < 3 && provider.created.Load() != 0 {
				t.Fatal("Brain allocated a sandbox before an approved Skill Read")
			}
			sendChatSDK(t, &client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{
				Type: "user.tool_confirmation", ToolUseID: toolID, Result: anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult(step.decision),
			}})
		}
		receiveChatAnswer(t, ctx, events, func() { model.preview <- struct{}{} })
		f.code, err = e.database.GetCodeSessionBySessionExternalID(ctx, e.key.WorkspaceUUID.String(), f.session.ExternalID)
		requireOK(t, err)
		waitChatIdle(t, f, turn+1)
		closeStream()
		cancel()
		if toolID != "" {
			assertFilePermissionHistory(t, e, f, toolID, step.decision, "oma-skill-body-v1")
		}
		wanted := int32(1)
		if mode == "brain" && turn < 2 {
			wanted = 0
		}
		if provider.created.Load() != wanted {
			t.Fatalf("Skill step %d allocated %d sandboxes, want %d", turn, provider.created.Load(), wanted)
		}
		if wanted != 0 && !cleanupRegistered {
			registerPublicSandboxCleanup(t, e, f)
			cleanupRegistered = true
		}
	}
	container := fmt.Sprintf("%s-1", provider.name)
	err = exec.CommandContext(t.Context(), "docker", "exec", container, "sh", "-c", `printf changed > "$1"`, "sh", brainSkillPath).Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("Skill mount did not reject mutation with a process failure: %v", err)
	}
	body, err := exec.CommandContext(t.Context(), "docker", "exec", container, "cat", brainSkillPath).Output()
	requireOK(t, err)
	if !bytes.Contains(body, []byte("oma-skill-body-v1")) || bytes.Contains(body, []byte("changed")) {
		t.Fatal("readonly Skill bytes changed")
	}
}

func waitNewFilePermission(t *testing.T, client *anthropic.Client, sessionID, previous string) string {
	t.Helper()
	var toolID string
	waitRealWorker(t, "new file tool permission", func() bool {
		history := chatSDKHistory(t, client, sessionID)
		for index := len(history) - 1; index >= 0; index-- {
			event := history[index]
			if event.Type == "session.status_idle" && event.StopReason.Type == "requires_action" && len(event.StopReason.EventIDs) == 1 {
				toolID = event.StopReason.EventIDs[0]
				return toolID != previous
			}
		}
		return false
	})
	return toolID
}

func assertFilePermissionHistory(t *testing.T, e *liveEnv, f *liveSession, id, decision, deniedMarker string) {
	t.Helper()
	var history struct {
		Data []chatEvent `json:"data"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/events?order=asc&limit=100", e.apiKey, nil, 200), &history))
	var uses, results, confirmations int
	for _, event := range history.Data {
		switch {
		case event.Type == "agent.tool_use" && event.ID == id:
			uses++
		case event.Type == "agent.tool_result" && event.ToolUseID == id:
			results++
			if event.IsError != (decision == "deny") || decision == "deny" && bytes.Contains(event.ToolUseResult, []byte(deniedMarker)) {
				t.Fatal("Skill tool result does not match its permission")
			}
			for _, content := range event.Content {
				if decision == "deny" && strings.Contains(content.Text, deniedMarker) {
					t.Fatal("denied Skill body leaked into public history")
				}
			}
		case event.Type == "user.tool_confirmation" && event.ToolUseID == id:
			confirmations++
		}
	}
	if uses != 1 || results != 1 || confirmations != 1 {
		t.Fatalf("Skill history association: uses=%d results=%d confirmations=%d", uses, results, confirmations)
	}
}

func skillLoadingModel(t *testing.T, decision, location string) *brainSkillModel {
	t.Helper()
	model := &brainSkillModel{preview: make(chan struct{}, 4)}
	id := uuid.NewString()
	marker := "oma-skill-body-v1"
	if strings.Contains(location, "/references/") {
		marker = "oma-skill-reference-v1"
	} else if !strings.HasPrefix(location, "/root/.claude/skills/") {
		marker = ""
	}
	return fileCatalogModel(t, model, id, decision, location, marker, []string{"brain-proof", "Verify Brain skill loading"})
}

func fileCatalogModel(t *testing.T, model *brainSkillModel, id, decision, location, marker string, expected []string) *brainSkillModel {
	t.Helper()
	model.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		requireOK(t, err)
		call := model.calls.Add(1)
		var payload struct {
			System json.RawMessage `json:"system"`
		}
		requireOK(t, json.Unmarshal(body, &payload))
		for _, fragment := range expected {
			if !bytes.Contains(body, []byte(fragment)) {
				t.Errorf("model request missing file catalog metadata %q", fragment)
			}
		}
		if marker != "" && bytes.Contains(payload.System, []byte(marker)) || bytes.Contains(payload.System, []byte("oma-skill-body-")) || bytes.Contains(payload.System, []byte("oma-skill-reference-")) {
			t.Error("file body was eagerly inserted into system prompt")
		}
		if marker != "" && call > 1 && decision == "allow" && !bytes.Contains(body, []byte(marker)) {
			t.Errorf("approved Read did not return %q", marker)
		}
		if marker != "" && decision == "deny" && bytes.Contains(body, []byte(marker)) {
			t.Error("denied Read exposed file content")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(event string, fields map[string]any) {
			fields["type"] = event
			data, _ := json.Marshal(fields)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": "msg_" + id, "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		reason := "end_turn"
		if call == 1 && decision != "text" {
			reason = "tool_use"
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_" + id, "name": "Read", "input": map[string]any{}}})
			input, _ := json.Marshal(map[string]string{"file_path": location})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
		} else {
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]string{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]string{"type": "text_delta", "text": chatAnswer}})
			select {
			case <-model.preview:
			case <-r.Context().Done():
				return
			}
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": reason}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(model.Close)
	return model
}
