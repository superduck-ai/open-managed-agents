package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

type taskDetailFixture struct {
	*evaluationFixture
	detail     db.EvaluationTaskDetail
	events     []db.SessionEvent
	datasetErr error
	usage      db.EvaluationTaskUsage
}

func (f *taskDetailFixture) GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error) {
	return f.detail, nil
}

func (f *taskDetailFixture) GetEvaluationScorePrompt(_ context.Context, _, _, itemUUID string) (string, string, string, error) {
	for _, score := range f.detail.Scores {
		if score.ItemUUID == itemUUID {
			return score.JudgeSystem, score.JudgePrompt, score.JudgeResponse, nil
		}
	}
	return "", "", "", nil
}

func (f *taskDetailFixture) ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	return f.events, false, nil
}

func (f *taskDetailFixture) GetAgentVersion(context.Context, string, string, int) (db.Agent, error) {
	return db.Agent{Name: "客服 Agent"}, nil
}

func (f *taskDetailFixture) GetEvaluationDataset(context.Context, string, string, string) (db.EvaluationDataset, error) {
	if f.datasetErr != nil {
		return db.EvaluationDataset{}, f.datasetErr
	}
	return db.EvaluationDataset{Name: "回归集"}, nil
}

func (f *taskDetailFixture) GetEvaluationRubric(context.Context, string, string, string) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{Name: "准确性"}, nil
}

func (f *taskDetailFixture) SumEvaluationTaskUsage(context.Context, string, string, string) (db.EvaluationTaskUsage, error) {
	return f.usage, nil
}

func newTaskDetailFixture() *taskDetailFixture {
	return &taskDetailFixture{
		evaluationFixture: &evaluationFixture{},
		detail: db.EvaluationTaskDetail{
			Task: db.EvaluationTask{
				UUID: "task-uuid", ExternalID: "evaltask_1", Status: "completed", AgentExternalID: "agent_1", AgentVersion: 2,
				Resources: json.RawMessage(`[{"type":"file"}]`), VaultIDs: json.RawMessage(`["vlt_1","vlt_2"]`),
			},
			Items: []db.EvaluationTaskItem{{
				UUID: "item-uuid", ExternalID: "evalitem_1", Round: 1, SessionExternalID: "sesn_1", Status: "completed",
				Query: "退款多久到账", Acceptance: "说明到账时效",
			}},
			Scores: []db.EvaluationTaskScore{{
				ItemUUID: "item-uuid", Status: "completed", Score: 80, Passed: true, Answer: "3 个工作日",
				JudgeSystem: "系统提示", JudgePrompt: "评分提示", JudgeResponse: "模型原文",
			}},
		},
	}
}

func withTaskRoute(request *http.Request, taskID, itemID string) *http.Request {
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", "org")
	route.URLParams.Add("workspaceId", "ws")
	route.URLParams.Add("taskId", taskID)
	route.URLParams.Add("itemId", itemID)
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, route)
	return request.WithContext(auth.WithPrincipal(ctx, auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}))
}

func TestTaskItemTraceRejectsUnknownItem(t *testing.T) {
	handler := NewHandler(newTaskDetailFixture(), nil)
	request := withTaskRoute(httptest.NewRequest(http.MethodGet, "/", nil), "evaltask_1", "evalitem_missing")

	err := handler.getTaskItemTrace(httptest.NewRecorder(), request)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.NotFound || appErr.PublicMessage != itemNotFoundMessage {
		t.Fatalf("error = %#v", err)
	}
}

func TestTaskDetailReportsUnexpectedLookupFailure(t *testing.T) {
	store := newTaskDetailFixture()
	store.datasetErr = errors.New("db down")
	handler := NewHandler(store, nil)

	_, err := handler.taskDetailJSON(context.Background(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}, store.detail)
	var appErr *apperr.Error
	if !errors.As(err, &appErr) || appErr.Kind != apperr.Internal {
		t.Fatalf("error = %#v", err)
	}
}

func TestTaskItemTraceListsConversationSteps(t *testing.T) {
	store := newTaskDetailFixture()
	at := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
	store.events = []db.SessionEvent{
		{EventType: "user.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"退款多久到账"}]}`), CreatedAt: at},
		{EventType: "span.model_request_end", Payload: json.RawMessage(`{}`), CreatedAt: at},
		{EventType: "agent.tool_use", Payload: json.RawMessage(`{"name":"search","input":{"q":"退款"}}`), CreatedAt: at},
		{EventType: "agent.tool_result", Payload: json.RawMessage(`{"content":[{"type":"text","text":"T+3"}]}`), CreatedAt: at},
		{EventType: "agent.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"3 个工作日"}]}`), CreatedAt: at},
	}
	handler := NewHandler(store, nil)
	request := withTaskRoute(httptest.NewRequest(http.MethodGet, "/", nil), "evaltask_1", "evalitem_1")
	response := httptest.NewRecorder()

	if err := handler.getTaskItemTrace(response, request); err != nil {
		t.Fatal(err)
	}
	var body traceResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.SessionID != "sesn_1" || len(body.Steps) != 4 {
		t.Fatalf("body = %+v", body)
	}
	tool := body.Steps[1]
	if tool.Type != "tool_use" || tool.Name != "search" || tool.Text != `{"q":"退款"}` {
		t.Fatalf("tool step = %+v", tool)
	}
	if body.Steps[0].Type != "user" || body.Steps[2].Text != "T+3" || body.Steps[3].Type != "answer" {
		t.Fatalf("steps = %+v", body.Steps)
	}
	if body.JudgeSystem != "系统提示" || body.JudgePrompt != "评分提示" || body.JudgeResponse != "模型原文" {
		t.Fatalf("exchange = %+v", body)
	}
}

func TestTaskDetailIncludesNamesUsageAndAnswer(t *testing.T) {
	store := newTaskDetailFixture()
	store.usage = db.EvaluationTaskUsage{InputTokens: 120, OutputTokens: 30, CacheReadInputTokens: 8}
	handler := NewHandler(store, nil)

	response, err := handler.taskDetailJSON(context.Background(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}, store.detail)
	if err != nil {
		t.Fatal(err)
	}
	if response.AgentName != "客服 Agent" || response.DatasetName != "回归集" || response.RubricName != "准确性" {
		t.Fatalf("names = %q %q %q", response.AgentName, response.DatasetName, response.RubricName)
	}
	if response.ResourceCount != 1 || response.VaultCount != 2 || response.Usage == nil || response.Usage.InputTokens != 120 {
		t.Fatalf("response = %+v usage=%+v", response, response.Usage)
	}
	item := response.Items[0]
	if item.Acceptance != "说明到账时效" || item.Score == nil || item.Score.Answer != "3 个工作日" {
		t.Fatalf("item = %+v", item)
	}
	encoded, err := json.Marshal(item.Score)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "judge_system") {
		t.Fatalf("detail score includes the scoring prompt: %s", encoded)
	}
}

func TestTaskDetailKeepsDeletedDatasetBlank(t *testing.T) {
	store := newTaskDetailFixture()
	store.datasetErr = db.ErrNotFound
	handler := NewHandler(store, nil)

	response, err := handler.taskDetailJSON(context.Background(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}, store.detail)
	if err != nil {
		t.Fatal(err)
	}
	if response.DatasetName != "" || response.RubricName != "准确性" {
		t.Fatalf("names = %q %q", response.DatasetName, response.RubricName)
	}
}
