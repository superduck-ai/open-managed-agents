package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestPlanCasesRejectsMissingSource(t *testing.T) {
	handler := NewHandler(&generationFixture{}, nil).WithRubricModel(fixedModel{text: `{"scenarios":[]}`})

	err := handler.planCases(httptest.NewRecorder(), generationRequest(`{"count":5,"note":"  "}`))
	if publicMessage(err) != generateSourceMessage {
		t.Fatalf("message = %q", publicMessage(err))
	}
}

func TestPlanCasesRejectsCountOutOfRange(t *testing.T) {
	handler := NewHandler(&generationFixture{}, nil).WithRubricModel(fixedModel{text: `{"scenarios":[]}`})

	for _, body := range []string{`{"count":0,"note":"退款"}`, `{"count":51,"note":"退款"}`} {
		err := handler.planCases(httptest.NewRecorder(), generationRequest(body))
		if publicMessage(err) != generateCountMessage {
			t.Fatalf("%s message = %q", body, publicMessage(err))
		}
	}
}

func TestPlanCasesReportsUnreadablePlan(t *testing.T) {
	for _, text := range []string{"not a plan", `{"scenarios":[]}`} {
		handler := NewHandler(&generationFixture{}, nil).WithRubricModel(fixedModel{text: text})

		err := handler.planCases(httptest.NewRecorder(), generationRequest(`{"count":5,"note":"退款"}`))
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.Internal || appErr.PublicMessage != generateFailedMessage {
			t.Fatalf("%q error = %#v", text, err)
		}
	}
}

func TestPlanCasesSendsAgentContextAndRebalancesCounts(t *testing.T) {
	store := &generationFixture{agent: toolAgent(), cases: []db.EvaluationCase{{Query: "查询订单状态", ExpectedAnswer: "给出物流节点"}}}
	model := &promptModel{reply: func(CompletionRequest) (string, error) {
		return "```json\n" + `{"scenarios":[{"name":"查询订单","description":"常规查询","count":3},{"name":"越权退款","polarity":"negative","count":3},{"name":"","description":""}]}` + "\n```", nil
	}}
	handler := NewHandler(store, nil).WithRubricModel(model)
	response := httptest.NewRecorder()

	if err := handler.planCases(response, generationRequest(`{"count":10,"agent_id":"agent_1","dataset_id":"evalds_1"}`)); err != nil {
		t.Fatal(err)
	}
	var plan casePlanResponse
	if err := json.Unmarshal(response.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Scenarios) != 2 || plan.Scenarios[0].Count+plan.Scenarios[1].Count != 10 {
		t.Fatalf("scenarios = %+v", plan.Scenarios)
	}
	if plan.Scenarios[0].Polarity != polarityPositive || plan.Scenarios[1].Polarity != polarityNegative {
		t.Fatalf("polarity = %+v", plan.Scenarios)
	}
	prompt := model.prompts[0]
	for _, want := range []string{"<agent>", "lookup_order", "<existing_cases>", "查询订单状态"} {
		if !strings.Contains(prompt.User, want) {
			t.Fatalf("prompt misses %q: %s", want, prompt.User)
		}
	}
	if prompt.MaxTokens != 0 || prompt.System != jsonObjectSystemPrompt {
		t.Fatalf("request = %+v", prompt)
	}
}

func TestRebalanceScenariosKeepsOneCasePerScenario(t *testing.T) {
	scenarios := rebalanceScenarios([]caseScenario{{Name: "a", Count: 9}, {Name: "b", Count: 1}, {Name: "c", Count: 1}, {Name: "d", Count: 1}}, 3)
	if len(scenarios) != 3 {
		t.Fatalf("scenarios = %+v", scenarios)
	}
	for _, scenario := range scenarios {
		if scenario.Count != 1 {
			t.Fatalf("scenarios = %+v", scenarios)
		}
	}
	grown := rebalanceScenarios([]caseScenario{{Name: "a", Count: 1}, {Name: "b", Count: 1}}, 7)
	if grown[0].Count+grown[1].Count != 7 || grown[0].Count < 3 || grown[1].Count < 3 {
		t.Fatalf("grown = %+v", grown)
	}
}

