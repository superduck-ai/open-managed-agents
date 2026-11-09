package evaluations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestCreateTaskRunsCaseAndRecordsScore(t *testing.T) {
	store := newEvaluationMemory()
	starter := &recordingStarter{sessionID: "sesn_case"}
	model := staticModel{text: `{"score":90,"passed":true,"explanation":"覆盖了各月","dimensions":[{"name":"完整性","score":90}]}`}
	handler := NewHandler(store, nil).WithSessionStarter(starter).WithRubricModel(model)
	handler.observeSync = true
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws", WorkspaceExternalID: "ws", UserUUID: "user"}
	response := httptest.NewRecorder()
	body := `{"name":"销售回归","mode":"evaluate","agent_id":"agent_demo","environment_id":"env_demo","dataset_id":"evalds_demo","rubric_id":"evalrb_demo"}`
	if err := handler.createTask(response, evaluationRequest(http.MethodPost, body, principal)); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.Code, response.Body.String())
	}
	if starter.query != "汇总销售额" || starter.version != 4 {
		t.Fatalf("starter = %+v", starter)
	}
	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "sesn_case", EventType: "session.status_idle",
		Payload: json.RawMessage(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	detail, err := store.GetEvaluationTask(context.Background(), "org", "ws", store.task.ExternalID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.Status != "completed" || len(detail.Scores) != 1 || !detail.Scores[0].Passed || detail.Scores[0].Score != 90 {
		t.Fatalf("detail status=%s scores=%+v items=%+v", detail.Task.Status, detail.Scores, detail.Items)
	}
}

type staticModel struct {
	text string
}

func (m staticModel) Complete(context.Context, CompletionRequest) (string, error) {
	return m.text, nil
}

type recordingStarter struct {
	sessionID string
	query     string
	version   int
}

func (s *recordingStarter) Start(_ context.Context, input SessionInput) (string, error) {
	s.query = input.Query
	s.version = input.AgentVersion
	return s.sessionID, nil
}

type evaluationMemory struct {
	task      db.EvaluationTask
	items     []db.EvaluationTaskItem
	scores    []db.EvaluationTaskScore
	agent     db.Agent
	env       db.Environment
	dataset   db.EvaluationDataset
	rubric    db.EvaluationRubric
	cases     []db.EvaluationCase
	events    []db.SessionEvent
	sessionID string
}

func newEvaluationMemory() *evaluationMemory {
	system := "你是销售分析助手"
	return &evaluationMemory{
		agent:   db.Agent{UUID: "agent", ExternalID: "agent_demo", CurrentVersion: 4, System: &system},
		env:     db.Environment{UUID: "env", ExternalID: "env_demo"},
		dataset: db.EvaluationDataset{UUID: "dataset", ExternalID: "evalds_demo", VersionExternalID: "edv_demo", Version: 1},
		rubric:  db.EvaluationRubric{UUID: "rubric", ExternalID: "evalrb_demo", VersionExternalID: "erv_demo", Version: 1, Body: "必须按月汇总", PassScore: 60},
		cases:   []db.EvaluationCase{{UUID: "case", ExternalID: "evalcase_demo", Query: "汇总销售额", Position: 1}},
	}
}

func (m *evaluationMemory) GetAgent(context.Context, string, string) (db.Agent, error) {
	return m.agent, nil
}
func (m *evaluationMemory) GetAgentVersion(context.Context, string, string, int) (db.Agent, error) {
	return m.agent, nil
}
func (m *evaluationMemory) GetEnvironment(context.Context, string, string) (db.Environment, error) {
	return m.env, nil
}
func (m *evaluationMemory) ListEvaluationCases(context.Context, string, string, string) ([]db.EvaluationCase, error) {
	return m.cases, nil
}
func (m *evaluationMemory) GetEvaluationDataset(context.Context, string, string, string) (db.EvaluationDataset, error) {
	return m.dataset, nil
}
func (m *evaluationMemory) GetEvaluationRubric(context.Context, string, string, string) (db.EvaluationRubric, error) {
	return m.rubric, nil
}
func (m *evaluationMemory) CreateEvaluationTask(_ context.Context, task db.EvaluationTask, items []db.EvaluationTaskItem) (db.EvaluationTaskDetail, error) {
	m.task = task
	m.items = append([]db.EvaluationTaskItem(nil), items...)
	for index := range m.items {
		m.items[index].OrganizationUUID = task.OrganizationUUID
		m.items[index].WorkspaceUUID = task.WorkspaceUUID
		m.items[index].TaskUUID = task.UUID
		m.items[index].TaskExternalID = task.ExternalID
		m.items[index].Status = "pending"
	}
	return db.EvaluationTaskDetail{Task: m.task, Items: m.items}, nil
}
func (m *evaluationMemory) GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error) {
	return db.EvaluationTaskDetail{Task: m.task, Items: append([]db.EvaluationTaskItem(nil), m.items...), Scores: append([]db.EvaluationTaskScore(nil), m.scores...)}, nil
}
func (m *evaluationMemory) MarkEvaluationItemRunning(_ context.Context, _, _, itemUUID, sessionID string, _ time.Time) (bool, error) {
	for index := range m.items {
		if m.items[index].UUID == itemUUID && m.items[index].Status == "pending" {
			m.items[index].Status = "running"
			m.items[index].SessionExternalID = sessionID
			return true, nil
		}
	}
	return false, nil
}
func (m *evaluationMemory) FinishRunningEvaluationItem(_ context.Context, _, _, itemUUID, status, message string, _ time.Time) (bool, error) {
	for index := range m.items {
		if m.items[index].UUID == itemUUID && m.items[index].Status == "running" {
			m.items[index].Status = status
			m.items[index].ErrorMessage = message
			return true, nil
		}
	}
	return false, nil
}
func (m *evaluationMemory) UpdateEvaluationTaskStatus(_ context.Context, _, _, _, status, message string, _ time.Time) error {
	m.task.Status = status
	m.task.ErrorMessage = message
	return nil
}
func (m *evaluationMemory) ListRunningEvaluationItemsBySession(_ context.Context, _, _, sessionID string) ([]db.EvaluationTaskItem, error) {
	var items []db.EvaluationTaskItem
	for _, item := range m.items {
		if item.Status == "running" && item.SessionExternalID == sessionID {
			items = append(items, item)
		}
	}
	return items, nil
}
func (m *evaluationMemory) ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	if m.events != nil {
		return m.events, false, nil
	}
	return []db.SessionEvent{{EventType: "agent.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"一月 10，二月 12"}]}`)}}, false, nil
}
func (m *evaluationMemory) CreateEvaluationTaskScore(_ context.Context, score db.EvaluationTaskScore) (db.EvaluationTaskScore, error) {
	m.scores = append(m.scores, score)
	return score, nil
}

func (m *evaluationMemory) CreateEvaluationDataset(context.Context, db.EvaluationDataset, []db.EvaluationCaseWrite) (db.EvaluationDataset, error) {
	return db.EvaluationDataset{}, nil
}
func (m *evaluationMemory) ListEvaluationDatasets(context.Context, string, string, int, int) ([]db.EvaluationDataset, error) {
	return nil, nil
}
func (m *evaluationMemory) DeleteEvaluationDataset(context.Context, string, string, string, time.Time) error {
	return nil
}
func (m *evaluationMemory) CreateEvaluationCases(_ context.Context, _, _, _ string, writes []db.EvaluationCaseWrite, _ time.Time) ([]db.EvaluationCase, error) {
	created := make([]db.EvaluationCase, 0, len(writes))
	for _, write := range writes {
		created = append(created, db.EvaluationCase{
			ExternalID: write.ExternalID, Query: write.Query, ExpectedAnswer: write.ExpectedAnswer,
			SourceSessionID: write.SourceSessionID, Polarity: write.Polarity, Acceptance: write.Acceptance,
		})
	}
	m.cases = append(m.cases, created...)
	return created, nil
}
func (m *evaluationMemory) UpdateEvaluationCase(context.Context, string, string, string, db.EvaluationCaseWrite, time.Time) (db.EvaluationCase, error) {
	return db.EvaluationCase{}, nil
}
func (m *evaluationMemory) DeleteEvaluationCase(context.Context, string, string, string, string, time.Time) error {
	return nil
}
func (m *evaluationMemory) CreateEvaluationRubric(context.Context, db.EvaluationRubric) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{}, nil
}
func (m *evaluationMemory) AppendEvaluationRubricVersion(context.Context, string, string, string, db.EvaluationRubric, time.Time) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{}, nil
}
func (m *evaluationMemory) ListEvaluationRubrics(context.Context, string, string, int, int) ([]db.EvaluationRubric, error) {
	return nil, nil
}
func (m *evaluationMemory) DeleteEvaluationRubric(context.Context, string, string, string, time.Time) error {
	return nil
}
func (m *evaluationMemory) ListEvaluationTasks(context.Context, string, string, int, int) ([]db.EvaluationTask, error) {
	return nil, nil
}
func (m *evaluationMemory) ListEvaluationTaskChildren(context.Context, string, string, []string) ([]db.EvaluationTaskItem, []db.EvaluationTaskScore, error) {
	return m.items, m.scores, nil
}
func (m *evaluationMemory) ResetFailedEvaluationItems(context.Context, string, string, string, int, time.Time) (int64, error) {
	return 0, nil
}
func (m *evaluationMemory) CancelPendingEvaluationItems(context.Context, string, string, string, time.Time) error {
	return nil
}
func (m *evaluationMemory) GetSession(_ context.Context, _, sessionID string) (db.Session, bool, error) {
	if m.sessionID != "" && m.sessionID == sessionID {
		return db.Session{ExternalID: sessionID}, true, nil
	}
	return db.Session{}, false, nil
}

