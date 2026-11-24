//go:build integration || e2e || e2b_integration

package liveworker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestChatBrainCancel(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	tools := []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}}
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{"version": e.agent.CurrentVersion, "multiagent": map[string]string{"type": "multiagent_20261001"}, "tools": tools}, 200)
	var rootCalls, childCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request compactionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad model request", 400)
			return
		}
		root := false
		for _, tool := range request.Tools {
			if tool.Name == "subagent" {
				root = true
			}
		}
		if root {
			step := rootCalls.Add(1)
			switch step {
			case 1, 3:
				writeCompactionResponse(w, request.Model, 100, "", "subagent", map[string]string{"description": fmt.Sprintf("worker-%d", step), "prompt": "Run the assigned Bash command."})
			case 2:
				cancelProbeBash(w, request.Model, "root")
			default:
				writeCompactionResponse(w, request.Model, 100, "Coordinator finished", "", nil)
			}
			return
		}
		step := childCalls.Add(1)
		cancelProbeBash(w, request.Model, fmt.Sprintf("child-%d", step))
	}))
	defer upstream.Close()
	configureChatModel(t, e, upstream.URL)
	provider, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	node := startBrainNode(t, e, cfg, "cancel")
	defer node.stop(t)
	f := createPublicChat(t, e)
	sandbox := provider.name + "-1"
	client := chatSDK(e)
	for index, scenario := range []struct {
		roles    []string
		wantRoot int32
	}{
		{[]string{"root", "child-1"}, 2},
		{[]string{"child-2"}, 4},
	} {
		input := submitChat(t, f, fmt.Sprintf("delegate task %d", index+1))
		waitRealWorker(t, "Bash started in the sandbox", func() bool {
			for _, role := range scenario.roles {
				if exec.CommandContext(t.Context(), "docker", "exec", sandbox, "test", "-s", "/tmp/"+role+".pid").Run() != nil {
					return false
				}
			}
			return rootCalls.Load() == scenario.wantRoot
		}, 70*time.Second)
		f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
		requireOK(t, err)
		if index == 0 {
			registerPublicSandboxCleanup(t, e, f)
		}
		if index == 1 {
			waitRealWorker(t, "coordinator finished its answer before cancel", func() bool {
				for _, event := range chatHistory(t, f) {
					for _, content := range event.Content {
						if event.Type == "agent.message" && content.Text == "Coordinator finished" {
							return true
						}
					}
				}
				return false
			})
		}
		verifySDKSessionInterrupt(t, &client, f.session.ExternalID)
		verifyCancelledThreads(t, &client, f.session.ExternalID, input, index+1)
		for _, role := range scenario.roles {
			waitRealWorker(t, "cancelled Bash process exited", func() bool {
				return exec.CommandContext(t.Context(), "docker", "exec", sandbox, "sh", "-c", `test -s "$1" && ! kill -0 "$(cat "$1")" 2>/dev/null`, "sh", "/tmp/"+role+".pid").Run() == nil
			}, 10*time.Second)
		}
		if index == 0 {
			chatProof(t, started, "parent_and_child_stopped")
		} else {
			chatProof(t, started, "idle_parent_preserved")
		}
	}
	chatProof(t, started, "sdk_interrupt_stream_and_history")
	timer := time.NewTimer(41 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if rootCalls.Load() != 4 || childCalls.Load() != 2 {
		t.Fatalf("unexpected model calls after cancel: parent %d child %d", rootCalls.Load(), childCalls.Load())
	}
	requireOK(t, exec.CommandContext(t.Context(), "docker", "exec", sandbox, "sh", "-c", "test ! -e /tmp/root.done && test ! -e /tmp/child-1.done && test ! -e /tmp/child-2.done").Run())
	chatProof(t, started, "no_work_after_cancel")
	submitChat(t, f, "reply in the same session")
	waitRealWorker(t, "new user input completed", func() bool {
		history := chatHistory(t, f)
		for index := len(history) - 1; index >= 0; index-- {
			if history[index].Type == "session.status_idle" {
				return rootCalls.Load() == 5 && history[index].StopReason.Type == "end_turn"
			}
		}
		return false
	})
	chatProof(t, started, "next_turn_completed")
}

func cancelProbeBash(w http.ResponseWriter, model, role string) {
	writeCompactionResponse(w, model, 100, "", "Bash", map[string]string{"command": fmt.Sprintf("echo $$ > /tmp/%s.pid; sleep 40; touch /tmp/%s.done", role, role), "description": "Wait for session stop"})
}

