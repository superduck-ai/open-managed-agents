package liveworker

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

type dshLiveConfig struct {
	Base  string
	Key   string
	Model string
}
type dshCall struct {
	Name      string
	Arguments string
}
type dshDelivery struct {
	Revision  int
	InputID   string
	Completed []string
	Outbox    []json.RawMessage
}
type dshProbe struct {
	dshDelivery
	Calls          []dshCall
	UnknownResults int
}

func TestChatBrainDshLive(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	var live dshLiveConfig
	path := os.Getenv("OMA_DSH_LIVE_CONFIG")
	if path == "" {
		t.Fatal("OMA_DSH_LIVE_CONFIG must name an explicit private configuration")
	}
	data, err := os.ReadFile(path)
	requireOK(t, err)
	requireOK(t, json.Unmarshal(data, &live))
	if live.Base == "" || live.Key == "" || live.Model == "" {
		t.Fatal("Incomplete live model configuration")
	}
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	configureDshLiveModel(t, e, cfg, live)
	container, endpoint := brainSandbox(t)
	first := startBrainNode(t, e, cfg, "dsh-live-first")
	second := startBrainNode(t, e, cfg, "dsh-live-second")
	marker := "OMA_DSH_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	f := newDshLiveSession(t, e, endpoint, live.Model, "always_ask")
	client := chatSDK(e)
	input := submitChat(t, f, "Use Write exactly once to write /tmp/oma-dsh-live.txt with this exact content: "+marker+". You may use Read to check this path first. After Write succeeds, reply DONE.")
	toolID := waitDshWritePermission(t, f, &client, "/tmp/oma-dsh-live.txt")
	assertDshFile(t, container, "/tmp/oma-dsh-live.txt", "", false)
	sendChatSDK(t, &client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{Type: "user.tool_confirmation", ToolUseID: toolID, Result: "allow"}})
	waitDshTurn(t, f, input)
	assertDshFile(t, container, "/tmp/oma-dsh-live.txt", marker, true)
	state := readDshState(t, e, f)
	if len(state.Completed) != 1 || len(dshWriteCalls(state)) != 1 {
		t.Fatal("real model tool turn did not settle exactly once")
	}
	chatProof(t, started, "dsh_live_approved_tool")
	before, err := e.database.GetBrainSession(t.Context(), f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.ExternalID)
	requireOK(t, err)
	first.stop(t)
	second.stop(t)
	recoveryInput := submitChat(t, f, "Without using any tools, reply with the exact content you wrote to /tmp/oma-dsh-live.txt in our previous turn. Do not add other text.")
	waitDshLease(t, e, f)
	replacement := startBrainNode(t, e, cfg, "dsh-live-replacement")
	answer := waitDshTurn(t, f, recoveryInput)
	if !strings.Contains(answer, marker) {
		t.Fatalf("restored model did not recall prior file content: %q", answer)
	}
	after, err := e.database.GetBrainSession(t.Context(), f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.ExternalID)
	requireOK(t, err)
	if after.Epoch <= before.Epoch {
		t.Fatal("replacement did not rotate epoch")
	}
	brainHTTP(t, e, cfg, db.BrainScope{OrganizationUUID: f.code.OrganizationUUID, WorkspaceUUID: f.code.WorkspaceUUID, ExternalID: f.code.ExternalID, OwnerUUID: *before.OwnerUUID, Epoch: before.Epoch}, "POST", "/sessions/"+f.code.ExternalID+"/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}, 409)
	chatProof(t, started, "dsh_live_history_takeover")
	edited := newDshLiveSession(t, e, endpoint, live.Model, "always_ask")
	editedInput := submitChat(t, edited, "Use Write exactly once to write /tmp/oma-dsh-original.txt containing ORIGINAL. You may use Read to check this path first. After Write returns, reply DONE. If the platform changes the arguments, accept the changes and do not issue another tool call.")
	editedTool := waitDshWritePermission(t, edited, &client, "/tmp/oma-dsh-original.txt")
	effective := map[string]any{"file_path": "/tmp/oma-dsh-approved.txt", "content": marker + "_EDITED"}
	e.request(t, "POST", "/v1/sessions/"+edited.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []any{map[string]any{"type": "user.tool_confirmation", "tool_use_id": editedTool, "result": "allow", "updated_input": effective}}}, 400)
	publishDshEditedApproval(t, e, edited, editedTool, effective)
	waitDshEditedTurn(t, e, edited, &client, editedInput, editedTool)
	assertDshFile(t, container, "/tmp/oma-dsh-original.txt", "", false)
	assertDshFile(t, container, "/tmp/oma-dsh-approved.txt", marker+"_EDITED", true)
	editedState := readDshState(t, e, edited)
	if len(dshWriteCalls(editedState)) != 1 {
		t.Fatal("edited approval did not execute exactly one Write")
	}
	for _, call := range dshWriteCalls(editedState) {
		var original map[string]any
		requireOK(t, json.Unmarshal([]byte(call.Arguments), &original))
		if original["file_path"] != "/tmp/oma-dsh-original.txt" {
			t.Fatal("native call lost original arguments")
		}
	}
	changedNotice := false
	for _, event := range chatHistory(t, edited) {
		if event.Type != "system.message" {
			continue
		}
		for _, block := range event.Content {
			if strings.HasPrefix(block.Text, "Approved Write arguments: ") && strings.Contains(block.Text, "/tmp/oma-dsh-approved.txt") {
				changedNotice = true
			}
		}
	}
	if !changedNotice {
		t.Fatal("changed approval notice was not delivered")
	}
	requireOK(t, e.service.TerminateManagedAgentCodeSession(t.Context(), edited.session, edited.code.ExternalID))
	chatProof(t, started, "dsh_live_ccr_modified_input")
	crash := newDshLiveSession(t, e, endpoint, live.Model, "always_allow")
	crashInput := submitChat(t, crash, "Use Bash exactly once. Execute precisely this command:\nprintf 'once\\n' >> /tmp/oma-dsh-crash-count; sleep 10\nAfter Bash succeeds reply DONE. Do not run any other commands.")
	waitRealWorker(t, "live Bash side effect before process crash", func() bool {
		output, err := exec.CommandContext(t.Context(), "docker", "exec", container, "cat", "/tmp/oma-dsh-crash-count").Output()
		return err == nil && string(output) == "once\n"
	}, 90*time.Second)
	dispatched := readDshState(t, e, crash)
	if len(dispatched.Calls) != 1 || dispatched.InputID == "" {
		t.Fatal("Bash dispatch not durably recorded before effect")
	}
	replacement.stop(t)
	waitDshLease(t, e, crash)
	startBrainNode(t, e, cfg, "dsh-live-crash-replacement")
	waitRealWorker(t, "native recovery records the unknown tool result", func() bool { return readDshState(t, e, crash).UnknownResults == 1 }, 30*time.Second)
	waitRealWorker(t, "recovery error reaches public history", func() bool {
		for _, event := range chatHistory(t, crash) {
			if event.Type == "session.error" {
				return true
			}
		}
		return false
	})
	assertDshFile(t, container, "/tmp/oma-dsh-crash-count", "once\n", true)
	recovered := readDshState(t, e, crash)
	if recovered.InputID != dispatched.InputID || len(recovered.Completed) != 1 || recovered.Completed[0] != dispatched.InputID {
		t.Fatal("interrupted input ledger was not closed")
	}
	assertDshInputProcessed(t, crash, crashInput)
	chatProof(t, started, "dsh_live_inflight_no_replay")
	evidence := map[string]any{"model": live.Model, "model_base": live.Base, "runtime": "Bun embedded dsh", "public_session": f.session.ExternalID, "approval_session": edited.session.ExternalID, "crash_session": crash.session.ExternalID, "epoch_before": before.Epoch, "epoch_after": after.Epoch, "restored_answer": answer, "modified_file_content": marker + "_EDITED", "public_updated_input": "rejected HTTP 400; CCRv2 response injected via real JetStream; public approval remains pending until session termination", "native_unknown_results": recovered.UnknownResults, "crash_side_effect_count": 1}
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	requireOK(t, err)
	requireOK(t, os.WriteFile(filepath.Join(filepath.Dir(os.Getenv("CONFIG_FILE")), "dsh-live-evidence.json"), encoded, 0600))
}

