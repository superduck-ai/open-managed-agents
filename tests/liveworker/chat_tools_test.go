//go:build integration || e2e || e2b_integration

package liveworker

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestToolFixturesKeepAgentFirstTurnAfterTitle(t *testing.T) {
	for _, fixture := range []struct {
		name   string
		start  func(*testing.T) string
		toolID string
	}{
		{name: "malformed", start: func(t *testing.T) string {
			return malformedToolFixture(t, new(atomic.Int32))
		}, toolID: "toolu_malformed_e2e"},
		{name: "confirmation", start: func(t *testing.T) string {
			resume := make(chan struct{})
			close(resume)
			return realWorkerModelFixture(t, new(atomic.Int32), new(atomic.Int32), false, resume, "/tmp/oma-control-e2e.txt")
		}, toolID: "toolu_control_e2e"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			modelURL := strings.Replace(fixture.start(t), "host.docker.internal", "127.0.0.1", 1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			requestModel := func(body, contentType string) []byte {
				t.Helper()
				request, err := http.NewRequestWithContext(ctx, http.MethodPost, modelURL+"/v1/messages", strings.NewReader(body))
				requireOK(t, err)
				response, err := http.DefaultClient.Do(request)
				requireOK(t, err)
				defer response.Body.Close()
				if got := response.Header.Get("Content-Type"); response.StatusCode != http.StatusOK || got != contentType {
					t.Fatalf("status=%d content type=%q, want 200 %q", response.StatusCode, got, contentType)
				}
				payload, err := io.ReadAll(response.Body)
				requireOK(t, err)
				return payload
			}

			var title struct {
				StopReason string `json:"stop_reason"`
			}
			requireOK(t, jsonv2.Unmarshal(requestModel(`{"max_tokens":128}`, "application/json"), &title))
			if title.StopReason != "end_turn" {
				t.Fatalf("title stop reason=%q, want end_turn", title.StopReason)
			}
			turn := string(requestModel(`{"stream":true}`, "text/event-stream"))
			if !strings.Contains(turn, fixture.toolID) || !strings.Contains(turn, `"stop_reason":"tool_use"`) {
				t.Fatalf("first Agent turn did not request %s: %s", fixture.toolID, turn)
			}
		})
	}
}

func chatSDK(e *liveEnv) anthropic.Client {
	return anthropic.NewClient(option.WithBaseURL(e.url), option.WithAPIKey(e.apiKey), option.WithMaxRetries(0), option.WithRequestTimeout(10*time.Second))
}

func sendChatSDK(t *testing.T, client *anthropic.Client, sessionID string, events ...anthropic.BetaManagedAgentsEventParamsUnion) {
	t.Helper()
	result, err := client.Beta.Sessions.Events.Send(t.Context(), sessionID, anthropic.BetaSessionEventSendParams{Events: events})
	requireOK(t, err)
	if len(result.Data) != len(events) {
		t.Fatal("SDK send must acknowledge every submitted event")
	}
}

func chatSDKHistory(t *testing.T, client *anthropic.Client, sessionID string) []anthropic.BetaManagedAgentsSessionEventUnion {
	t.Helper()
	pager := client.Beta.Sessions.Events.ListAutoPaging(t.Context(), sessionID, anthropic.BetaSessionEventListParams{Limit: anthropic.Int(100), Order: "asc"})
	var events []anthropic.BetaManagedAgentsSessionEventUnion
	for pager.Next() {
		events = append(events, pager.Current())
	}
	requireOK(t, pager.Err())
	return events
}

func TestChatTools(t *testing.T) {
	if os.Getenv("VERIFY_BE_RUN_ID") == "" || os.Getenv("LIVE_WORKER_REAL_CLAUDE") != "1" {
		t.Skip("run through verify-be with isolated dependencies and a real Worker image")
	}
	started := time.Now()
	e := newLiveEnv(t)
	client := chatSDK(e)
	if t.Run("malformed", func(t *testing.T) {
		for _, policy := range []string{"always_ask", "always_allow"} {
			t.Run(policy, func(t *testing.T) { verifyMalformedTool(t, e, &client, policy) })
		}
	}) {
		t.Logf("BE_PROOF {\"stage\":\"malformed_tool_rejected\",\"elapsed_ms\":%d}", time.Since(started).Milliseconds())
	}
	for _, decision := range []anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult{"deny", "allow"} {
		if t.Run(string(decision), func(t *testing.T) { verifySDKToolDecision(t, e, &client, decision) }) {
			stage := "tool_denied"
			if decision == "allow" {
				stage = "tool_allowed"
			}
			t.Logf("BE_PROOF {\"stage\":%q,\"elapsed_ms\":%d}", stage, time.Since(started).Milliseconds())
		}
	}
}

