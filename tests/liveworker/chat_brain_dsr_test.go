//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
	"github.com/superduck-ai/open-managed-agents/internal/workerevents"
)

func TestChatBrainDSR(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	cfg.CodeSession.BrainWorkerProtocol = "dsr1"
	_, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{"multiagent": map[string]any{"type": "multiagent_20261001", "subagents": map[string]any{"type": "enabled", "inline_agents": map[string]string{"type": "enabled"}}}, "version": e.agent.CurrentVersion, "model": map[string]string{"id": "claude-sonnet-4-6"}, "tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_ask"}}}}}, 200)
	f := createPublicChat(t, e)
	previews := make(chan struct{}, 8)
	var calls atomic.Int32
	model := dsrHistoryModel(t, previews, &calls)
	configureChatModel(t, e, model.URL)
	first := startBrainNode(t, e, cfg, "dsr-first")
	ctx, cancel := context.WithTimeout(t.Context(), chatTimeout(t, 300*time.Second))
	defer cancel()
	events, closeEvents := connectChatStream(t, ctx, f)
	defer closeEvents()
	input := submitChat(t, f, "请回复固定的聊天验证文本。")
	waitRealWorker(t, "DSR owner registration", func() bool {
		code, err := e.database.GetCodeSessionBySessionExternalID(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
		if err != nil {
			return false
		}
		f.code = code
		return code.WorkerRegisteredAt != nil
	})
	final := receiveChatAnswer(t, ctx, events, func() { previews <- struct{}{} })
	verifyChatHistory(t, f, input, final)
	waitChatIdle(t, f, 1)
	if calls.Load() != 1 {
		t.Fatalf("duplicate model requests=%d", calls.Load())
	}
	chatProof(t, started, "dsr_roundtrip")
	token := dsrOwnerToken(t, f)
	dsrRequest(t, e, "POST", "/v1/dsr/sessions/"+f.code.ExternalID+"/register", token, map[string]any{}, 200)
	dsrRequest(t, e, "POST", "/v1/dsr/sessions/"+f.code.ExternalID+"/heartbeat", token, map[string]any{}, 200)
	dsrRequest(t, e, "GET", "/v1/dsr/sessions/"+f.code.ExternalID+"/context", token, nil, 404)
	dsrRequest(t, e, "GET", "/v1/dsr/sessions/"+f.code.ExternalID+"/worker-state", token, nil, 404)
	dsrRequest(t, e, "PUT", "/v1/dsr/sessions/"+f.code.ExternalID+"/worker-state", token, map[string]any{}, 404)
	verifyDSRTranscriptInterfaces(t, e, f, token)
	chatProof(t, started, "dsr_transcript_contracts")
	verifyDSRTool(t, e, false)
	chatProof(t, started, "dsr_tool_approval")
	verifyDSRTool(t, e, true)
	chatProof(t, started, "dsr_cancel")
	firstInput, firstAnswer := input, final.ID
	first.stop(t)
	verifyDSRStateContracts(t, e, f, token)
	verifyDSRACKRecovery(t, e, f, input)
	chatProof(t, started, "dsr_ack_recovery")
	input = submitChat(t, f, "请在接管后继续回复固定文本。")
	configureChatModel(t, e, model.URL)
	claimToken, err := os.ReadFile(cfg.CodeSession.BrainClaimTokenFile)
	requireOK(t, err)
	claimed := claimDSRProbe(t, ctx, e, strings.TrimSpace(string(claimToken)))
	dsrFleetRequest(t, e, "/v1/dsr/claims/"+claimed.CodeSessionID+"/release", strings.TrimSpace(string(claimToken)), map[string]string{"epoch": claimed.WorkerEpoch, "reason": "shutdown"}, 200)
	chatProof(t, started, "dsr_claim_release")
	replacement := startBrainNode(t, e, cfg, "dsr-replacement")
	defer replacement.stop(t)
	final = receiveChatAnswer(t, ctx, events, func() { previews <- struct{}{} })
	waitChatIdle(t, f, 2)
	verifyDSRRecoveryHistory(t, f, []string{firstInput, input}, []string{firstAnswer, final.ID})
	if calls.Load() != 2 {
		t.Fatalf("takeover duplicated model requests: %d", calls.Load())
	}
	current, _, err := e.database.GetCodeSession(t.Context(), f.code.ExternalID)
	requireOK(t, err)
	if current.CurrentWorkerEpoch <= f.code.CurrentWorkerEpoch {
		t.Fatal("takeover did not advance epoch")
	}
	dsrRequest(t, e, "POST", "/v1/dsr/sessions/"+f.code.ExternalID+"/heartbeat", token, map[string]any{}, 409)
	f.code = current
	chatProof(t, started, "dsr_takeover")
}