func waitDshWritePermission(t *testing.T, f *liveSession, client *anthropic.Client, path string) string {
	t.Helper()
	previous := ""
	for attempt := 0; attempt < 4; attempt++ {
		id := waitNewFilePermission(t, client, f.session.ExternalID, previous)
		var tool chatEvent
		for _, event := range chatHistory(t, f) {
			if event.ID == id && event.Type == "agent.tool_use" {
				tool = event
				break
			}
		}
		var input map[string]any
		requireOK(t, json.Unmarshal(tool.Input, &input))
		if input["file_path"] != path {
			t.Fatal("model requested an unexpected file path")
		}
		switch strings.ToLower(tool.Name) {
		case "write":
			return id
		case "read":
			sendChatSDK(t, client, f.session.ExternalID, anthropic.BetaManagedAgentsEventParamsUnion{OfUserToolConfirmation: &anthropic.BetaManagedAgentsUserToolConfirmationEventParams{Type: "user.tool_confirmation", ToolUseID: id, Result: "allow"}})
		default:
			t.Fatalf("model requested an unexpected tool: %s", tool.Name)
		}
		previous = id
	}
	t.Fatal("model did not request Write within the tool budget")
	return ""
}

func dshWriteCalls(state dshProbe) []dshCall {
	var calls []dshCall
	for _, call := range state.Calls {
		if strings.EqualFold(call.Name, "Write") {
			calls = append(calls, call)
		}
	}
	return calls
}