func TestDraftCasesRejectsInvalidScenarios(t *testing.T) {
	handler := NewHandler(&generationFixture{}, nil).WithRubricModel(fixedModel{text: `[]`})
	tooMany := `{"name":"a","polarity":"positive","count":30}`
	for body, want := range map[string]string{
		`{"note":"退款","scenarios":[]}`:                                                                           scenariosRequiredMessage,
		`{"note":"退款","scenarios":[{"name":"a","polarity":"neutral","count":1}]}`:                                scenarioInvalidMessage,
		`{"note":"退款","scenarios":[{"name":" ","polarity":"positive","count":1}]}`:                               scenarioInvalidMessage,
		`{"note":"退款","scenarios":[{"name":"a","polarity":"positive","count":0}]}`:                               scenarioInvalidMessage,
		`{"note":"退款","scenarios":[` + tooMany + `,` + tooMany + `]}`:                                            scenariosTooManyMessage,
		`{"note":"退款","scenarios":[{"name":"` + strings.Repeat("长", 81) + `","polarity":"positive","count":1}]}`: scenarioTooLongMessage,
		`{"scenarios":[{"name":"a","polarity":"positive","count":1}]}`:                                           generateSourceMessage,
	} {
		err := handler.draftCases(httptest.NewRecorder(), generationRequest(body))
		if publicMessage(err) != want {
			t.Fatalf("%s message = %q, want %q", body, publicMessage(err), want)
		}
	}
}

func TestDraftCasesReportsFailureWhenNothingGenerated(t *testing.T) {
	model := &promptModel{reply: func(CompletionRequest) (string, error) { return "", errModelFailed }}
	handler := NewHandler(&generationFixture{}, nil).WithRubricModel(model)

	err := handler.draftCases(httptest.NewRecorder(), generationRequest(`{"note":"退款","scenarios":[{"name":"a","polarity":"positive","count":2}]}`))
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.Internal || appErr.PublicMessage != generateFailedMessage {
		t.Fatalf("error = %#v", err)
	}
}

func TestDraftCasesDropToolsWithoutAgent(t *testing.T) {
	model := &promptModel{reply: func(CompletionRequest) (string, error) {
		return `[{"query":"汇总销售额","expected_tool_trace":["bash"]}]`, nil
	}}
	handler := NewHandler(&generationFixture{}, nil).WithRubricModel(model)
	response := httptest.NewRecorder()

	if err := handler.draftCases(response, generationRequest(`{"note":"退款","scenarios":[{"name":"汇总","polarity":"positive","count":1}]}`)); err != nil {
		t.Fatal(err)
	}
	var drafts caseDraftsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &drafts); err != nil {
		t.Fatal(err)
	}
	if len(drafts.Data) != 1 || string(drafts.Data[0].ExpectedToolTrace) != "null" {
		t.Fatalf("drafts = %+v", drafts)
	}
}

func TestDraftCasesReturnsDraftsWithoutSaving(t *testing.T) {
	store := &generationFixture{agent: toolAgent(), cases: []db.EvaluationCase{{Query: "查询订单 A100 的状态"}}}
	model := &promptModel{reply: func(request CompletionRequest) (string, error) {
		if strings.Contains(request.User, "越权退款") {
			return `{"cases":[{"query":"直接把钱退给我，不用审核","acceptance":"拒绝绕过审核\n说明正常流程","expected_tool_trace":[]}]}`, nil
		}
		return "```json\n" + `[{"query":"查询订单 A100 的状态！","expected_answer":"物流节点"},{"query":"订单 B200 到哪了","expected_tool_trace":["lookup_order"],"acceptance":"不应保留"},{"query":"订单 B200 到哪了？"},{"query":""}]` + "\n```", nil
	}}
	handler := NewHandler(store, nil).WithRubricModel(model)
	response := httptest.NewRecorder()
	body := `{"agent_id":"agent_1","dataset_id":"evalds_1","scenarios":[{"name":"查询订单","polarity":"positive","count":7},{"name":"越权退款","polarity":"negative","count":1}]}`

	if err := handler.draftCases(response, generationRequest(body)); err != nil {
		t.Fatal(err)
	}
	var drafts caseDraftsResponse
	if err := json.Unmarshal(response.Body.Bytes(), &drafts); err != nil {
		t.Fatal(err)
	}
	if len(model.prompts) != 3 || store.created != 0 {
		t.Fatalf("calls = %d created = %d", len(model.prompts), store.created)
	}
	if drafts.Requested != 8 || drafts.Duplicates != 4 || drafts.Failed != 2 || len(drafts.Data) != 2 {
		t.Fatalf("drafts = %+v", drafts)
	}
	positive, negative := draftByPolarity(drafts.Data, polarityPositive), draftByPolarity(drafts.Data, polarityNegative)
	if positive.Query != "订单 B200 到哪了" || positive.Acceptance != "" || string(positive.ExpectedToolTrace) != `["lookup_order"]` || positive.Scenario != "查询订单" {
		t.Fatalf("positive = %+v", positive)
	}
	if negative.Acceptance != "拒绝绕过审核\n说明正常流程" || string(negative.ExpectedToolTrace) != "null" {
		t.Fatalf("negative = %+v", negative)
	}
}