func dsrOwnerToken(t *testing.T, f *liveSession) string {
	t.Helper()
	identity := brainCredentialIdentity(t, f, f.code.CurrentWorkerEpoch)
	identity.Protocol = "dsr1"
	token, err := f.env.credentials.Issue(identity)
	requireOK(t, err)
	return token
}

func dsrRequest(t *testing.T, e *liveEnv, method, path, token string, body any, status int) []byte {
	t.Helper()
	raw, err := json.Marshal(body)
	requireOK(t, err)
	request, err := http.NewRequestWithContext(t.Context(), method, e.url+path, bytes.NewReader(raw))
	requireOK(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("DSR-Protocol", "1")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Worker-ID", "dsr-interface-probe")
	response, err := http.DefaultClient.Do(request)
	requireOK(t, err)
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	requireOK(t, err)
	if response.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, response.StatusCode, status, result)
	}
	return result
}

func dsrFleetRequest(t *testing.T, e *liveEnv, path, token string, body any, status int) []byte {
	t.Helper()
	return dsrRequest(t, e, "POST", path, token, body, status)
}

func claimDSRProbe(t *testing.T, ctx context.Context, e *liveEnv, token string) dsh.Claim {
	t.Helper()
	for {
		request, err := http.NewRequestWithContext(ctx, "POST", e.url+"/v1/dsr/claims?block_ms=30000", strings.NewReader(`{}`))
		requireOK(t, err)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("DSR-Protocol", "1")
		request.Header.Set("X-Worker-ID", "dsr-interface-probe")
		response, err := http.DefaultClient.Do(request)
		requireOK(t, err)
		if response.StatusCode == 204 {
			response.Body.Close()
			continue
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			t.Fatalf("claim status=%d", response.StatusCode)
		}
		var claim dsh.Claim
		err = json.NewDecoder(response.Body).Decode(&claim)
		response.Body.Close()
		requireOK(t, err)
		return claim
	}
}

func verifyDSRTranscriptInterfaces(t *testing.T, e *liveEnv, f *liveSession, token string) {
	t.Helper()
	base := "/v1/dsr/sessions/" + f.code.ExternalID
	dsrRequest(t, e, "GET", base+"/transcripts?limit=1", token, nil, 200)
	raw := dsrRequest(t, e, "GET", base+"/transcripts/"+f.code.ExternalID+"?from_seq=0&limit_bytes=8388608", token, nil, 200)
	var page struct {
		Header json.RawMessage   `json:"header"`
		Events []json.RawMessage `json:"events"`
	}
	requireOK(t, json.Unmarshal(raw, &page))
	if len(page.Events) == 0 {
		t.Fatal("missing native log")
	}
	dsrRequest(t, e, "PUT", base+"/transcripts/"+f.code.ExternalID+"/header", token, map[string]any{"parent_dsh_session_id": nil, "header": page.Header}, 200)
	dsrRequest(t, e, "POST", base+"/transcripts/"+f.code.ExternalID+"/events", token, map[string]any{"events": page.Events[:1]}, 200)
	dsrRequest(t, e, "POST", base+"/public-events", token, map[string]any{"events": []any{map[string]any{"id": "invalid-output", "type": "user.message", "content": []any{}}}}, 400)
	dsrRequest(t, e, "POST", base+"/inputs/missing/finish", token, map[string]string{"result": "recovered", "failure_code": "interrupted_recovery"}, 400)
	dsrRequest(t, e, "POST", base+"/inputs/missing/finish", token, map[string]string{"result": "failed", "reason": "turn_failed"}, 400)
	for _, event := range chatHistory(t, f) {
		if event.Type != "user.message" {
			continue
		}
		raw := dsrRequest(t, e, "POST", base+"/inputs/delivery", token, map[string]any{"updates": []any{map[string]string{"id": event.ID, "status": "processing"}}}, 200)
		var response dsh.DeliveryResponse
		requireOK(t, json.Unmarshal(raw, &response))
		if len(response.Results) != 1 || response.Results[0].ExecutionStatus != "completed" || bytes.Contains(raw, []byte(`"turn_state"`)) || bytes.Contains(raw, []byte(`"cancel_requested"`)) {
			t.Fatalf("execution response=%s", raw)
		}
		dsrRequest(t, e, "POST", base+"/inputs/"+event.ID+"/finish", token, map[string]string{"result": "success"}, 200)
		dsrRequest(t, e, "POST", base+"/inputs/"+event.ID+"/finish", token, map[string]string{"result": "failed", "failure_code": "execution_failed"}, 409)
		raw = dsrRequest(t, e, "POST", base+"/inputs/delivery", token, map[string]any{"updates": []any{map[string]string{"id": event.ID, "status": "processed"}}}, 200)
		requireOK(t, json.Unmarshal(raw, &response))
		if response.Scheduled != 1 || response.Applied != 0 || response.Results[0].Outcome != "scheduled" {
			t.Fatalf("asynchronous settlement response=%s", raw)
		}

		break
	}

	dsrRequest(t, e, "POST", base+"/inputs/missing/finish", token, map[string]string{"result": "success"}, 409)
}