func waitDshEditedTurn(t *testing.T, e *liveEnv, f *liveSession, client *anthropic.Client, inputID, toolID string) {
	t.Helper()
	waitRealWorker(t, "modified CCR approval completes the private turn and drains delivery", func() bool {
		state := readDshState(t, e, f)
		if state.InputID == "" || len(state.Outbox) != 0 || len(state.Completed) != 1 || state.Completed[0] != state.InputID {
			return false
		}
		task, reply := f.consumer(t), realWorkerReplyConsumer(t, f)
		return task.NumPending == 0 && task.NumAckPending == 0 && reply.NumPending == 0 && reply.NumAckPending == 0
	}, 90*time.Second)
	assertDshInputProcessed(t, f, inputID)
	var lastStatus anthropic.BetaManagedAgentsSessionEventUnion
	var answered, succeeded bool
	for _, event := range chatSDKHistory(t, client, f.session.ExternalID) {
		switch event.Type {
		case "session.error":
			t.Fatal("modified CCR approval caused a session error")
		case "session.status_idle":
			lastStatus = event
		case "agent.message":
			answered = true
		case "agent.tool_result":
			if event.ToolUseID == toolID && !event.IsError {
				succeeded = true
			}
		}
	}
	if !answered || !succeeded || lastStatus.StopReason.Type != "requires_action" || len(lastStatus.StopReason.EventIDs) != 1 || lastStatus.StopReason.EventIDs[0] != toolID {
		t.Fatal("expected completed model turn with the injected approval still pending publicly")
	}
}

func assertDshInputProcessed(t *testing.T, f *liveSession, inputID string) {
	t.Helper()
	for _, event := range chatHistory(t, f) {
		if event.ID == inputID && event.Type == "user.message" && event.Processed != nil {
			return
		}
	}
	t.Fatal("public input was not marked processed")
}

func configureDshLiveModel(t *testing.T, e *liveEnv, cfg config.Config, live dshLiveConfig) {
	t.Helper()
	providers, err := e.database.ListLLMProviders(t.Context(), e.key.OrganizationUUID.String(), e.key.WorkspaceUUID.String())
	requireOK(t, err)
	if len(providers) != 1 || !strings.HasPrefix(providers[0].ExternalID, "llmprov_probe_") {
		t.Fatal("requires an isolated fixture provider")
	}
	provider := providers[0]
	provider.BaseURL = live.Base
	provider.ModelIDs = []string{live.Model, "claude-opus-4-6", "claude-sonnet-4-6"}
	mk := cfg.Vault.MasterKey
	kek, err := secrets.ResolveKEK(mk.Kek, mk.KekFile)
	requireOK(t, err)
	defer clear(kek)
	service, err := secrets.NewLocalServiceWithKeys(t.Context(), secrets.LocalKeyMaterial{Version: mk.EffectiveVersion(), KEK: kek}, nil)
	requireOK(t, err)
	envelope, err := service.Seal(t.Context(), llmproviders.SecretBinding(provider), []byte(live.Key))
	requireOK(t, err)
	provider.SecretEnvelope = &envelope
	_, err = e.database.UpdateLLMProvider(t.Context(), provider)
	requireOK(t, err)
}

func newDshLiveSession(t *testing.T, e *liveEnv, endpoint, model, policy string) *liveSession {
	t.Helper()
	snapshot, err := json.Marshal(map[string]any{"model": map[string]string{"id": model}, "tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": policy}}}}})
	requireOK(t, err)
	f := e.newSessionWithSnapshot(t, snapshot)
	launch, err := json.Marshal(codesessions.BrainLaunch{Version: 1, MCPURL: endpoint, Model: model, WorkDir: "/tmp", System: "Follow the user's exact integration test instructions. Use the requested tool exactly once. Never repeat side effects."})
	requireOK(t, err)
	requireOK(t, e.service.AssignBrain(t.Context(), f.code, launch))
	return f
}