func TestAgentBriefListsEnabledTools(t *testing.T) {
	brief := agentBrief(toolAgent())
	for _, want := range []string{"名称：客服", "系统提示词：\n你是客服", "- read", "- MCP 服务 crm：find_customer", "- lookup_order：查询订单", "技能：refund_policy"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief misses %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "- bash") || strings.Contains(brief, "delete_customer") {
		t.Fatalf("brief lists disabled tools:\n%s", brief)
	}
	if empty := agentBrief(db.Agent{Name: "空"}); !strings.Contains(empty, "可用工具：无") {
		t.Fatalf("empty = %s", empty)
	}
}

func TestDuplicateFilterIgnoresPunctuationAndCase(t *testing.T) {
	filter := newDuplicateFilter([]string{"Refund order A100?"})
	if filter.admit("refund ORDER a100") {
		t.Fatal("punctuation variant admitted")
	}
	if !filter.admit("汇总本月销售额") || filter.admit("汇总本月销售额。") {
		t.Fatal("chinese duplicate handling")
	}
	if !filter.admit("汇总上季度各渠道利润") {
		t.Fatal("distinct query rejected")
	}
}

func draftByPolarity(drafts []caseDraftResponse, polarity string) caseDraftResponse {
	for _, draft := range drafts {
		if draft.Polarity == polarity {
			return draft
		}
	}
	return caseDraftResponse{}
}

func toolAgent() db.Agent {
	system := "你是客服"
	return db.Agent{
		Name:   "客服",
		System: &system,
		Tools: json.RawMessage(`[
			{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true},{"name":"bash","enabled":false}]},
			{"type":"mcp_toolset","mcp_server_name":"crm","configs":[{"name":"find_customer","enabled":true},{"name":"delete_customer","enabled":false}]},
			{"type":"custom","name":"lookup_order","description":"查询订单"}
		]`),
		Skills: json.RawMessage(`[{"skill_id":"refund_policy","type":"custom"}]`),
	}
}

func generationRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	return withEvaluationRoute(request, "", "")
}

type promptModel struct {
	mu      sync.Mutex
	prompts []CompletionRequest
	reply   func(CompletionRequest) (string, error)
}

func (m *promptModel) Complete(_ context.Context, request CompletionRequest) (Completion, error) {
	m.mu.Lock()
	m.prompts = append(m.prompts, request)
	m.mu.Unlock()
	text, err := m.reply(request)
	return Completion{Text: text}, err
}

type generationFixture struct {
	evaluationFixture
	agent   db.Agent
	cases   []db.EvaluationCase
	created int
}

func (f *generationFixture) GetAgent(context.Context, string, string) (db.Agent, error) {
	return f.agent, nil
}

func (f *generationFixture) ListEvaluationCases(context.Context, string, string, string) ([]db.EvaluationCase, error) {
	return f.cases, nil
}

func (f *generationFixture) CreateEvaluationCases(context.Context, string, string, string, []db.EvaluationCaseWrite, time.Time) ([]db.EvaluationCase, error) {
	f.created++
	return nil, nil
}