func verifySDKSessionInterrupt(t *testing.T, client *anthropic.Client, sessionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	service := &client.Beta.Sessions.Events
	stream := service.StreamEvents(ctx, sessionID, anthropic.BetaSessionEventStreamParams{}, option.WithRequestTimeout(0))
	defer stream.Close()
	if err := stream.Err(); err != nil {
		t.Fatalf("open SDK stream: %v", err)
	}
	sent, err := service.Send(ctx, sessionID, anthropic.BetaSessionEventSendParams{
		Events: []anthropic.BetaManagedAgentsEventParamsUnion{anthropic.BetaManagedAgentsEventParamsOfUserInterrupt("user.interrupt")},
	})
	if err != nil {
		t.Fatalf("send SDK interrupt: %v", err)
	}
	if len(sent.Data) != 1 || sent.Data[0].Type != "user.interrupt" {
		t.Fatal("expected one SDK interrupt receipt")
	}
	receipt := sent.Data[0].AsUserInterrupt()
	if receipt.ID == "" || receipt.JSON.ProcessedAt.Raw() != "null" || receipt.JSON.SessionThreadID.Raw() != "" || len(receipt.JSON.ExtraFields) != 0 {
		t.Fatalf("unexpected session interrupt receipt: %s", receipt.RawJSON())
	}
	t.Logf("SDK accepted interrupt %s", receipt.ID)
	var applied time.Time
	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "user.interrupt":
			interrupt := event.AsUserInterrupt()
			if interrupt.ID != receipt.ID || !applied.IsZero() || interrupt.ProcessedAt.IsZero() || interrupt.JSON.SessionThreadID.Raw() != "" || len(interrupt.JSON.ExtraFields) != 0 {
				t.Fatalf("unexpected streamed interrupt: %s", interrupt.RawJSON())
			}
			applied = interrupt.ProcessedAt
			t.Logf("SDK observed applied interrupt %s", interrupt.ID)
		case "session.status_idle":
			if idle := event.AsSessionStatusIdle(); applied.IsZero() || idle.StopReason.Type != "end_turn" || len(idle.JSON.ExtraFields) != 0 {
				t.Fatalf("unexpected SDK session idle after interrupt at %v: %s", applied, event.RawJSON())
			}
			verifySDKInterruptHistory(t, client, sessionID, receipt.ID, applied)
			return
		}
	}
	t.Fatalf("SDK stream ended before session.status_idle, interrupt=%s processed_at=%v: %v", receipt.ID, applied, stream.Err())
}

func verifySDKInterruptHistory(t *testing.T, client *anthropic.Client, sessionID, id string, processedAt time.Time) {
	t.Helper()
	matches := 0
	for _, event := range chatSDKHistory(t, client, sessionID) {
		if event.ID != id {
			continue
		}
		interrupt := event.AsUserInterrupt()
		if event.Type != "user.interrupt" || interrupt.JSON.SessionThreadID.Raw() != "" || !interrupt.ProcessedAt.Equal(processedAt) || len(interrupt.JSON.ExtraFields) != 0 {
			t.Fatalf("interrupt history differs from SSE: %s", event.RawJSON())
		}
		matches++
	}
	if matches != 1 {
		t.Fatalf("found %d history records for %s", matches, id)
	}
}

func verifyCancelledThreads(t *testing.T, client *anthropic.Client, sessionID, input string, childCount int) {
	t.Helper()
	threads := client.Beta.Sessions.Threads.ListAutoPaging(t.Context(), sessionID, anthropic.BetaSessionThreadListParams{})
	count := 0
	for threads.Next() {
		thread := threads.Current()
		if !strings.HasPrefix(thread.ID, "sthr_") {
			t.Fatalf("unexpected managed-agent thread ID: %q", thread.ID)
		}
		count++
		history := client.Beta.Sessions.Threads.Events.ListAutoPaging(t.Context(), thread.ID, anthropic.BetaSessionThreadEventListParams{SessionID: sessionID})
		reason := ""
		interruptedTools := map[string]bool{}
		for history.Next() {
			event := history.Current()
			if event.SessionThreadID != "" && event.SessionThreadID != thread.ID {
				continue
			}
			if event.Type == "agent.tool_use" && strings.EqualFold(event.Name, "Bash") {
				interruptedTools[event.ID] = false
			}
			if _, found := interruptedTools[event.ToolUseID]; found && event.Type == "agent.tool_result" && event.IsError {
				interruptedTools[event.ToolUseID] = true
			}
			if event.Type == "session.thread_status_idle" {
				idle := event.AsSessionThreadStatusIdle()
				if idle.SessionThreadID != thread.ID || !idle.JSON.AgentName.Valid() || idle.ProcessedAt.IsZero() || len(idle.JSON.ExtraFields) != 0 {
					t.Fatalf("unexpected SDK thread idle for %s: %s", thread.ID, idle.RawJSON())
				}
				reason = string(idle.StopReason.Type)
			}
		}
		requireOK(t, history.Err())
		if len(interruptedTools) == 0 {
			t.Fatalf("missing Bash event for thread %s", thread.ID)
		}
		for call, settled := range interruptedTools {
			if !settled {
				t.Fatalf("Bash %s in thread %s missing its interrupt result", call, thread.ID)
			}
		}
		if thread.Status != "idle" || reason != "end_turn" {
			t.Fatalf("thread %s after input %s: status=%s last idle=%q, want idle/end_turn", thread.ID, input, thread.Status, reason)
		}
	}
	requireOK(t, threads.Err())
	if count != childCount+1 {
		t.Fatalf("session has %d threads, want %d", count, childCount+1)
	}
}