func waitDshTurn(t *testing.T, f *liveSession, input string) string {
	t.Helper()
	var answer string
	waitRealWorker(t, "real model turn completes with drained deliveries", func() bool {
		active, complete := false, false
		answer = ""
		for _, event := range chatHistory(t, f) {
			if event.ID == input {
				active = true
				continue
			}
			if !active {
				continue
			}
			if event.Type == "session.error" {
				t.Fatal("real model turn failed; see run-owned Brain and server logs")
			}
			if event.Type == "agent.message" {
				for _, block := range event.Content {
					answer += block.Text
				}
			}
			if event.Type == "session.status_idle" && event.StopReason.Type == "end_turn" {
				complete = true
			}
		}
		if !complete {
			return false
		}
		task, reply := f.consumer(t), realWorkerReplyConsumer(t, f)
		return task.NumPending == 0 && task.NumAckPending == 0 && reply.NumPending == 0 && reply.NumAckPending == 0
	}, 120*time.Second)
	return answer
}

func readDshState(t *testing.T, e *liveEnv, f *liveSession) dshProbe {
	t.Helper()
	events, _, err := e.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionExternalID: f.code.ExternalID, Limit: 500})
	requireOK(t, err)
	var state dshProbe
	for _, event := range events {
		var payload struct {
			Type string
			Data json.RawMessage
		}
		requireOK(t, json.Unmarshal(event.Payload, &payload))
		switch payload.Type {
		case "ccrv2_delivery":
			var delivery dshDelivery
			requireOK(t, json.Unmarshal(payload.Data, &delivery))
			if delivery.Revision > state.Revision {
				state.dshDelivery = delivery
			}
		case "dsh_log":
			var native struct {
				Type string
				Data struct {
					Name      string
					Arguments string
					Error     struct{ Code string }
				}
			}
			requireOK(t, json.Unmarshal(payload.Data, &native))
			if native.Type == "tool/call" {
				state.Calls = append(state.Calls, dshCall{Name: native.Data.Name, Arguments: native.Data.Arguments})
			}
			if native.Type == "tool/result" && native.Data.Error.Code == "TOOL_OUTCOME_UNKNOWN" {
				state.UnknownResults++
			}
		}
	}
	return state
}

func waitDshLease(t *testing.T, e *liveEnv, f *liveSession) {
	t.Helper()
	waitRealWorker(t, "real Brain lease expires", func() bool {
		available, err := e.database.AvailableBrains(t.Context())
		requireOK(t, err)
		for _, s := range available {
			if s.ExternalID == f.code.ExternalID {
				return true
			}
		}
		return false
	}, 80*time.Second)
}

func assertDshFile(t *testing.T, container, path, want string, exists bool) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "docker", "exec", container, "cat", path).Output()
	if !exists {
		if err == nil {
			t.Fatal("unapproved file exists")
		}
		return
	}
	requireOK(t, err)
	if string(output) != want {
		t.Fatalf("sandbox file %s has unexpected bytes", path)
	}
}

func publishDshEditedApproval(t *testing.T, e *liveEnv, f *liveSession, publicID string, effective map[string]any) {
	t.Helper()
	code, found, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	if !found {
		t.Fatal("missing code session")
	}
	var metadata map[string]json.RawMessage
	requireOK(t, json.Unmarshal(code.WorkerExternalMetadata, &metadata))
	var request struct {
		PublicEventID string `json:"public_event_id"`
		RequestID     string `json:"request_id"`
		ToolUseID     string `json:"provider_tool_use_id"`
	}
	for _, raw := range metadata {
		var candidate struct {
			PublicEventID string `json:"public_event_id"`
			RequestID     string `json:"request_id"`
			ToolUseID     string `json:"provider_tool_use_id"`
		}
		if json.Unmarshal(raw, &candidate) == nil && candidate.PublicEventID == publicID {
			request = candidate
			break
		}
	}
	if request.RequestID == "" {
		t.Fatal("pending CCR approval metadata is missing")
	}
	id := uuid.NewString()
	payload, err := json.Marshal(map[string]any{"type": "control_response", "uuid": id, "response": map[string]any{"subtype": "success", "request_id": request.RequestID, "response": map[string]any{"behavior": "allow", "updatedInput": effective, "toolUseID": request.ToolUseID}}})
	requireOK(t, err)
	requireOK(t, e.broker.Publish(t.Context(), id, workerevents.EnvelopeV1{Version: 2, CodeSessionID: f.code.ExternalID, EventID: id, EventType: "control_response", Payload: payload, ExpiresAt: time.Now().Add(5 * time.Minute)}))
}
