package tests

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
	sessionsapi "github.com/superduck-ai/open-managed-agents/internal/sessions"
)

func TestHostAgentRuntimeDatabaseAndMemoryBroker(t *testing.T) {
	app := newPayloadIntegrationApp(t, newFakeStore("host-agent-runtime"))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"host-agent-runtime","tools":[{"type":"agent_toolset_20260401","configs":[{"name":"write","enabled":true,"permission_policy":{"type":"always_ask"}}]}]}`)
	env := createEnvironment(t, app, `{"name":"host-agent-runtime"}`)
	publicSession := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(env.ID)+`}`)
	workspaceUUID := getDefaultDBIDs(t, app.pool).WorkspaceUUID
	session, found, err := app.db.GetSession(ctx, workspaceUUID, publicSession.ID)
	if err != nil || !found {
		t.Fatalf("load public session: found=%t error=%v", found, err)
	}
	environment, err := app.db.GetEnvironment(ctx, workspaceUUID, env.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, err := app.db.GetLatestEnvironmentWorkForSession(ctx, workspaceUUID, env.ID, session.UUID)
	if err != nil {
		t.Fatal(err)
	}
	service := newCodeSessionService(app, nil, nil)
	service.SetPublicEventSink(sessionsapi.NewHandler(app.cfg, app.db, service, nil, nil, app.vaultSecrets, nil))
	created, err := service.CreateManagedAgentCodeSession(ctx, codesessions.ManagedAgentCreateInput{
		Session: session, Environment: environment, EnvironmentWork: work,
		Model: "claude-opus-4-6", Config: json.RawMessage(`{"agent_runtime_mode":"host"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	patch := json.RawMessage(`{"claude_code_session_id":` + quoteJSON(created.CodeSessionID) + `,"runtime":"crush_host","agent_runtime_mode":"host"}`)
	if err := app.db.BindManagedAgentRuntimeMetadata(ctx, session, work, patch, patch); err != nil {
		t.Fatal(err)
	}
	if _, err := service.OpenHostWorker(ctx, created.CodeSessionID, created.WorkerEpoch+1); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("unreserved epoch accepted: %v", err)
	}
	worker, err := service.OpenHostWorker(ctx, created.CodeSessionID, created.WorkerEpoch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Close(context.Background()) }()
	record, err := getCodeSession(app, ctx, created.CodeSessionID)
	if err != nil || record.CurrentWorkerEpoch != created.WorkerEpoch {
		t.Fatalf("registration changed reserved epoch: epoch=%d error=%v", record.CurrentWorkerEpoch, err)
	}
	if err := worker.AppendHistory(ctx, []codesessions.HostHistoryEntry{{ID: "invalid", Role: "message", Payload: json.RawMessage(`{}`)}}); !errors.Is(err, codesessions.ErrHostHistoryInvalid) {
		t.Fatalf("invalid private history accepted: %v", err)
	}
	if err := worker.SetState(ctx, "unknown"); !errors.Is(err, codesessions.ErrProtocol) {
		t.Fatalf("invalid worker state accepted: %v", err)
	}
	if _, err := worker.RequestPermission(ctx, codesessions.HostToolRequest{}); !errors.Is(err, codesessions.ErrHostPermissionInvalid) {
		t.Fatalf("invalid permission request accepted: %v", err)
	}
	initialize, err := worker.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var initializePayload struct {
		Type    string `json:"type"`
		Request struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal(initialize.Payload, &initializePayload); err != nil || initializePayload.Type != "control_request" || initializePayload.Request.Subtype != "initialize" {
		t.Fatalf("initial native delivery: type=%q subtype=%q error=%v", initializePayload.Type, initializePayload.Request.Subtype, err)
	}
	if err := worker.Acknowledge(ctx, initialize.EventID, "processed"); err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("x", 40000)
	sent := sendSessionEvents(t, app, session.ExternalID, `{"events":[{"type":"user.message","content":[{"type":"text","text":`+quoteJSON(text)+`}]}]}`, defaultTestKey)
	input, err := worker.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var inputPayload struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(input.Payload, &inputPayload); err != nil || inputPayload.Type != "user" || inputPayload.ID != sessionEventStringField(t, sent.Data[0], "id") || inputPayload.Message.Content != text {
		t.Fatalf("native user delivery lost public identity or offloaded content: type=%q error=%v", inputPayload.Type, err)
	}
	runID := codesessions.HostRunID(created.CodeSessionID, input.EventID)
	entries := []codesessions.HostHistoryEntry{
		{ID: "native-user", Role: "message", RunID: runID, InputEventID: input.EventID, Payload: json.RawMessage(`{"role":"user","content":` + quoteJSON(text) + `}`)},
		{ID: "native-start", Role: "run_started", RunID: runID, InputEventID: input.EventID, Payload: json.RawMessage(`{}`)},
	}
	if err := worker.AppendHistory(ctx, entries); err != nil {
		t.Fatal(err)
	}
	if err := worker.AppendHistory(ctx, entries); err != nil {
		t.Fatalf("private history replay: %v", err)
	}
	history, err := worker.LoadHistory(ctx)
	if err != nil || len(history) != len(entries) {
		t.Fatalf("private history deduplication: entries=%d error=%v", len(history), err)
	}
	for i, entry := range history {
		if entry.ID != entries[i].ID || entry.Role != entries[i].Role || entry.RunID != runID || entry.InputEventID != input.EventID {
			t.Fatalf("private history changed entry %d", i)
		}
	}
	var privateMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(history[0].Payload, &privateMessage); err != nil || privateMessage.Role != "user" || privateMessage.Content != text {
		t.Fatalf("private history lost offloaded provider payload: role=%q error=%v", privateMessage.Role, err)
	}
	for _, status := range []string{"processing", "processed", "processed"} {
		if err := worker.Acknowledge(ctx, input.EventID, status); err != nil {
			t.Fatalf("native input %s ACK: %v", status, err)
		}
	}
	if pending := app.workerEvents.Pending(created.CodeSessionID); len(pending) != 0 {
		t.Fatalf("native processed ACK left %d broker deliveries", len(pending))
	}
	if err := worker.SetState(ctx, "running"); err != nil {
		t.Fatal(err)
	}
	if status := retrieveSession(t, app, session.ExternalID, defaultTestKey).Status; status != "running" {
		t.Fatalf("native running state was not public: %q", status)
	}
	finalPayload := json.RawMessage(`{"id":"native-final","type":"agent.message","content":[{"type":"text","text":"native answer"}]}`)
	for range 2 {
		if err := worker.Publish(ctx, finalPayload); err != nil {
			t.Fatal(err)
		}
	}
	finals := listSessionEvents(t, app, session.ExternalID, "types[]=agent.message&order=asc", defaultTestKey)
	if len(finals.Data) != 1 || sessionEventStringField(t, finals.Data[0], "id") != "native-final" {
		t.Fatalf("native durable output changed its identity or duplicated: %s", finals.Data)
	}
	request := codesessions.HostToolRequest{RequestID: "host-write-request", ToolUseID: "host-write-tool", ToolName: "Write", Input: json.RawMessage(`{"file_path":"/workspace/native.txt","content":"native"}`)}
	permission, err := worker.RequestPermission(ctx, request)
	if err != nil || permission.Behavior != "ask" || permission.RequestID != request.RequestID || permission.PublicEventID == "" {
		t.Fatalf("native tool permission: %+v error=%v", permission, err)
	}
	if pending := app.workerEvents.Pending(created.CodeSessionID); len(pending) != 0 {
		t.Fatal("ask permission queued an answer before public confirmation")
	}
	if err := worker.SetState(ctx, "requires_action"); err != nil {
		t.Fatal(err)
	}
	publicEvents := listSessionEvents(t, app, session.ExternalID, "order=desc&limit=100", defaultTestKey)
	toolEvent := sessionEventObjectByType(t, publicEvents, "agent.tool_use")
	if toolEvent["id"] != permission.PublicEventID || toolEvent["evaluated_permission"] != "ask" {
		t.Fatalf("native public tool permission: %#v", toolEvent)
	}
	confirmation := sendSessionEvents(t, app, session.ExternalID, `{"events":[{"type":"user.tool_confirmation","tool_use_id":`+quoteJSON(permission.PublicEventID)+`,"result":"allow"}]}`, defaultTestKey)
	answer, err := worker.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var controlResponse struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Response struct {
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior  string `json:"behavior"`
				ToolUseID string `json:"toolUseID"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(answer.Payload, &controlResponse); err != nil || controlResponse.Type != "control_response" || controlResponse.ID != sessionEventStringField(t, confirmation.Data[0], "id") || controlResponse.Response.RequestID != request.RequestID || controlResponse.Response.Response.Behavior != "allow" || controlResponse.Response.Response.ToolUseID != request.ToolUseID {
		t.Fatalf("native permission answer lost confirmation or tool identity: %+v error=%v", controlResponse, err)
	}
	if err := worker.Acknowledge(ctx, answer.EventID, "processed"); err != nil {
		t.Fatal(err)
	}
	if pending := app.workerEvents.Pending(created.CodeSessionID); len(pending) != 0 {
		t.Fatal("confirmed tool answer remained in the broker after ACK")
	}
	if err := worker.SetState(ctx, "running"); err != nil {
		t.Fatal(err)
	}
	request.RequestID, request.ToolUseID = "host-failed-request", "host-failed-tool"
	permission, err = worker.RequestPermission(ctx, request)
	if err != nil || permission.Behavior != "ask" {
		t.Fatalf("second native permission: %+v error=%v", permission, err)
	}
	if err := worker.SetState(ctx, "requires_action"); err != nil {
		t.Fatal(err)
	}
	record, err = getCodeSession(app, ctx, created.CodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	pendingIDs, err := maevents.PendingToolEventIDs(record.WorkerExternalMetadata, "", "")
	if err != nil || len(pendingIDs) != 1 || pendingIDs[0] != permission.PublicEventID {
		t.Fatalf("native pending permission before failed turn: %v error=%v", pendingIDs, err)
	}
	if err := worker.EndTurn(ctx, true); err != nil {
		t.Fatal(err)
	}
	record, err = getCodeSession(app, ctx, created.CodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	pendingIDs, err = maevents.PendingToolEventIDs(record.WorkerExternalMetadata, "", "")
	if err != nil || len(pendingIDs) != 0 || record.WorkerStatus != "idle" {
		t.Fatalf("failed native turn retained approval or worker state: ids=%v state=%q error=%v", pendingIDs, record.WorkerStatus, err)
	}
	publicEvents = listSessionEvents(t, app, session.ExternalID, "types[]=session.status_idle&order=desc&limit=100", defaultTestKey)
	var failedIdle struct {
		StopReason struct {
			Type string `json:"type"`
		} `json:"stop_reason"`
	}
	if len(publicEvents.Data) == 0 {
		t.Fatal("failed native turn did not publish idle")
	}
	if err := json.Unmarshal(publicEvents.Data[0], &failedIdle); err != nil || failedIdle.StopReason.Type != "retries_exhausted" {
		t.Fatalf("failed native turn public stop reason: %q error=%v", failedIdle.StopReason.Type, err)
	}
	if status := retrieveSession(t, app, session.ExternalID, defaultTestKey).Status; status != "idle" {
		t.Fatalf("failed native turn public status: %q", status)
	}
	if err := worker.Close(ctx); err != nil {
		t.Fatal(err)
	}
	replacement, err := service.RecoverManagedAgentCodeSession(ctx, codesessions.ManagedAgentRecoverInput{Session: session, CodeSessionID: created.CodeSessionID})
	if err != nil || replacement.WorkerEpoch != created.WorkerEpoch+1 {
		t.Fatalf("native replacement epoch: %d error=%v", replacement.WorkerEpoch, err)
	}
	if _, err := service.OpenHostWorker(ctx, created.CodeSessionID, created.WorkerEpoch); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale native registration accepted: %v", err)
	}
	replacementWorker, err := service.OpenHostWorker(ctx, created.CodeSessionID, replacement.WorkerEpoch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = replacementWorker.Close(context.Background()) }()
	if err := worker.AppendHistory(ctx, entries); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale private history append accepted: %v", err)
	}
	if _, err := worker.LoadHistory(ctx); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale private history read accepted: %v", err)
	}
	if err := worker.SetState(ctx, "running"); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale native state accepted: %v", err)
	}
	if err := worker.EndTurn(ctx, true); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale native completion accepted: %v", err)
	}
	if err := worker.Heartbeat(ctx); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale native heartbeat accepted: %v", err)
	}
	if err := worker.Publish(ctx, json.RawMessage(`{"id":"sevt_stale_native","type":"session.status_running"}`)); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale native output accepted: %v", err)
	}
	if err := worker.Acknowledge(ctx, input.EventID, "processed"); !errors.Is(err, db.ErrWorkerEpochMismatch) {
		t.Fatalf("stale native ACK accepted: %v", err)
	}
	recoveredHistory, err := replacementWorker.LoadHistory(ctx)
	if err != nil || len(recoveredHistory) != len(entries) || recoveredHistory[1].Role != "run_started" || recoveredHistory[1].RunID != runID {
		t.Fatalf("replacement lost unresolved run marker: entries=%d error=%v", len(recoveredHistory), err)
	}
}
