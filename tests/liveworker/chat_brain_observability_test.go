//go:build integration

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
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/observability"
)

func TestChatBrainObservability(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	if !cfg.Observability.Enabled {
		t.Fatal("real OpenObserve configuration is required")
	}
	_, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	preview := make(chan struct{}, 4)
	var calls atomic.Int32
	model := chatModelFixture(t, preview, &calls, true)
	configureChatModel(t, e, model.URL)
	node := startBrainNode(t, e, cfg, "observability")
	defer node.stop(t)
	f := createPublicChat(t, e)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	events, closeStream := connectChatStream(t, ctx, f)
	defer closeStream()
	input := submitChat(t, f, "请回复固定的聊天验证文本。")
	final := receiveChatAnswer(t, ctx, events, func() { preview <- struct{}{} })
	verifyChatHistory(t, f, input, final)
	if calls.Load() != 1 {
		t.Fatal("unexpected duplicate model calls")
	}
	f.code, err = e.database.GetCodeSessionBySessionExternalID(t.Context(), e.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	waitChatIdle(t, f, 1)
	chatProof(t, started, "brain_observability_roundtrip")

	backend := observability.NewOpenObserve(cfg.Observability.OpenObserve, nil)
	service := observability.New(backend, e.database)
	scope := observability.Scope{OrganizationUUID: e.key.OrganizationUUID.String(), WorkspaceUUID: e.key.WorkspaceUUID.String()}
	window := observability.Window{Start: started.Add(-time.Minute), End: started.Add(time.Hour)}
	main := observability.Target{SessionID: f.session.ExternalID}
	var page observability.Page
	waitRealWorker(t, "native turn/end readable from real OpenObserve", func() bool {
		list, queryErr := service.List(t.Context(), scope, window, observability.ListQuery{})
		if queryErr != nil || !listedSession(list, f.session.ExternalID) {
			return false
		}
		page, queryErr = service.Page(t.Context(), scope, window, main, -1)
		return queryErr == nil && nativeEventCount(page, "turn/end") > 0
	}, 45*time.Second)
	if page.Segment != f.code.ExternalID || len(page.Segments) != 1 || page.Segments[0].ID != f.code.ExternalID {
		t.Fatalf("main lane did not resolve to the code session: %+v", page.Segments)
	}
	verifyStoredNativeHistory(t, f, page)
	suffix, err := service.Page(t.Context(), scope, window, observability.Target{SessionID: f.session.ExternalID, Segment: f.code.ExternalID}, page.NextSeq)
	requireOK(t, err)
	if len(suffix.Events) != 0 || suffix.NextSeq != page.NextSeq {
		t.Fatal("native cursor replay duplicated history")
	}
	chatProof(t, started, "brain_observability_native_history")

	for _, foreign := range []observability.Scope{
		{OrganizationUUID: uuid.NewString(), WorkspaceUUID: scope.WorkspaceUUID, SessionID: f.session.ExternalID},
		{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: uuid.NewString(), SessionID: f.session.ExternalID},
	} {
		list, err := backend.List(t.Context(), foreign, window, observability.ListQuery{Sort: observability.SortRecent})
		requireOK(t, err)
		isolated, err := backend.Page(t.Context(), foreign, window, f.code.ExternalID, -1)
		requireOK(t, err)
		if len(list.Items) != 0 || len(isolated.Events) != 0 {
			t.Fatal("OpenObserve trajectory escaped its tenant scope")
		}
	}
	if _, err := service.Page(t.Context(), observability.Scope{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: uuid.NewString()}, window, main, -1); !isNotFound(err) {
		t.Fatalf("foreign workspace opened the session trajectory: %v", err)
	}
	chatProof(t, started, "brain_observability_isolation")

	configureChatModel(t, e, brainSubagentModel(t, false).URL)
	enableInlineSubagents(t, e)
	delegated, removeDelegated := createRemovableChat(t, e)
	submitChat(t, delegated, "请委派一个子 agent 完成观测验证。")
	childThread := ""
	progress := "no subagent thread"
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("subagent lane progress: %s", progress)
		}
	})
	waitRealWorker(t, "root and subagent lanes readable from real OpenObserve", func() bool {
		if childThread == "" {
			childThread = subagentThreadID(t, delegated)
		}
		if childThread == "" {
			types := []string{}
			for _, event := range chatHistory(t, delegated) {
				types = append(types, event.Type)
			}
			results := delegated.env.request(t, "GET", "/v1/sessions/"+delegated.session.ExternalID+"/events?types[]=agent.tool_result", delegated.env.apiKey, nil, 200)
			progress = fmt.Sprintf("no subagent thread; public events: %v; tool results: %.1000s", types, results)
			return false
		}
		root, rootErr := service.Page(t.Context(), scope, window, observability.Target{SessionID: delegated.session.ExternalID}, -1)
		child, childErr := service.Page(t.Context(), scope, window, observability.Target{SessionID: delegated.session.ExternalID, ThreadID: childThread}, -1)
		progress = fmt.Sprintf("thread=%s root_err=%v root_turn_ends=%d child_err=%v child_events=%d child_turn_ends=%d", childThread, rootErr, nativeEventCount(root, "turn/end"), childErr, len(child.Events), nativeEventCount(child, "turn/end"))
		return rootErr == nil && childErr == nil && nativeEventCount(root, "turn/end") > 0 && nativeEventCount(child, "turn/end") > 0
	}, 60*time.Second)
	verifySubagentLanes(t, service, scope, window, delegated, childThread, f.session.ExternalID)
	chatProof(t, started, "brain_observability_session_lanes")

	agentScope := observability.Scope{OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID, AgentID: e.agent.ExternalID}
	var tabs map[string]map[string]observability.Panel
	waitRealWorker(t, "dashboard aggregates readable from real OpenObserve", func() bool {
		tabs, err = dashboardTabs(t, service, agentScope, window)
		if err != nil {
			return false
		}
		subagents := tabs["subagents"]["subagents"].Data.(observability.TableData)
		failures := statValue(tabs["models"]["model_failure_rate"])
		return len(subagents.Rows) == 1 && failures > 0 && statValue(tabs["tools"]["tool_calls"]) > 0
	}, 45*time.Second)
	verifyDashboards(t, tabs)
	chatProof(t, started, "brain_observability_overview")

	removeDelegated(t.Context())
	list, err := service.List(t.Context(), scope, window, observability.ListQuery{})
	requireOK(t, err)
	if listedSession(list, delegated.session.ExternalID) || !listedSession(list, f.session.ExternalID) {
		t.Fatal("deleted session remained in the trajectory list")
	}
	for _, target := range []observability.Target{{SessionID: delegated.session.ExternalID}, {SessionID: delegated.session.ExternalID, ThreadID: childThread}} {
		if _, err := service.Page(t.Context(), scope, window, target, -1); !isNotFound(err) {
			t.Fatalf("deleted session trajectory remained readable: %v", err)
		}
	}
	chatProof(t, started, "brain_observability_deleted_session")
	verifyTraceExport(t, e, cfg, started)
}

