package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

type settleStore struct {
	*evaluationFixture
	detail   db.EvaluationTaskDetail
	events   map[string][]db.SessionEvent
	missing  map[string]bool
	stale    []db.EvaluationTaskItem
	ready    []db.EvaluationTaskRef
	touched  []string
	status   string
	retryErr error
}

func newSettleStore(items ...db.EvaluationTaskItem) *settleStore {
	task := db.EvaluationTask{
		UUID: "task", ExternalID: "evaltask_1", OrganizationUUID: "org", WorkspaceUUID: "ws", Status: "running",
		PassScore: 60, RubricBody: "回答要准确", RubricModel: "judge-model",
	}
	for index := range items {
		items[index].OrganizationUUID, items[index].WorkspaceUUID = "org", "ws"
		items[index].TaskUUID, items[index].TaskExternalID, items[index].Round = "task", "evaltask_1", 1
	}
	return &settleStore{evaluationFixture: &evaluationFixture{}, detail: db.EvaluationTaskDetail{Task: task, Items: items}, events: map[string][]db.SessionEvent{}}
}

func attachAnswer(store *settleStore) {
	store.events["sesn_answer"] = []db.SessionEvent{{
		EventType: "agent.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"已回答"}]}`),
	}}
	for index := range store.detail.Items {
		if store.detail.Items[index].SessionExternalID == "" {
			store.detail.Items[index].SessionExternalID = "sesn_answer"
		}
	}
}

func completedItems(count int) []db.EvaluationTaskItem {
	items := make([]db.EvaluationTaskItem, 0, count)
	for index := range count {
		items = append(items, db.EvaluationTaskItem{
			UUID: fmt.Sprintf("item-%d", index), ExternalID: fmt.Sprintf("evalitem_%d", index), Status: "completed", Query: "问题",
		})
	}
	return items
}

func (s *settleStore) GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error) {
	detail := s.detail
	detail.Items = append([]db.EvaluationTaskItem(nil), s.detail.Items...)
	detail.Scores = append([]db.EvaluationTaskScore(nil), s.detail.Scores...)
	return detail, nil
}

func (s *settleStore) CreateEvaluationTaskScore(_ context.Context, score db.EvaluationTaskScore) (db.EvaluationTaskScore, error) {
	for _, existing := range s.detail.Scores {
		if existing.ItemUUID == score.ItemUUID {
			return db.EvaluationTaskScore{}, db.ErrDuplicate
		}
	}
	s.detail.Scores = append(s.detail.Scores, score)
	return score, nil
}

func (s *settleStore) UpdateEvaluationTaskStatus(_ context.Context, _, _, _, status, _ string, _ time.Time) error {
	s.status = status
	s.detail.Task.Status = status
	return nil
}

func (s *settleStore) FinishRunningEvaluationItem(_ context.Context, _, _, itemUUID, status, message string, _ time.Time) (bool, error) {
	for index, item := range s.detail.Items {
		if item.UUID == itemUUID && item.Status == "running" {
			s.detail.Items[index].Status, s.detail.Items[index].ErrorMessage = status, message
			return true, nil
		}
	}
	return false, nil
}

func (s *settleStore) GetSession(_ context.Context, _, sessionID string) (db.Session, bool, error) {
	return db.Session{}, !s.missing[sessionID], nil
}

func (s *settleStore) ListSessionEventsPage(_ context.Context, params db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	return s.events[params.SessionExternalID], false, nil
}

func (s *settleStore) TouchRunningEvaluationItem(_ context.Context, _, _, itemUUID string, _ time.Time) error {
	s.touched = append(s.touched, itemUUID)
	return nil
}

func (s *settleStore) ListStaleRunningEvaluationItems(context.Context, time.Time, int) ([]db.EvaluationTaskItem, error) {
	return s.stale, nil
}

func (s *settleStore) ListSettleableEvaluationTasks(context.Context, time.Time, int) ([]db.EvaluationTaskRef, error) {
	return s.ready, nil
}