func verifyDSRTool(t *testing.T, e *liveEnv, cancel bool) {
	t.Helper()
	f := createPublicChat(t, e)
	filename := "/tmp/dsr-tool-" + fmt.Sprint(cancel)
	model := brainToolCallModelWithID(t, "call_brain_write", "Write", fmt.Sprintf(`{"file_path":%q,"content":"dsr proof"}`, filename), nil)
	configureChatModel(t, e, model.URL)
	submitChat(t, f, "Write the verification file then reply done.")
	var toolID string
	waitRealWorker(t, "DSR pending approval", func() bool {
		for _, event := range chatHistory(t, f) {
			if event.Type == "agent.tool_use" && event.EvaluatedPermission == "ask" {
				toolID = event.ID
				return true
			}
		}
		return false
	})
	code, err := e.database.GetCodeSessionBySessionExternalID(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
	requireOK(t, err)
	f.code = code
	token := dsrOwnerToken(t, f)
	id := dsh.PermissionID(code.ExternalID, code.ExternalID, "call_brain_write")
	scope := db.DSHScope{CodeSessionID: code.ExternalID, CodeSessionUUID: code.UUID, SessionID: code.SessionExternalID, OrganizationUUID: code.OrganizationUUID, WorkspaceUUID: code.WorkspaceUUID, Epoch: code.CurrentWorkerEpoch}
	permission, found, err := e.database.GetDSHPermission(t.Context(), scope, id)
	requireOK(t, err)
	if !found || permission.PublicEventID != toolID {
		t.Fatal("public tool and DSR request are not linked")
	}
	request := dsh.PermissionRequest{InputID: permission.InputID, Turn: permission.Turn, DSHSessionID: permission.DSHSessionID, ToolUseID: permission.ToolUseID, Input: permission.Input, Available: permission.Available}
	requireOK(t, json.Unmarshal(permission.Tool, &request.Tool))
	dsrRequest(t, e, "POST", "/v1/dsr/sessions/"+code.ExternalID+"/permission-requests", token, request, 200)
	if cancel {
		dsrRequest(t, e, "POST", "/v1/dsr/sessions/"+code.ExternalID+"/permission-requests/"+id+"/cancel", token, dsh.PermissionCancel{DSHSessionID: code.ExternalID, ToolUseID: "call_brain_write", InputID: permission.InputID}, 200)
	} else {
		e.request(t, "POST", "/v1/sessions/"+f.session.ExternalID+"/events", e.apiKey, map[string]any{"events": []any{map[string]any{"type": "user.tool_confirmation", "tool_use_id": toolID, "result": "allow"}}}, 200)
	}
	waitChatIdle(t, f, 1)
	sandbox, err := e.database.GetResumableEnvironmentSandboxForCodeSession(t.Context(), code.ExternalID)
	if cancel {
		if err == nil && sandbox.ProviderSandboxID != nil {
			t.Fatal("cancelled tool provisioned a sandbox")
		}
		return
	}
	requireOK(t, err)
	output, err := exec.CommandContext(t.Context(), "docker", "exec", *sandbox.ProviderSandboxID, "cat", filename).Output()
	requireOK(t, err)
	if string(output) != "dsr proof" {
		t.Fatalf("tool result=%q", output)
	}
}

func dsrHistoryModel(t *testing.T, previews <-chan struct{}, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	fixture := chatModelFixture(t, previews, calls, true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Anthropic-Version") != "2023-06-01" {
			t.Error("DSR model request lost Anthropic-Version")
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		requireOK(t, err)
		if calls.Load() == 1 {
			for _, expected := range []string{"请回复固定的聊天验证文本", chatAnswer} {
				if bytes.Count(body, []byte(expected)) != 1 {
					t.Errorf("restored context must contain one occurrence of %q", expected)
				}
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		fixture.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func verifyDSRRecoveryHistory(t *testing.T, f *liveSession, inputs, answers []string) {
	t.Helper()
	for _, scope := range []string{"", "/threads/" + answersThreadID(t, f)} {
		var history struct {
			Data []chatEvent `json:"data"`
		}
		requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+scope+"/events?order=asc&limit=1000", f.env.apiKey, nil, 200), &history))
		var actualInputs, actualAnswers []string
		starts, ends := 0, 0
		seen := make(map[string]bool)
		for _, event := range history.Data {
			if seen[event.ID] {
				t.Fatal("duplicate DSR history ID")
			}
			seen[event.ID] = true
			switch event.Type {
			case "user.message":
				if event.Processed == nil {
					t.Fatal("DSR input remains unprocessed")
				}
				actualInputs = append(actualInputs, event.ID)
			case "agent.message":
				if len(event.Content) != 1 || event.Content[0].Text != chatAnswer {
					t.Fatal("DSR answer content changed")
				}
				actualAnswers = append(actualAnswers, event.ID)
			case "span.model_request_start":
				starts++
			case "span.model_request_end":
				ends++
			case "event_start", "event_delta":
				t.Fatal("DSR preview entered history")
			}
		}
		if !slices.Equal(actualInputs, inputs) || !slices.Equal(actualAnswers, answers) || starts != len(inputs) || ends != len(inputs) {
			t.Fatalf("DSR recovered history mismatch: inputs=%v answers=%v starts=%d ends=%d", actualInputs, actualAnswers, starts, ends)
		}
	}
}

func answersThreadID(t *testing.T, f *liveSession) string {
	t.Helper()
	id, found, err := f.env.database.GetPrimarySessionThreadExternalID(t.Context(), f.session.WorkspaceUUID, f.session.ExternalID)
	requireOK(t, err)
	if !found {
		t.Fatal("missing DSR primary thread")
	}
	return id
}

func verifyDSRACKRecovery(t *testing.T, e *liveEnv, f *liveSession, inputID string) {
	t.Helper()
	database := e.database.SQLDB()
	waitRealWorker(t, "initial acknowledged delivery removed", func() bool {
		var remaining int
		requireOK(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM dsh_input_deliveries WHERE organization_uuid=$1 AND workspace_uuid=$2 AND code_session_uuid=$3 AND input_id=$4`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID, inputID).Scan(&remaining))
		return remaining == 0
	})
	original := dsrACKProbeSubject(t, e, f, inputID)
	_, err := database.ExecContext(t.Context(), `INSERT INTO dsh_input_deliveries (organization_uuid,workspace_uuid,code_session_uuid,input_execution_uuid,input_id,ack_subject,status) SELECT organization_uuid,workspace_uuid,code_session_uuid,uuid,input_id,'verify.invalid.ack','pending' FROM dsh_input_executions WHERE organization_uuid=$1 AND workspace_uuid=$2 AND code_session_uuid=$3 AND input_id=$4`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID, inputID)
	requireOK(t, err)
	waitRealWorker(t, "backend ACK retry without Brain", func() bool {
		var attempts int
		requireOK(t, database.QueryRowContext(t.Context(), `SELECT attempts FROM dsh_input_deliveries WHERE organization_uuid=$1 AND workspace_uuid=$2 AND code_session_uuid=$3 AND input_id=$4`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID, inputID).Scan(&attempts))
		return attempts > 0
	})
	_, err = database.ExecContext(t.Context(), `UPDATE code_sessions SET worker_lease_expires_at=now()-interval '1 hour' WHERE organization_uuid=$1 AND workspace_uuid=$2 AND uuid=$3`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID)
	requireOK(t, err)
	if _, found, err := e.database.ClaimNextCodeSession(t.Context(), db.ClaimCodeSessionInput{WorkerProtocol: "dsr1", WorkerID: "ack-must-not-claim", OAuthAccessTokenHash: "unused", ClaimTTL: 30 * time.Second, LeaseGrace: 10 * time.Second, MaxAttempts: 3, ExhaustedHold: time.Minute}); err != nil || found {
		t.Fatalf("pending ACK caused Brain claim: found=%v err=%v", found, err)
	}
	var status, result string
	requireOK(t, database.QueryRowContext(t.Context(), `SELECT status,result FROM dsh_input_executions WHERE organization_uuid=$1 AND workspace_uuid=$2 AND code_session_uuid=$3 AND input_id=$4`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID, inputID).Scan(&status, &result))
	if status != "completed" || result != "success" {
		t.Fatalf("ACK failure changed business result: status=%s result=%s", status, result)
	}
	_, err = database.ExecContext(t.Context(), `UPDATE dsh_input_deliveries SET ack_subject=$5,next_attempt_at=now(),lease_token=NULL,lease_until=NULL WHERE organization_uuid=$1 AND workspace_uuid=$2 AND code_session_uuid=$3 AND input_id=$4`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID, inputID, original)
	requireOK(t, err)
	waitRealWorker(t, "backend settles ACK without Brain", func() bool {
		var remaining int
		requireOK(t, database.QueryRowContext(t.Context(), `SELECT count(*) FROM dsh_input_deliveries WHERE organization_uuid=$1 AND workspace_uuid=$2 AND code_session_uuid=$3 AND input_id=$4`, f.code.OrganizationUUID, f.code.WorkspaceUUID, f.code.UUID, inputID).Scan(&remaining))
		return remaining == 0
	})
}

func verifyDSRStateContracts(t *testing.T, e *liveEnv, f *liveSession, token string) {
	t.Helper()
	base := "/v1/dsr/sessions/" + f.code.ExternalID
	first := json.RawMessage(`{"id":"digest-probe","type":"system.message","content":[{"type":"text","text":"DSR receipt probe"}]}`)
	same := json.RawMessage(`{ "content":[{"text":"DSR receipt probe","type":"text"}],"type":"system.message","id":"digest-probe" }`)
	for index, event := range []json.RawMessage{first, same} {
		raw := dsrRequest(t, e, "POST", base+"/public-events", token, map[string]any{"events": []json.RawMessage{event}}, 200)
		var result dsh.PublishResponse
		requireOK(t, json.Unmarshal(raw, &result))
		if result.Accepted != 1-index || result.Duplicates != index {
			t.Fatalf("digest retry=%s", raw)
		}
	}
	changed := json.RawMessage(`{"id":"digest-probe","type":"system.message","content":[{"type":"text","text":"changed"}]}`)
	dsrRequest(t, e, "POST", base+"/public-events", token, map[string]any{"events": []json.RawMessage{changed}}, 409)
}

func dsrACKProbeSubject(t *testing.T, e *liveEnv, f *liveSession, inputID string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	envelope := workerevents.EventEnvelope(f.code.ExternalID, inputID, "", "user", "", json.RawMessage(`{"type":"user.message","content":[]}`), time.Now().Add(time.Hour))
	requireOK(t, e.broker.Publish(ctx, "ack-delete-probe-"+inputID, envelope))
	sub, err := e.broker.Subscribe(ctx, f.code.ExternalID)
	requireOK(t, err)
	defer sub.Close()
	select {
	case delivery := <-sub.Messages():
		if delivery.Envelope.EventID != inputID || delivery.AckSubject == "" {
			t.Fatal("unexpected ACK probe delivery")
		}
		return delivery.AckSubject
	case err := <-sub.Errors():
		t.Fatalf("ACK probe=%v", err)
	case <-ctx.Done():
		t.Fatal("ACK probe timed out")
	}
	return ""
}