func dashboardTabs(t *testing.T, service *observability.Service, scope observability.Scope, window observability.Window) (map[string]map[string]observability.Panel, error) {
	t.Helper()
	tabs := map[string]map[string]observability.Panel{}
	for _, tab := range []string{"overview", "models", "tools", "subagents"} {
		dashboard, err := service.Dashboard(t.Context(), scope, window, observability.DashboardRequest{Tab: tab})
		if err != nil {
			return nil, err
		}
		tabs[tab] = map[string]observability.Panel{}
		for _, panel := range dashboard.Panels {
			tabs[tab][panel.ID] = panel
		}
	}
	return tabs, nil
}

func statValue(panel observability.Panel) float64 {
	stat, ok := panel.Data.(observability.StatValue)
	if !ok || stat.Value == nil {
		return 0
	}
	return *stat.Value
}

func verifyDashboards(t *testing.T, tabs map[string]map[string]observability.Panel) {
	t.Helper()
	overview, models, tools := tabs["overview"], tabs["models"], tabs["tools"]
	if statValue(overview["active_sessions"]) < 2 || statValue(overview["interactions"]) < 2 || statValue(overview["interaction_duration"]) <= 0 {
		t.Fatalf("interaction stats = %+v %+v %+v", overview["active_sessions"].Data, overview["interactions"].Data, overview["interaction_duration"].Data)
	}
	requests := statValue(models["model_requests"])
	if requests < 4 || statValue(models["model_retry_rate"]) <= 0 || statValue(models["ttft"]) <= 0 {
		t.Fatalf("model stats = %+v %+v %+v", models["model_requests"].Data, models["model_retry_rate"].Data, models["ttft"].Data)
	}
	if statValue(overview["tokens"]) <= 0 {
		t.Fatalf("token usage did not reach the dashboard: %+v", overview["tokens"].Data)
	}
	table := models["model_table"].Data.(observability.TableData)
	if len(table.Rows) != 1 || table.Rows[0]["model"] == "" || *table.Rows[0]["requests"].(*float64) != requests || *table.Rows[0]["failures"].(*float64) != 1 {
		t.Fatalf("model table = %+v", table.Rows)
	}
	delegation := false
	for _, row := range tools["tool_table"].Data.(observability.TableData).Rows {
		if row["tool"] == "send_to_agent" {
			delegation = *row["calls"].(*float64) == 1 && *row["errors"].(*float64) == 0 && *row["total_ms"].(*float64) > 0
		}
	}
	if !delegation {
		t.Fatalf("tools = %+v", tools["tool_table"].Data)
	}
	subagent := tabs["subagents"]["subagents"].Data.(observability.TableData).Rows[0]
	if subagent["name"] != "observer" || subagent["threads"] != 1.0 || subagent["delegations"] != 1.0 || subagent["failures"] != 0.0 || subagent["avg_ms"] == nil {
		t.Fatalf("subagents = %+v", subagent)
	}
	versions := overview["versions"].Data.(observability.TableData)
	reasons := overview["end_reasons"].Data.([]observability.Bar)
	if len(versions.Rows) == 0 || len(reasons) == 0 || reasons[0].Name != "completed" {
		t.Fatalf("versions/reasons = %+v %+v", versions.Rows, reasons)
	}
}