func verifySDKToolDecision(t *testing.T, e *liveEnv, client *anthropic.Client, decision anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult) {
	t.Helper()
	f := e.newSessionWithSnapshot(t, json.RawMessage(`{"model":{"id":"claude-sonnet-4-6"},"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":true,"permission_policy":{"type":"always_ask"}}}]}`))
	var calls, queued atomic.Int32
	resume := make(chan struct{})
	close(resume)
	modelURL := realWorkerModelFixture(t, &calls, &queued, false, resume, "/tmp/oma-control-e2e.txt")
	configureChatModel(t, e, strings.Replace(modelURL, "host.docker.internal", "127.0.0.1", 1))
	worker := startRealControlWorker(t, f, "")
	sendChatSDK(t, client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{
		OfUserMessage: &anthropic.BetaManagedAgentsUserMessageEventParams{Type: "user.message", Content: []anthropic.BetaManagedAgentsUserMessageEventParamsContentUnion{{OfText: &anthropic.BetaManagedAgentsTextBlockParam{Type: "text", Text: "Write the verification file using the requested tool, then reply done."}}}},
	})
	toolID := waitSDKToolPermission(t, client, f.session.ExternalID)
	assertToolFile(t, worker, false)
	sendChatSDK(t, client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{
		OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{Type: "user.tool_confirmation", ToolUseID: toolID, Result: decision},
	})
	waitRealWorker(t, "SDK tool turn idle and delivery drained", func() bool {
		session, err := client.Beta.Sessions.Get(t.Context(), f.session.ExternalID, anthropic.BetaSessionGetParams{})
		requireOK(t, err)
		if calls.Load() != 2 || session.Status != "idle" {
			return false
		}
		input, reply := f.consumer(t), realWorkerReplyConsumer(t, f)
		return input.NumPending == 0 && input.NumAckPending == 0 && reply.NumPending == 0 && reply.NumAckPending == 0
	})
	assertToolFile(t, worker, decision == "allow")
	assertSDKToolHistory(t, chatSDKHistory(t, client, f.session.ExternalID), toolID, decision, 1)
}

func waitSDKToolPermission(t *testing.T, client *anthropic.Client, sessionID string) string {
	t.Helper()
	var toolID string
	waitRealWorker(t, "SDK requires_action event", func() bool {
		events := chatSDKHistory(t, client, sessionID)
		for _, event := range events {
			if event.Type == "session.status_idle" && event.StopReason.Type == "requires_action" && len(event.StopReason.EventIDs) == 1 {
				candidate := event.StopReason.EventIDs[0]
				for _, tool := range events {
					if tool.Type == "agent.tool_use" && tool.ID == candidate {
						toolID = candidate
						return true
					}
				}
			}
		}
		return false
	})
	return toolID
}

func assertToolFile(t *testing.T, worker *realControlWorker, written bool) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "docker", "exec", worker.container, "sh", "-c", "if [ -f /tmp/oma-control-e2e.txt ]; then cat /tmp/oma-control-e2e.txt; else printf '<absent>'; fi").Output()
	requireOK(t, err)
	want := "<absent>"
	if written {
		want = "verified control delivery"
	}
	if string(output) != want {
		t.Fatalf("tool file=%q want=%q", output, want)
	}
}

func assertSDKToolHistory(t *testing.T, events []anthropic.BetaManagedAgentsSessionEventUnion, toolID string, decision anthropic.BetaManagedAgentsUserToolConfirmationEventParamsResult, wantConfirmations int) {
	t.Helper()
	var uses, results, confirmations, answers int
	for _, event := range events {
		switch event.Type {
		case "agent.tool_use":
			if event.ID != toolID {
				t.Fatal("unexpected tool call")
			}
			uses++
		case "agent.tool_result":
			if event.ToolUseID != toolID || event.IsError != (decision == "deny") {
				t.Fatalf("unexpected tool result: id=%s error=%t", event.ToolUseID, event.IsError)
			}
			results++
		case "user.tool_confirmation":
			if event.ToolUseID != toolID || event.Result != string(decision) {
				t.Fatal("confirmation does not match requested decision")
			}
			confirmations++
		case "agent.message":
			message := event.AsAgentMessage()
			if len(message.Content) != 1 || message.Content[0].Text != "done" {
				t.Fatal("tool turn did not complete with final reply")
			}
			answers++
		}
	}
	if uses != 1 || results != 1 || confirmations != wantConfirmations || answers != 1 {
		t.Fatalf("tool history: uses=%d results=%d confirmations=%d answers=%d", uses, results, confirmations, answers)
	}
}