func (s *settleStore) RetryEvaluationTask(context.Context, string, string, string, int, time.Time) (db.EvaluationTaskRetry, error) {
	if s.retryErr != nil {
		return db.EvaluationTaskRetry{}, s.retryErr
	}
	kept := s.detail.Scores[:0]
	var cleared int64
	for _, score := range s.detail.Scores {
		if score.Status == "failed" {
			cleared++
			continue
		}
		kept = append(kept, score)
	}
	s.detail.Scores = kept
	s.detail.Task.Status = "running"
	return db.EvaluationTaskRetry{ClearedScores: cleared}, nil
}

type modelReply struct {
	text string
	err  error
}

type sequenceModel struct {
	replies []modelReply
	calls   int
	request CompletionRequest
}

func (m *sequenceModel) Complete(_ context.Context, request CompletionRequest) (string, error) {
	m.request = request
	reply := m.replies[min(m.calls, len(m.replies)-1)]
	m.calls++
	return reply.text, reply.err
}

type recordingQueue struct{ tasks []string }

func (q *recordingQueue) EnqueueSettle(_ context.Context, _, _, taskID string) error {
	q.tasks = append(q.tasks, taskID)
	return nil
}

func TestSettleReturnsRetryableModelFailureBeforeFinalAttempt(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	handler := NewHandler(store, nil).WithRubricModel(&sequenceModel{replies: []modelReply{{err: errModelFailed}}})

	done, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err == nil || done || len(store.detail.Scores) != 0 || store.status != "" {
		t.Fatalf("done=%v err=%v scores=%d status=%q", done, err, len(store.detail.Scores), store.status)
	}
}

func TestSettleReturnsParseFailureBeforeFinalAttempt(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	handler := NewHandler(store, nil).WithRubricModel(&sequenceModel{replies: []modelReply{{text: "看起来不错"}}})

	if _, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", false); !errors.Is(err, errScoreParse) {
		t.Fatalf("err = %v", err)
	}
	if len(store.detail.Scores) != 0 {
		t.Fatalf("scores = %+v", store.detail.Scores)
	}
}

func TestSettleRecordsFailuresOnFinalAttempt(t *testing.T) {
	store := newSettleStore(completedItems(2)...)
	handler := NewHandler(store, nil).WithRubricModel(&sequenceModel{replies: []modelReply{{err: errModelFailed}, {text: "看起来不错"}}})

	done, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || store.status != "completed" || len(store.detail.Scores) != 2 {
		t.Fatalf("done=%v err=%v status=%q scores=%d", done, err, store.status, len(store.detail.Scores))
	}
	if store.detail.Scores[0].ErrorMessage != scoreFailedMessage || store.detail.Scores[1].ErrorMessage != scoreParseFailedMessage {
		t.Fatalf("scores = %+v", store.detail.Scores)
	}
}

func TestSettleRecordsMissingModelWithoutRetry(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	handler := NewHandler(store, nil)

	done, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err != nil || !done || len(store.detail.Scores) != 1 || store.detail.Scores[0].ErrorMessage != modelMissingMessage {
		t.Fatalf("done=%v err=%v scores=%+v", done, err, store.detail.Scores)
	}
}

func TestSettleWaitsForRunningItems(t *testing.T) {
	items := completedItems(2)
	items[1].Status = "running"
	store := newSettleStore(items...)
	model := &sequenceModel{replies: []modelReply{{text: `{"score":90}`}}}
	handler := NewHandler(store, nil).WithRubricModel(model)

	done, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || model.calls != 0 || store.status != "" {
		t.Fatalf("done=%v err=%v calls=%d status=%q", done, err, model.calls, store.status)
	}
}