func verifySubagentLanes(t *testing.T, service *observability.Service, scope observability.Scope, window observability.Window, f *liveSession, childThread, otherSession string) {
	t.Helper()
	code, err := f.env.database.GetCodeSessionBySessionExternalID(t.Context(), f.env.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	primary, found, err := f.env.database.GetPrimarySessionThreadExternalID(t.Context(), f.env.key.WorkspaceUUID.String(), f.session.ExternalID)
	requireOK(t, err)
	if !found {
		t.Fatal("delegated session has no primary thread")
	}
	for _, test := range []struct {
		target observability.Target
		native string
	}{
		{observability.Target{SessionID: f.session.ExternalID}, code.ExternalID},
		{observability.Target{SessionID: f.session.ExternalID, ThreadID: primary}, code.ExternalID},
		{observability.Target{SessionID: f.session.ExternalID, ThreadID: childThread}, childThread},
	} {
		page, err := service.Page(t.Context(), scope, window, test.target, -1)
		requireOK(t, err)
		var header struct {
			ID            string `json:"id"`
			ParentSession string `json:"parentSession"`
		}
		requireOK(t, json.Unmarshal(page.Header, &header))
		if header.ID != test.native || page.Segment != test.native || len(page.Segments) != 1 {
			t.Fatalf("lane %+v resolved to %s (%+v)", test.target, header.ID, page.Segments)
		}
		if test.native == childThread && header.ParentSession != code.ExternalID {
			t.Fatalf("subagent lane lost its parent session: %+v", header)
		}
	}
	for _, target := range []observability.Target{
		{SessionID: otherSession, ThreadID: childThread},
		{SessionID: f.session.ExternalID, ThreadID: "sthr_" + strings.ReplaceAll(uuid.NewString(), "-", "")},
		{SessionID: f.session.ExternalID, Segment: childThread},
	} {
		if _, err := service.Page(t.Context(), scope, window, target, -1); !isNotFound(err) {
			t.Fatalf("lane outside the session was readable %+v: %v", target, err)
		}
	}
	list, err := service.List(t.Context(), scope, window, observability.ListQuery{})
	requireOK(t, err)
	matches := 0
	for _, item := range list.Items {
		if item.SessionID != f.session.ExternalID {
			continue
		}
		matches++
		if item.SubagentCount != 1 || item.AgentID != f.env.agent.ExternalID || item.Status == "" || item.StartedAt <= 0 || item.LastEventAt < item.StartedAt {
			t.Fatalf("session summary = %+v", item)
		}
	}
	if matches != 1 {
		t.Fatal("trajectory list did not aggregate native sessions into one OMA session")
	}
}

func enableInlineSubagents(t *testing.T, e *liveEnv) {
	t.Helper()
	var agent struct {
		Version int `json:"version"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "GET", "/v1/agents/"+e.agent.ExternalID, e.apiKey, nil, 200), &agent))
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version":    agent.Version,
		"multiagent": map[string]any{"type": "multiagent_20261001", "subagents": map[string]any{"type": "enabled", "inline_agents": map[string]string{"type": "enabled"}}},
	}, 200)
}

func createRemovableChat(t *testing.T, e *liveEnv) (*liveSession, func(context.Context)) {
	t.Helper()
	var session struct {
		ID string `json:"id"`
	}
	requireOK(t, json.Unmarshal(e.request(t, "POST", "/v1/sessions", e.apiKey, map[string]string{"agent": e.agent.ExternalID, "environment_id": e.environment.ExternalID}, 200), &session))
	removed := false
	remove := func(ctx context.Context) {
		if removed {
			return
		}
		removed = true
		e.requestContext(ctx, t, "POST", "/v1/sessions/"+session.ID+"/archive", e.apiKey, nil, 200)
		e.requestContext(ctx, t, "DELETE", "/v1/sessions/"+session.ID, e.apiKey, nil, 200)
	}
	t.Cleanup(func() { remove(context.Background()) })
	record, _, err := e.database.GetSession(t.Context(), e.key.WorkspaceUUID.String(), session.ID)
	requireOK(t, err)
	return &liveSession{env: e, session: record}, remove
}

func subagentThreadID(t *testing.T, f *liveSession) string {
	t.Helper()
	var threads struct {
		Data []struct {
			ID       string  `json:"id"`
			ParentID *string `json:"parent_thread_id"`
		} `json:"data"`
	}
	requireOK(t, json.Unmarshal(f.env.request(t, "GET", "/v1/sessions/"+f.session.ExternalID+"/threads", f.env.apiKey, nil, 200), &threads))
	for _, thread := range threads.Data {
		if thread.ParentID != nil {
			return thread.ID
		}
	}
	return ""
}

func listedSession(list observability.List, sessionID string) bool {
	return slices.ContainsFunc(list.Items, func(item observability.SessionTrajectory) bool { return item.SessionID == sessionID })
}

func nativeEventCount(page observability.Page, kind string) int {
	count := 0
	for _, raw := range page.Events {
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &event) == nil && event.Type == kind {
			count++
		}
	}
	return count
}

func isNotFound(err error) bool {
	var appError *apperr.Error
	return errors.As(err, &appError) && appError.Kind == apperr.NotFound
}

const brainObservabilityChild = "OMA_OBSERVABILITY_CHILD_V1"

func brainSubagentModel(t *testing.T, rootWrite bool) *httptest.Server {
	t.Helper()
	var calls, rootFailures atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil || r.URL.Path != "/v1/messages" {
			http.Error(w, "unexpected model request", http.StatusBadRequest)
			return
		}
		call := calls.Add(1)
		child := bytes.Contains(body, []byte(brainObservabilityChild)) && !bytes.Contains(body, []byte("call_obs_delegate"))
		if !child && rootFailures.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"scripted overload"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(kind string, fields map[string]any) {
			fields["type"] = kind
			data, _ := json.Marshal(fields)
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data)
			w.(http.Flusher).Flush()
		}
		write("message_start", map[string]any{"message": map[string]any{"id": fmt.Sprintf("msg_obs_subagent_%d", call), "type": "message", "role": "assistant", "model": "claude-sonnet-4-6", "content": []any{}, "stop_reason": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}})
		stop := "end_turn"
		if rootWrite && !child && !bytes.Contains(body, []byte(traceWriteCallID)) {
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": traceWriteCallID, "name": "Write", "input": map[string]any{}}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"file_path":"/tmp/oma-trace-proof.txt","content":"traced write"}`}})
			stop = "tool_use"
		} else if !child && !bytes.Contains(body, []byte("call_obs_delegate")) {
			arguments, _ := json.Marshal(map[string]string{"name": "observer", "system_prompt": brainObservabilityChild, "message": "请返回子 agent 观测验证结果。"})
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "call_obs_delegate", "name": "send_to_agent", "input": map[string]any{}}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(arguments)}})
			stop = "tool_use"
		} else {
			text := "主 agent 已收到子 agent 结果。"
			if child {
				text = "子 agent 观测验证完成。"
			}
			write("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
			write("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
		}
		write("content_block_stop", map[string]any{"index": 0})
		write("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 12}})
		write("message_stop", map[string]any{})
	}))
	t.Cleanup(server.Close)
	return server
}