func TestImportSessionKeepsSelectedTurns(t *testing.T) {
	store := newEvaluationMemory()
	store.sessionID = "sesn_demo"
	store.events = []db.SessionEvent{
		{EventType: "user.message", Payload: json.RawMessage(`{"text":"第一问"}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"text":"第一答"}`)},
		{EventType: "user.message", Payload: json.RawMessage(`{"text":"第二问"}`)},
		{EventType: "agent.message", Payload: json.RawMessage(`{"text":"第二答"}`)},
	}
	handler := NewHandler(store, nil)
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws", WorkspaceExternalID: "ws", UserUUID: "user"}
	response := httptest.NewRecorder()
	body := `{"session_id":"sesn_demo","turns":[2],"polarity":"negative","acceptance":"要列出合计"}`
	if err := handler.caseFromSession(response, evaluationRequest(http.MethodPost, body, principal)); err != nil {
		t.Fatal(err)
	}
	created := store.cases[len(store.cases)-1]
	if response.Code != http.StatusOK || len(store.cases) != 2 || created.Query != "第二问" || created.ExpectedAnswer != "" || created.SourceSessionID != "sesn_demo" || created.Acceptance != "要列出合计" {
		t.Fatalf("status=%d cases=%+v body=%s", response.Code, store.cases, response.Body.String())
	}
}

func evaluationRequest(method, body string, principal auth.Principal) *http.Request {
	request := httptest.NewRequest(method, "/api/console/organizations/org/workspaces/ws/evaluations/tasks", strings.NewReader(body))
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", principal.OrganizationUUID)
	route.URLParams.Add("workspaceId", principal.WorkspaceUUID)
	route.URLParams.Add("datasetId", "evalds_demo")
	ctx := context.WithValue(auth.WithPrincipal(request.Context(), principal), chi.RouteCtxKey, route)
	return request.WithContext(ctx)
}
