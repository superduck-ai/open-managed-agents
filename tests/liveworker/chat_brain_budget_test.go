//go:build integration || e2e || e2b_integration

package liveworker

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/billing"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestChatBrainBudget(t *testing.T) {
	isolatedChat(t)
	started := time.Now()
	e := newLiveEnv(t)
	cfg, err := config.Load()
	requireOK(t, err)
	_, stop := startLazyBrainRunner(t, e, cfg)
	defer stop()
	brainURL, denied := observeBudgetDenials(t, e.url)
	brainEnv := *e
	brainEnv.url = brainURL
	node := startBrainNode(t, &brainEnv, cfg, "budget")
	defer node.stop(t)
	const modelID = "claude-sonnet-4-6"
	price := db.CatalogPrice{Model: modelID, Original: []byte(`{}`), Price: billing.ModelPrice{Source: "openrouter", SourceModel: modelID, Rates: &billing.Rates{Input: new("1000"), Output: new("1000")}}}
	requireOK(t, e.database.ReplaceModelPriceCatalog(t.Context(), "openrouter", []db.CatalogPrice{price}))
	e.request(t, "POST", "/v1/agents/"+e.agent.ExternalID, e.apiKey, map[string]any{
		"version": e.agent.CurrentVersion, "model": map[string]string{"id": modelID},
		"tools": []any{map[string]any{"type": "agent_toolset_20260401", "default_config": map[string]any{"enabled": true, "permission_policy": map[string]string{"type": "always_allow"}}}},
	}, http.StatusOK)
	for _, tc := range []struct {
		name string
		next any
	}{{"raise", testSessionBudget("1000")}, {"remove", nil}} {
		t.Run(tc.name, func(t *testing.T) {
			verifyBudgetContinuation(t, e, tc.name, tc.next, denied)
			chatProof(t, started, "budget_"+tc.name+"_resumed")
		})
	}
}

func testSessionBudget(amount string) any {
	return map[string]any{"type": "limit", "max_list_cost": map[string]string{"amount": amount, "currency": "USD"}}
}

func observeBudgetDenials(t *testing.T, targetURL string) (string, <-chan string) {
	t.Helper()
	target, err := url.Parse(targetURL)
	requireOK(t, err)
	denied := make(chan string, 4)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(response *http.Response) error {
		if response.Request.URL.Path != "/v1/messages" || response.StatusCode != http.StatusBadRequest {
			return nil
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			return err
		}
		response.Body = io.NopCloser(bytes.NewReader(body))
		if bytes.Contains(body, []byte("session budget reached")) {
			select {
			case denied <- response.Header.Get("request-id"):
			default:
			}
		}
		return nil
	}
	observer := httptest.NewServer(proxy)
	t.Cleanup(func() { observer.CloseClientConnections(); observer.Close() })
	return observer.URL, denied
}

func budgetTaskModel(t *testing.T, calls *atomic.Int32) string {
	t.Helper()
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req compactionRequest
		if err := json.UnmarshalRead(r.Body, &req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if calls.Add(1) == 1 {
			writeCompactionResponse(w, req.Model, 100, "", "Bash", map[string]string{"command": "printf BUDGET_TASK_PROOF"})
			return
		}
		for _, m := range req.Messages {
			if hasBudgetToolProof(m.Content) {
				writeCompactionResponse(w, req.Model, 100, "BUDGET_TASK_DONE", "", nil)
				return
			}
		}
		t.Error("resumed model request lost the tool result")
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(model.Close)
	return model.URL
}

func hasBudgetToolProof(content []byte) bool {
	var blocks []struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		IsError bool   `json:"is_error"`
	}
	if err := json.Unmarshal(content, &blocks); err != nil {
		return false
	}
	for _, block := range blocks {
		if block.Type == "tool_result" && !block.IsError && strings.Contains(block.Content, "BUDGET_TASK_PROOF") {
			return true
		}
	}
	return false
}