func TestSettleScoresInBatchesAndRecordsJudge(t *testing.T) {
	store := newSettleStore(completedItems(settleBatchSize + 2)...)
	attachAnswer(store)
	model := &sequenceModel{replies: []modelReply{{text: `{"score":82,"passed":false,"explanation":"准确"}`}}}
	handler := NewHandler(store, nil).WithRubricModel(model)

	done, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err != nil || done || len(store.detail.Scores) != settleBatchSize || store.status != "" {
		t.Fatalf("first done=%v err=%v scores=%d status=%q", done, err, len(store.detail.Scores), store.status)
	}
	done, err = handler.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err != nil || !done || len(store.detail.Scores) != settleBatchSize+2 || store.status != "completed" {
		t.Fatalf("second done=%v err=%v scores=%d status=%q", done, err, len(store.detail.Scores), store.status)
	}
	score := store.detail.Scores[0]
	if score.Status != "completed" || !score.Passed || score.JudgeModel != "judge-model" || score.JudgePromptVersion != scoringPromptVersion {
		t.Fatalf("score = %+v", score)
	}
	if model.request.Model != "judge-model" || !strings.HasPrefix(model.request.System, scoringSystemPrompt) || strings.Contains(model.request.System, "Markdown scoring rules") {
		t.Fatalf("request = %+v", model.request)
	}
	if !strings.Contains(model.request.System, "回答要准确") || strings.Contains(model.request.User, "回答要准确") {
		t.Fatalf("rubric placement system=%q user=%q", model.request.System, model.request.User)
	}
}