func verifyStoredNativeHistory(t *testing.T, f *liveSession, page observability.Page) {
	t.Helper()
	var header struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	requireOK(t, json.Unmarshal(page.Header, &header))
	if header.ID != f.code.ExternalID || header.Version != 4 {
		t.Fatal("native Session header was not preserved")
	}
	rows, _, err := f.env.service.EventPayloadStore().ListCodeSessionInternalEventsPage(t.Context(), db.ListCodeSessionInternalEventsPageParams{WorkspaceUUID: f.code.WorkspaceUUID, CodeSessionExternalID: f.code.ExternalID, Limit: 500})
	requireOK(t, err)
	originals := map[int]json.RawMessage{}
	for _, row := range rows {
		var payload struct {
			Subtype string `json:"subtype"`
			Data    struct {
				Header struct {
					ID string `json:"id"`
				} `json:"header"`
				Events []json.RawMessage `json:"events"`
			} `json:"data"`
		}
		requireOK(t, json.Unmarshal(row.Payload, &payload))
		if payload.Subtype != "dsh_log" || payload.Data.Header.ID != f.code.ExternalID {
			continue
		}
		for _, raw := range payload.Data.Events {
			var event struct {
				Seq int `json:"seq"`
			}
			requireOK(t, json.Unmarshal(raw, &event))
			originals[event.Seq] = raw
		}
	}
	if len(page.Events) == 0 || len(originals) == 0 {
		t.Fatal("native ledger is empty")
	}
	assistant := false
	for seq, raw := range page.Events {
		var envelope struct {
			Seq  int    `json:"seq"`
			Type string `json:"type"`
		}
		requireOK(t, json.Unmarshal(raw, &envelope))
		if envelope.Seq != seq {
			t.Fatal("native history lost event ordering")
		}
		assistant = assistant || envelope.Type == "assistant/message"
		original, found := originals[seq]
		if !found {
			t.Fatalf("OpenObserve event %d has no canonical source", seq)
		}
		var want, got any
		requireOK(t, json.Unmarshal(original, &want))
		requireOK(t, json.Unmarshal(raw, &got))
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("OpenObserve changed native event %d", seq)
		}
	}
	if !assistant {
		t.Fatal("native model output is missing")
	}
}