func TestBudgetToolProof(t *testing.T) {
	for _, content := range []string{
		`[{"type":"text","text":"BUDGET_TASK_PROOF"},{"type":"tool_result","content":"wrong output"}]`,
		`[{"type":"tool_result","is_error":true,"content":"BUDGET_TASK_PROOF"}]`,
	} {
		if hasBudgetToolProof([]byte(content)) {
			t.Fatalf("accepted invalid tool proof: %s", content)
		}
	}
	if !hasBudgetToolProof([]byte(`[{"type":"tool_result","content":"BUDGET_TASK_PROOF"}]`)) {
		t.Fatal("missing successful tool output")
	}
}

func verifyBudgetContinuation(t *testing.T, e *liveEnv, name string, replacement any, denied <-chan string) {
	t.Helper()
	var calls atomic.Int32
	configureChatModel(t, e, budgetTaskModel(t, &calls))
	f := createPublicChatWithBudget(t, e, "Budget "+name, testSessionBudget("1"))
	path := "/v1/sessions/" + f.session.ExternalID
	input := submitChat(t, f, "Run printf BUDGET_TASK_PROOF, then report BUDGET_TASK_DONE.")
	t.Cleanup(func() {
		data := e.requestContext(context.Background(), t, "GET", path+"/events?order=asc&limit=100", e.apiKey, nil, http.StatusOK)
		t.Logf("BUDGET_EVENTS session=%s change=%s %s", f.session.ExternalID, name, data)
	})
	select {
	case requestID := <-denied:
		t.Logf("session=%s budget gate rejected the next model request, HTTP 400 request_id=%s", f.session.ExternalID, requestID)
	case <-time.After(30 * time.Second):
		t.Fatal("worker did not encounter the budget gate")
	}
	eventually(t, 10*time.Second, func() bool {
		hist := chatHistory(t, f)
		return slices.ContainsFunc(hist, func(ev chatEvent) bool {
			return ev.Type == "session.status_idle" && ev.StopReason.Type == "budget_reached"
		}) && slices.ContainsFunc(hist, successfulBudgetTool)
	})
	if calls.Load() != 1 {
		t.Fatalf("budget failed to pause the task, model calls=%d", calls.Load())
	}
	e.request(t, "POST", path, e.apiKey, map[string]any{"budget": replacement}, http.StatusOK)
	var history []chatEvent
	eventually(t, 45*time.Second, func() bool {
		history = chatHistory(t, f)
		return slices.ContainsFunc(history, func(ev chatEvent) bool { return ev.Type == "session.status_idle" && ev.StopReason.Type == "end_turn" })
	})
	verifyBudgetEvents(t, history, input)
	if calls.Load() != 2 {
		t.Fatalf("model calls=%d, want two", calls.Load())
	}
}

func successfulBudgetTool(ev chatEvent) bool {
	return ev.Type == "agent.tool_result" && !ev.IsError && len(ev.Content) == 1 && ev.Content[0].Text == "BUDGET_TASK_PROOF"
}

func verifyBudgetEvents(t *testing.T, history []chatEvent, input string) {
	t.Helper()
	var inputs, continuation []string
	var uses, results, updates int
	for _, ev := range history {
		switch ev.Type {
		case "user.message":
			inputs = append(inputs, ev.ID)
		case "agent.tool_use":
			uses++
		case "agent.tool_result":
			results++
		case "session.updated":
			updates++
		case "session.error":
			t.Fatalf("unexpected session.error %s", ev.ID)
		case "session.status_idle":
			if ev.StopReason.Type != "budget_reached" && ev.StopReason.Type != "end_turn" {
				t.Fatalf("unexpected idle %s: %s", ev.ID, ev.StopReason.Type)
			}
			if updates > 0 {
				continuation = append(continuation, ev.StopReason.Type)
			}
		case "session.status_running", "span.model_request_start":
			if updates > 0 {
				continuation = append(continuation, ev.Type)
			}
		case "agent.message":
			if updates > 0 && len(ev.Content) == 1 {
				continuation = append(continuation, ev.Content[0].Text)
			}
		}
	}
	if !slices.Equal(inputs, []string{input}) || uses != 1 || results != 1 || updates != 1 {
		t.Fatalf("inputs=%v tool uses=%d results=%d budget updates=%d", inputs, uses, results, updates)
	}
	if !slices.Equal(continuation, []string{"session.status_running", "span.model_request_start", "BUDGET_TASK_DONE", "end_turn"}) {
		t.Fatalf("after budget update: %v", continuation)
	}
}