func TestObserverEnqueuesSettleWhenQueueConfigured(t *testing.T) {
	items := completedItems(1)
	items[0].Status, items[0].SessionExternalID = "running", "sesn_1"
	store := &observerStore{settleStore: newSettleStore(items...)}
	queue := &recordingQueue{}
	handler := NewHandler(store, nil).WithSettleQueue(queue)
	handler.observeSync = true

	handler.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "sesn_1", EventType: "session.status_idle",
		Payload: json.RawMessage(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if len(queue.tasks) != 1 || queue.tasks[0] != "evaltask_1" || store.detail.Items[0].Status != "completed" || len(store.detail.Scores) != 0 {
		t.Fatalf("queue=%v item=%+v scores=%d", queue.tasks, store.detail.Items[0], len(store.detail.Scores))
	}
}

type observerStore struct{ *settleStore }

func (s *observerStore) ListRunningEvaluationItemsBySession(_ context.Context, _, _, sessionID string) ([]db.EvaluationTaskItem, error) {
	var running []db.EvaluationTaskItem
	for _, item := range s.detail.Items {
		if item.SessionExternalID == sessionID && item.Status == "running" {
			running = append(running, item)
		}
	}
	return running, nil
}

func TestSweepReconcilesStaleItemsAndSettlesReadyTasks(t *testing.T) {
	items := completedItems(3)
	for index, session := range []string{"sesn_done", "sesn_busy", "sesn_gone"} {
		items[index].Status, items[index].SessionExternalID = "running", session
	}
	store := newSettleStore(items...)
	store.stale = append([]db.EvaluationTaskItem(nil), store.detail.Items...)
	store.ready = []db.EvaluationTaskRef{{OrganizationUUID: "org", WorkspaceUUID: "ws", ExternalID: "evaltask_ready"}}
	store.missing = map[string]bool{"sesn_gone": true}
	store.events["sesn_done"] = []db.SessionEvent{{EventType: "session.status_idle", Payload: json.RawMessage(`{"stop_reason":{"type":"end_turn"}}`)}}
	store.events["sesn_busy"] = []db.SessionEvent{{EventType: "agent.message", Payload: json.RawMessage(`{"content":"处理中"}`)}}
	queue := &recordingQueue{}
	handler := NewHandler(store, nil).WithSettleQueue(queue)

	if err := handler.sweep(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	statuses := []string{store.detail.Items[0].Status, store.detail.Items[1].Status, store.detail.Items[2].Status}
	if strings.Join(statuses, ",") != "completed,running,failed" || store.detail.Items[2].ErrorMessage != sessionTerminatedMessage {
		t.Fatalf("items = %+v", store.detail.Items)
	}
	if len(store.touched) != 1 || store.touched[0] != "item-1" {
		t.Fatalf("touched = %v", store.touched)
	}
	if strings.Join(queue.tasks, ",") != "evaltask_1,evaltask_1,evaltask_ready" {
		t.Fatalf("queue = %v", queue.tasks)
	}
}

func TestRetryRejectsTaskWithoutRetryableWork(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	store.detail.Task.Status = "completed"
	store.retryErr = db.ErrPreconditionFailed
	handler := NewHandler(store, nil)

	err := handler.retryTask(httptest.NewRecorder(), withTaskRetryRoute(httptest.NewRequest(http.MethodPost, "/", nil)))
	if publicMessage(err) != retryUnavailableMessage {
		t.Fatalf("err = %v", err)
	}
}

func TestRetryRescoresFailedScores(t *testing.T) {
	store := newSettleStore(completedItems(2)...)
	store.detail.Task.Status = "completed"
	store.detail.Scores = []db.EvaluationTaskScore{
		{ItemUUID: "item-0", Status: "completed", Score: 70, Passed: true},
		{ItemUUID: "item-1", Status: "failed", ErrorMessage: scoreFailedMessage},
	}
	model := &sequenceModel{replies: []modelReply{{text: `{"score":88}`}}}
	attachAnswer(store)
	handler := NewHandler(store, nil).WithRubricModel(model)
	response := httptest.NewRecorder()

	if err := handler.retryTask(response, withTaskRetryRoute(httptest.NewRequest(http.MethodPost, "/", nil))); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || store.status != "completed" || len(store.detail.Scores) != 2 {
		t.Fatalf("calls=%d status=%q scores=%+v", model.calls, store.status, store.detail.Scores)
	}
	rescored := store.detail.Scores[1]
	if rescored.ItemUUID != "item-1" || rescored.Status != "completed" || rescored.Score != 88 {
		t.Fatalf("rescored = %+v", rescored)
	}
}

func TestJudgeModelRejectsWorkspaceWithoutModel(t *testing.T) {
	handler := NewHandler(&evaluationFixture{}, nil).WithRubricModel(resolverModel{})
	if _, err := handler.judgeModel(context.Background(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}, ""); publicMessage(err) != modelMissingMessage {
		t.Fatalf("err = %v", err)
	}
}

func TestSweepFinishesItemWaitingForUser(t *testing.T) {
	items := completedItems(1)
	items[0].Status, items[0].SessionExternalID = "running", "sesn_ask"
	store := newSettleStore(items...)
	store.stale = append([]db.EvaluationTaskItem(nil), store.detail.Items...)
	store.events["sesn_ask"] = []db.SessionEvent{{
		EventType: "session.status_idle", Payload: json.RawMessage(`{"stop_reason":{"type":"requires_action","event_ids":["tool"]}}`),
	}}
	queue := &recordingQueue{}
	handler := NewHandler(store, nil).WithSettleQueue(queue)

	if err := handler.sweep(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if store.detail.Items[0].Status != "completed" || store.detail.Items[0].ErrorMessage != "" || len(store.touched) != 0 {
		t.Fatalf("item = %+v touched = %v", store.detail.Items[0], store.touched)
	}
	if len(queue.tasks) != 1 || queue.tasks[0] != "evaltask_1" {
		t.Fatalf("queue = %v", queue.tasks)
	}
}

func TestUnansweredRunCannotPass(t *testing.T) {
	items := completedItems(1)
	items[0].SessionExternalID = "sesn_ask"
	store := newSettleStore(items...)
	store.events["sesn_ask"] = []db.SessionEvent{{
		EventType: "agent.custom_tool_use",
		Payload:   json.RawMessage(`{"name":"AskUserQuestion","input":{"question":"数据从哪来"}}`),
	}}
	model := &sequenceModel{replies: []modelReply{{text: `{"score":90,"explanation":"没有汇总"}`}}}
	handler := NewHandler(store, nil).WithRubricModel(model)

	done, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || len(store.detail.Scores) != 1 {
		t.Fatalf("done=%v err=%v scores=%d", done, err, len(store.detail.Scores))
	}
	score := store.detail.Scores[0]
	if score.Status != "completed" || score.Passed || score.Score != 59 || score.Answer != "" {
		t.Fatalf("score = %+v", score)
	}
	if !strings.Contains(score.JudgeSystem, "回答要准确") || !strings.Contains(score.JudgePrompt, "AskUserQuestion") || !strings.Contains(score.JudgeResponse, "没有汇总") {
		t.Fatalf("exchange system=%q prompt=%q response=%q", score.JudgeSystem, score.JudgePrompt, score.JudgeResponse)
	}
	if !strings.Contains(model.request.User, "AskUserQuestion") || !strings.Contains(model.request.User, "数据从哪来") {
		t.Fatalf("trajectory missing the pending question: %s", model.request.User)
	}
}

func TestAnsweredRunKeepsJudgeScore(t *testing.T) {
	items := completedItems(1)
	items[0].SessionExternalID = "sesn_done"
	store := newSettleStore(items...)
	store.events["sesn_done"] = []db.SessionEvent{{
		EventType: "agent.message", Payload: json.RawMessage(`{"content":[{"type":"text","text":"本月销售额 12 万"}]}`),
	}}
	model := &sequenceModel{replies: []modelReply{{text: `{"score":90,"explanation":"金额正确"}`}}}
	handler := NewHandler(store, nil).WithRubricModel(model)

	if _, err := handler.settleTask(context.Background(), "org", "ws", "evaltask_1", true); err != nil {
		t.Fatal(err)
	}
	score := store.detail.Scores[0]
	if !score.Passed || score.Score != 90 || score.Answer != "本月销售额 12 万" {
		t.Fatalf("score = %+v", score)
	}
}

func TestJudgeModelFreezesWorkspaceDefault(t *testing.T) {
	handler := NewHandler(&evaluationFixture{}, nil).WithRubricModel(resolverModel{model: "glm-judge"})
	principal := auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}
	model, err := handler.judgeModel(context.Background(), principal, "")
	if err != nil || model != "glm-judge" {
		t.Fatalf("model=%q err=%v", model, err)
	}
	model, err = handler.judgeModel(context.Background(), principal, "rubric-model")
	if err != nil || model != "rubric-model" {
		t.Fatalf("configured model=%q err=%v", model, err)
	}
}

func TestStartItemsSendsCaseInputs(t *testing.T) {
	inputs := json.RawMessage(`[{"type":"text","text":"描述这张图里的金额"},{"type":"image","source":{"type":"file","file_id":"file_demo"}}]`)
	store := newSettleStore(
		db.EvaluationTaskItem{UUID: "item-plain", ExternalID: "evalitem_plain", Status: "pending", Query: "汇总销售额"},
		db.EvaluationTaskItem{UUID: "item-image", ExternalID: "evalitem_image", Status: "pending", Query: "描述这张图里的金额", Inputs: inputs},
	)
	starter := &recordingStarter{}
	handler := NewHandler(store, nil).WithSessionStarter(starter)
	handler.startItems(context.Background(), auth.Principal{}, store.detail.Task, store.detail.Items)
	if len(starter.inputs) != 2 {
		t.Fatalf("inputs = %+v", starter.inputs)
	}
	if len(starter.inputs[0].Content) != 0 || starter.inputs[0].Query != "汇总销售额" {
		t.Fatalf("plain input = %+v", starter.inputs[0])
	}
	if string(starter.inputs[1].Content) != string(inputs) {
		t.Fatalf("image content = %s", starter.inputs[1].Content)
	}
}

type recordingStarter struct{ inputs []SessionInput }

func (s *recordingStarter) Start(_ context.Context, input SessionInput) (string, error) {
	s.inputs = append(s.inputs, input)
	return fmt.Sprintf("sesn_%d", len(s.inputs)), nil
}

type resolverModel struct{ model string }

func (resolverModel) Complete(context.Context, CompletionRequest) (string, error) {
	return "", nil
}

func (m resolverModel) DefaultModel(context.Context, string, string) (string, bool, error) {
	return m.model, m.model != "", nil
}

func withTaskRetryRoute(request *http.Request) *http.Request {
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", "org")
	route.URLParams.Add("workspaceId", "ws")
	route.URLParams.Add("taskId", "evaltask_1")
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, route)
	return request.WithContext(auth.WithPrincipal(ctx, auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}))
}
