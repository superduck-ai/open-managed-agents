package liveworker

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

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
