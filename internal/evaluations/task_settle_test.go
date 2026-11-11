package evaluations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"

	"github.com/go-chi/chi/v5"
)

type settleStore struct {
	*evaluationFixture
	mu           sync.Mutex
	detail       db.EvaluationTaskDetail
	events       map[string][]db.SessionEvent
	sessions     map[string]db.Session
	stale        []db.EvaluationTaskItem
	unstarted    []db.EvaluationTaskItem
	ready        []db.EvaluationTaskRef
	touched      []string
	status       string
	retryErr     error
	attachErr    error
	beforeFinish func()
}

func newSettleStore(items ...db.EvaluationTaskItem) *settleStore {
	task := db.EvaluationTask{
		UUID: "task", ExternalID: "evaltask_1", OrganizationUUID: "org", WorkspaceUUID: "ws", Status: taskRunning,
		Name: "回归", PassScore: 60, RubricBody: "回答要准确", RubricModel: "judge-model",
	}
	for index := range items {
		items[index].OrganizationUUID, items[index].WorkspaceUUID = "org", "ws"
		items[index].TaskUUID, items[index].TaskExternalID, items[index].Round = "task", "evaltask_1", 1
	}
	return &settleStore{
		evaluationFixture: &evaluationFixture{}, detail: db.EvaluationTaskDetail{Task: task, Items: items},
		events: map[string][]db.SessionEvent{}, sessions: map[string]db.Session{},
	}
}

func newTestRunner(store *settleStore) *TaskRunner {
	runner := NewTaskRunner(store, nil)
	runner.spawn = func(run func()) { run() }
	return runner
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
			UUID: fmt.Sprintf("item-%d", index), ExternalID: fmt.Sprintf("evalitem_%d", index), Status: itemCompleted, Query: "问题",
		})
	}
	return items
}

func runningItems(sessions ...string) []db.EvaluationTaskItem {
	items := completedItems(len(sessions))
	for index, session := range sessions {
		items[index].Status, items[index].SessionExternalID = itemRunning, session
	}
	return items
}

func idleEvent(reason string) db.SessionEvent {
	return db.SessionEvent{EventType: "session.status_idle", Payload: json.RawMessage(`{"stop_reason":{"type":"` + reason + `"}}`)}
}

func (s *settleStore) item(itemUUID string) *db.EvaluationTaskItem {
	for index := range s.detail.Items {
		if s.detail.Items[index].UUID == itemUUID {
			return &s.detail.Items[index]
		}
	}
	return nil
}

func (s *settleStore) GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	detail := s.detail
	detail.Items = append([]db.EvaluationTaskItem(nil), s.detail.Items...)
	detail.Scores = append([]db.EvaluationTaskScore(nil), s.detail.Scores...)
	return detail, nil
}

func (s *settleStore) ListRunningEvaluationItemsBySession(_ context.Context, _, _, sessionID string) ([]db.EvaluationTaskItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var running []db.EvaluationTaskItem
	for _, item := range s.detail.Items {
		if item.SessionExternalID == sessionID && item.Status == itemRunning {
			running = append(running, item)
		}
	}
	return running, nil
}

func (s *settleStore) ClaimEvaluationItem(_ context.Context, _, _, itemUUID string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.item(itemUUID)
	if item == nil || item.Status != itemPending {
		return false, nil
	}
	item.Status, item.SessionExternalID, item.ErrorMessage = itemRunning, "", ""
	return true, nil
}

func (s *settleStore) AttachEvaluationItemSession(_ context.Context, _, _, itemUUID, sessionID string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attachErr != nil {
		return false, s.attachErr
	}
	item := s.item(itemUUID)
	if item == nil || item.Status != itemRunning || item.SessionExternalID != "" {
		return false, nil
	}
	item.SessionExternalID = sessionID
	return true, nil
}

func (s *settleStore) AbandonEvaluationItem(_ context.Context, _, _, itemUUID, message string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.item(itemUUID)
	if item == nil || !itemOpen(item.Status) || item.SessionExternalID != "" {
		return false, nil
	}
	item.Status, item.ErrorMessage = itemFailed, message
	return true, nil
}

func (s *settleStore) FinishRunningEvaluationItem(_ context.Context, _, _, itemUUID, status, message string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.item(itemUUID)
	if item == nil || item.Status != itemRunning {
		return false, nil
	}
	item.Status, item.ErrorMessage = status, message
	return true, nil
}

func (s *settleStore) TouchRunningEvaluationItem(_ context.Context, _, _, itemUUID string, _ time.Time) error {
	s.touched = append(s.touched, itemUUID)
	return nil
}

func (s *settleStore) ListStaleRunningEvaluationItems(context.Context, time.Time, int) ([]db.EvaluationTaskItem, error) {
	return s.stale, nil
}

func (s *settleStore) ListUnstartedEvaluationItems(context.Context, time.Time, int) ([]db.EvaluationTaskItem, error) {
	return s.unstarted, nil
}

func (s *settleStore) ListSettleableEvaluationTasks(context.Context, time.Time, int) ([]db.EvaluationTaskRef, error) {
	return s.ready, nil
}

func (s *settleStore) CancelEvaluationTask(context.Context, string, string, string, time.Time) ([]db.EvaluationTaskItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if taskClosed(s.detail.Task.Status) {
		return nil, db.ErrInvalidState
	}
	s.detail.Task.Status, s.status = taskCancelled, taskCancelled
	var cancelled []db.EvaluationTaskItem
	for index := range s.detail.Items {
		if itemOpen(s.detail.Items[index].Status) {
			s.detail.Items[index].Status = itemCancelled
			cancelled = append(cancelled, s.detail.Items[index])
		}
	}
	return cancelled, nil
}

func (s *settleStore) FinishEvaluationTask(_ context.Context, _, _, _, status string, _ time.Time) (bool, error) {
	if hook := s.beforeFinish; hook != nil {
		s.beforeFinish = nil
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	open := slices.ContainsFunc(s.detail.Items, func(item db.EvaluationTaskItem) bool { return itemOpen(item.Status) })
	if s.detail.Task.Status != taskRunning || open || len(unscoredItems(s.detail, latestRound(s.detail.Items))) > 0 {
		return false, nil
	}
	s.status, s.detail.Task.Status = status, status
	return true, nil
}

func (s *settleStore) CreateEvaluationTaskScore(_ context.Context, score db.EvaluationTaskScore) (db.EvaluationTaskScore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.detail.Scores {
		if existing.ItemUUID == score.ItemUUID {
			return db.EvaluationTaskScore{}, db.ErrDuplicate
		}
	}
	s.detail.Scores = append(s.detail.Scores, score)
	return score, nil
}

func (s *settleStore) GetSession(_ context.Context, _, sessionID string) (db.Session, bool, error) {
	session, ok := s.sessions[sessionID]
	return session, ok, nil
}

func (s *settleStore) ListSessionEventsPage(_ context.Context, params db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error) {
	var events []db.SessionEvent
	for _, event := range s.events[params.SessionExternalID] {
		if len(params.Types) == 0 || slices.Contains(params.Types, event.EventType) {
			events = append(events, event)
		}
	}
	if params.Order == "desc" {
		slices.Reverse(events)
	}
	if params.Limit > 0 && len(events) > params.Limit {
		events = events[:params.Limit]
	}
	return events, false, nil
}

func (s *settleStore) RetryEvaluationTask(context.Context, string, string, string, int, time.Time) (db.EvaluationTaskRetry, error) {
	if s.retryErr != nil {
		return db.EvaluationTaskRetry{}, s.retryErr
	}
	kept := s.detail.Scores[:0]
	var cleared int64
	for _, score := range s.detail.Scores {
		if score.Status == scoreFailed {
			cleared++
			continue
		}
		kept = append(kept, score)
	}
	s.detail.Scores = kept
	s.detail.Task.Status = taskRunning
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

func (m *sequenceModel) Complete(_ context.Context, request CompletionRequest) (Completion, error) {
	m.request = request
	reply := m.replies[min(m.calls, len(m.replies)-1)]
	m.calls++
	return Completion{Text: reply.text, InputTokens: 120, OutputTokens: 30}, reply.err
}

type recordingQueue struct{ tasks []string }

func (q *recordingQueue) EnqueueSettle(_ context.Context, _, _, taskID string) error {
	q.tasks = append(q.tasks, taskID)
	return nil
}

type recordingSessions struct {
	mu       sync.Mutex
	inputs   []SessionInput
	archived []string
	err      error
}

func (s *recordingSessions) Start(_ context.Context, input SessionInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	s.inputs = append(s.inputs, input)
	return "sesn_" + input.Metadata["evaluation_item_id"], nil
}

func (s *recordingSessions) Archive(_ context.Context, _ auth.Principal, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.archived = append(s.archived, sessionID)
	return nil
}

func TestSettleReturnsRetryableModelFailureBeforeFinalAttempt(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	runner := newTestRunner(store).WithRubricModel(&sequenceModel{replies: []modelReply{{err: errModelFailed}}})

	done, err := runner.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err == nil || done || len(store.detail.Scores) != 0 || store.status != "" {
		t.Fatalf("done=%v err=%v scores=%d status=%q", done, err, len(store.detail.Scores), store.status)
	}
}

func TestSettleReturnsParseFailureBeforeFinalAttempt(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	runner := newTestRunner(store).WithRubricModel(&sequenceModel{replies: []modelReply{{text: "看起来不错"}}})

	if _, err := runner.settleTask(context.Background(), "org", "ws", "evaltask_1", false); !errors.Is(err, errScoreParse) {
		t.Fatalf("err = %v", err)
	}
	if len(store.detail.Scores) != 0 {
		t.Fatalf("scores = %+v", store.detail.Scores)
	}
}

func TestSettleRecordsFailuresOnFinalAttempt(t *testing.T) {
	store := newSettleStore(completedItems(2)...)
	runner := newTestRunner(store).WithRubricModel(&sequenceModel{replies: []modelReply{{err: errModelFailed}, {text: "看起来不错"}}})

	done, err := runner.settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || store.status != taskCompleted || len(store.detail.Scores) != 2 {
		t.Fatalf("done=%v err=%v status=%q scores=%d", done, err, store.status, len(store.detail.Scores))
	}
	if store.detail.Scores[0].ErrorMessage != scoreFailedMessage || store.detail.Scores[1].ErrorMessage != scoreParseFailedMessage {
		t.Fatalf("scores = %+v", store.detail.Scores)
	}
}

func TestSettleRecordsMissingModelWithoutRetry(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	runner := newTestRunner(store)

	done, err := runner.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err != nil || !done || len(store.detail.Scores) != 1 || store.detail.Scores[0].ErrorMessage != modelMissingMessage {
		t.Fatalf("done=%v err=%v scores=%+v", done, err, store.detail.Scores)
	}
}

func TestSettleSkipsClosedTask(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	store.detail.Task.Status = taskCancelled
	model := &sequenceModel{replies: []modelReply{{text: `{"score":90}`}}}

	done, err := newTestRunner(store).WithRubricModel(model).settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || model.calls != 0 || len(store.detail.Scores) != 0 {
		t.Fatalf("done=%v err=%v calls=%d", done, err, model.calls)
	}
}

func TestSettleWaitsForRunningItems(t *testing.T) {
	items := completedItems(2)
	items[1].Status = itemRunning
	store := newSettleStore(items...)
	model := &sequenceModel{replies: []modelReply{{text: `{"score":90}`}}}

	done, err := newTestRunner(store).WithRubricModel(model).settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || model.calls != 0 || store.status != "" {
		t.Fatalf("done=%v err=%v calls=%d status=%q", done, err, model.calls, store.status)
	}
}

func TestSettleFailsRoundWhenEveryItemFailed(t *testing.T) {
	items := completedItems(2)
	items[0].Status, items[1].Status = itemFailed, itemCancelled
	store := newSettleStore(items...)

	done, err := newTestRunner(store).settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || store.status != taskFailed {
		t.Fatalf("done=%v err=%v status=%q", done, err, store.status)
	}
}

func TestSettleScoresInBatchesAndRecordsJudge(t *testing.T) {
	store := newSettleStore(completedItems(settleBatchSize + 2)...)
	attachAnswer(store)
	model := &sequenceModel{replies: []modelReply{{text: `{"score":82,"passed":false,"explanation":"准确"}`}}}
	runner := newTestRunner(store).WithRubricModel(model)

	done, err := runner.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err != nil || done || len(store.detail.Scores) != settleBatchSize || store.status != "" {
		t.Fatalf("first done=%v err=%v scores=%d status=%q", done, err, len(store.detail.Scores), store.status)
	}
	done, err = runner.settleTask(context.Background(), "org", "ws", "evaltask_1", false)
	if err != nil || !done || len(store.detail.Scores) != settleBatchSize+2 || store.status != taskCompleted {
		t.Fatalf("second done=%v err=%v scores=%d status=%q", done, err, len(store.detail.Scores), store.status)
	}
	score := store.detail.Scores[0]
	if score.Status != scoreCompleted || !score.Passed || score.JudgeModel != "judge-model" || score.JudgePromptVersion != scoringPromptVersion {
		t.Fatalf("score = %+v", score)
	}
	if score.JudgeInputTokens != 120 || score.JudgeOutputTokens != 30 {
		t.Fatalf("judge usage = %d/%d", score.JudgeInputTokens, score.JudgeOutputTokens)
	}
	if model.request.Model != "judge-model" || !strings.HasPrefix(model.request.System, scoringSystemPrompt) || strings.Contains(model.request.System, "Markdown scoring rules") {
		t.Fatalf("request = %+v", model.request)
	}
	if !strings.Contains(model.request.System, "回答要准确") || strings.Contains(model.request.User, "回答要准确") {
		t.Fatalf("rubric placement system=%q user=%q", model.request.System, model.request.User)
	}
}

func TestObserverEnqueuesSettleWhenQueueConfigured(t *testing.T) {
	store := newSettleStore(runningItems("sesn_1")...)
	queue := &recordingQueue{}
	runner := newTestRunner(store).WithSettleQueue(queue)

	runner.ObserveSessionEvents(context.Background(), []db.SessionEvent{{
		OrganizationUUID: "org", WorkspaceUUID: "ws", SessionExternalID: "sesn_1", EventType: "session.status_idle",
		Payload: json.RawMessage(`{"stop_reason":{"type":"end_turn"}}`),
	}})
	if len(queue.tasks) != 1 || queue.tasks[0] != "evaltask_1" || store.detail.Items[0].Status != itemCompleted || len(store.detail.Scores) != 0 {
		t.Fatalf("queue=%v item=%+v scores=%d", queue.tasks, store.detail.Items[0], len(store.detail.Scores))
	}
}

func TestSweepReconcilesStaleItemsAndSettlesReadyTasks(t *testing.T) {
	store := newSettleStore(runningItems("sesn_done", "sesn_busy", "sesn_gone", "sesn_archived")...)
	store.stale = append([]db.EvaluationTaskItem(nil), store.detail.Items...)
	store.ready = []db.EvaluationTaskRef{{OrganizationUUID: "org", WorkspaceUUID: "ws", ExternalID: "evaltask_ready"}}
	archivedAt := time.Now().UTC()
	store.sessions["sesn_done"] = db.Session{Status: "idle"}
	store.sessions["sesn_busy"] = db.Session{Status: "running", CreatedAt: archivedAt}
	store.sessions["sesn_archived"] = db.Session{Status: "idle", ArchivedAt: &archivedAt}
	store.events["sesn_done"] = []db.SessionEvent{idleEvent("retries_exhausted"), {EventType: "agent.message"}, idleEvent("end_turn")}
	queue := &recordingQueue{}

	if err := newTestRunner(store).WithSettleQueue(queue).sweep(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	statuses := make([]string, 0, len(store.detail.Items))
	for _, item := range store.detail.Items {
		statuses = append(statuses, item.Status)
	}
	if strings.Join(statuses, ",") != "completed,running,failed,failed" || store.detail.Items[2].ErrorMessage != sessionTerminatedMessage {
		t.Fatalf("items = %+v", store.detail.Items)
	}
	if len(store.touched) != 1 || store.touched[0] != "item-1" {
		t.Fatalf("touched = %v", store.touched)
	}
	if strings.Join(queue.tasks, ",") != "evaltask_1,evaltask_ready" {
		t.Fatalf("queue = %v", queue.tasks)
	}
}

func TestSweepAbandonsItemsThatNeverStarted(t *testing.T) {
	items := completedItems(3)
	items[0].Status = itemPending
	items[1].Status = itemRunning
	items[2].Status, items[2].SessionExternalID = itemRunning, "sesn_live"
	store := newSettleStore(items...)
	store.unstarted = append([]db.EvaluationTaskItem(nil), store.detail.Items...)
	queue := &recordingQueue{}

	if err := newTestRunner(store).WithSettleQueue(queue).sweep(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if store.detail.Items[0].Status != itemFailed || store.detail.Items[1].Status != itemFailed || store.detail.Items[2].Status != itemRunning {
		t.Fatalf("items = %+v", store.detail.Items)
	}
	if store.detail.Items[0].ErrorMessage != startInterruptedMessage {
		t.Fatalf("message = %q", store.detail.Items[0].ErrorMessage)
	}
	if strings.Join(queue.tasks, ",") != "evaltask_1" {
		t.Fatalf("queue = %v", queue.tasks)
	}
}

func TestSweepFinishesItemWaitingForUser(t *testing.T) {
	store := newSettleStore(runningItems("sesn_ask")...)
	store.stale = append([]db.EvaluationTaskItem(nil), store.detail.Items...)
	store.sessions["sesn_ask"] = db.Session{Status: "idle"}
	store.events["sesn_ask"] = []db.SessionEvent{{
		EventType: "session.status_idle", Payload: json.RawMessage(`{"stop_reason":{"type":"requires_action","event_ids":["tool"]}}`),
	}}
	queue := &recordingQueue{}

	if err := newTestRunner(store).WithSettleQueue(queue).sweep(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if store.detail.Items[0].Status != itemCompleted || store.detail.Items[0].ErrorMessage != "" || len(store.touched) != 0 {
		t.Fatalf("item = %+v touched = %v", store.detail.Items[0], store.touched)
	}
	if len(queue.tasks) != 1 || queue.tasks[0] != "evaltask_1" {
		t.Fatalf("queue = %v", queue.tasks)
	}
}

func TestLaunchFailsItemsWhenSessionStartFails(t *testing.T) {
	items := completedItems(2)
	for index := range items {
		items[index].Status = itemPending
	}
	store := newSettleStore(items...)
	sessions := &recordingSessions{err: errors.New("quota exceeded")}
	runner := newTestRunner(store).WithSessions(sessions)

	runner.Launch(context.Background(), auth.Principal{}, store.detail.Task, store.detail.Items)
	for _, item := range store.detail.Items {
		if item.Status != itemFailed || item.ErrorMessage != "quota exceeded" {
			t.Fatalf("item = %+v", item)
		}
	}
	if store.status != taskFailed {
		t.Fatalf("status = %q", store.status)
	}
}

func TestLaunchArchivesSessionWhenItemWasClaimedElsewhere(t *testing.T) {
	items := completedItems(1)
	items[0].Status = itemPending
	store := newSettleStore(items...)
	store.attachErr = errors.New("connection reset")
	sessions := &recordingSessions{}
	runner := newTestRunner(store).WithSessions(sessions)

	runner.Launch(context.Background(), auth.Principal{}, store.detail.Task, store.detail.Items)
	if strings.Join(sessions.archived, ",") != "sesn_evalitem_0" {
		t.Fatalf("archived = %v", sessions.archived)
	}
	if store.detail.Items[0].Status != itemRunning || store.detail.Items[0].SessionExternalID != "" {
		t.Fatalf("item = %+v", store.detail.Items[0])
	}
}

func TestLaunchSendsCaseInputs(t *testing.T) {
	inputs := json.RawMessage(`[{"type":"text","text":"描述这张图里的金额"},{"type":"image","source":{"type":"file","file_id":"file_demo"}}]`)
	store := newSettleStore(
		db.EvaluationTaskItem{UUID: "item-plain", ExternalID: "evalitem_plain", Position: 1, Status: itemPending, Query: "汇总销售额"},
		db.EvaluationTaskItem{UUID: "item-image", ExternalID: "evalitem_image", Position: 2, Status: itemPending, Query: "描述这张图里的金额", Inputs: inputs},
		db.EvaluationTaskItem{UUID: "item-done", ExternalID: "evalitem_done", Position: 3, Status: itemCompleted, Query: "已完成"},
	)
	sessions := &recordingSessions{}
	newTestRunner(store).WithSessions(sessions).Launch(context.Background(), auth.Principal{}, store.detail.Task, store.detail.Items)
	byItem := map[string]SessionInput{}
	for _, input := range sessions.inputs {
		byItem[input.Metadata["evaluation_item_id"]] = input
	}
	if len(byItem) != 2 {
		t.Fatalf("inputs = %+v", sessions.inputs)
	}
	plain, image := byItem["evalitem_plain"], byItem["evalitem_image"]
	if len(plain.Content) != 0 || plain.Query != "汇总销售额" || plain.Title != "回归 #1" {
		t.Fatalf("plain input = %+v", plain)
	}
	if string(image.Content) != string(inputs) || image.Title != "回归 #2" {
		t.Fatalf("image input = %+v", image)
	}
	if store.detail.Items[0].SessionExternalID != "sesn_evalitem_plain" || store.detail.Items[1].Status != itemRunning {
		t.Fatalf("items = %+v", store.detail.Items)
	}
}

func TestCancelArchivesRunningSessions(t *testing.T) {
	items := runningItems("sesn_live", "")
	items[1].Status = itemPending
	store := newSettleStore(append(items, completedItems(3)[2])...)
	sessions := &recordingSessions{}
	handler := NewHandler(store, nil).WithTaskRunner(newTestRunner(store).WithSessions(sessions))

	if err := handler.cancelTask(httptest.NewRecorder(), withTaskRunRoute(httptest.NewRequest(http.MethodPost, "/", nil))); err != nil {
		t.Fatal(err)
	}
	if store.status != taskCancelled || strings.Join(sessions.archived, ",") != "sesn_live" {
		t.Fatalf("status=%q archived=%v", store.status, sessions.archived)
	}
	if store.detail.Items[0].Status != itemCancelled || store.detail.Items[1].Status != itemCancelled || store.detail.Items[2].Status != itemCompleted {
		t.Fatalf("items = %+v", store.detail.Items)
	}
	err := handler.cancelTask(httptest.NewRecorder(), withTaskRunRoute(httptest.NewRequest(http.MethodPost, "/", nil)))
	if publicMessage(err) != cancelUnavailableMessage {
		t.Fatalf("second cancel err = %v", err)
	}
}

func TestTaskRoutesRequireRunner(t *testing.T) {
	handler := NewHandler(newSettleStore(), nil)
	if err := handler.cancelTask(httptest.NewRecorder(), withTaskRunRoute(httptest.NewRequest(http.MethodPost, "/", nil))); publicMessage(err) != starterMissingMessage {
		t.Fatalf("err = %v", err)
	}
}

func TestRetryRejectsTaskWithoutRetryableWork(t *testing.T) {
	store := newSettleStore(completedItems(1)...)
	store.detail.Task.Status = taskCompleted
	store.retryErr = db.ErrPreconditionFailed
	handler := NewHandler(store, nil).WithTaskRunner(newTestRunner(store))

	err := handler.retryTask(httptest.NewRecorder(), withTaskRunRoute(httptest.NewRequest(http.MethodPost, "/", nil)))
	if publicMessage(err) != retryUnavailableMessage {
		t.Fatalf("err = %v", err)
	}
}

func TestRetryRescoresFailedScores(t *testing.T) {
	store := newSettleStore(completedItems(2)...)
	store.detail.Task.Status = taskCompleted
	store.detail.Scores = []db.EvaluationTaskScore{
		{ItemUUID: "item-0", Status: scoreCompleted, Score: 70, Passed: true},
		{ItemUUID: "item-1", Status: scoreFailed, ErrorMessage: scoreFailedMessage},
	}
	model := &sequenceModel{replies: []modelReply{{text: `{"score":88}`}}}
	attachAnswer(store)
	handler := NewHandler(store, nil).WithTaskRunner(newTestRunner(store).WithRubricModel(model))
	response := httptest.NewRecorder()

	if err := handler.retryTask(response, withTaskRunRoute(httptest.NewRequest(http.MethodPost, "/", nil))); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || store.status != taskCompleted || len(store.detail.Scores) != 2 {
		t.Fatalf("calls=%d status=%q scores=%+v", model.calls, store.status, store.detail.Scores)
	}
	rescored := store.detail.Scores[1]
	if rescored.ItemUUID != "item-1" || rescored.Status != scoreCompleted || rescored.Score != 88 {
		t.Fatalf("rescored = %+v", rescored)
	}
}

func TestStaleSettleCannotCloseRetriedTask(t *testing.T) {
	store := newSettleStore(completedItems(2)...)
	store.detail.Scores = []db.EvaluationTaskScore{
		{ItemUUID: "item-0", Status: scoreCompleted, Score: 70, Passed: true},
		{ItemUUID: "item-1", Status: scoreFailed, ErrorMessage: scoreFailedMessage},
	}
	attachAnswer(store)
	store.beforeFinish = func() {
		if _, err := store.RetryEvaluationTask(context.Background(), "org", "ws", "task", 1, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	model := &sequenceModel{replies: []modelReply{{text: `{"score":88}`}}}
	runner := newTestRunner(store).WithRubricModel(model)

	done, err := runner.settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || done || store.status != "" || store.detail.Task.Status != taskRunning {
		t.Fatalf("stale done=%v err=%v status=%q task=%q", done, err, store.status, store.detail.Task.Status)
	}
	done, err = runner.settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || store.status != taskCompleted || model.calls != 1 || len(store.detail.Scores) != 2 {
		t.Fatalf("rescore done=%v err=%v status=%q calls=%d scores=%+v", done, err, store.status, model.calls, store.detail.Scores)
	}
}

func TestSweepFailsItemThatRunsTooLong(t *testing.T) {
	store := newSettleStore(runningItems("sesn_stuck", "sesn_fresh", "sesn_idle")...)
	store.stale = append([]db.EvaluationTaskItem(nil), store.detail.Items...)
	now := time.Now().UTC()
	store.sessions["sesn_stuck"] = db.Session{Status: "running", CreatedAt: now.Add(-maxItemRun - time.Minute)}
	store.sessions["sesn_fresh"] = db.Session{Status: "running", CreatedAt: now.Add(-time.Minute)}
	store.sessions["sesn_idle"] = db.Session{Status: "idle", CreatedAt: now.Add(-maxItemRun - time.Minute)}
	queue := &recordingQueue{}

	if err := newTestRunner(store).WithSettleQueue(queue).sweep(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	stuck, fresh, idle := store.detail.Items[0], store.detail.Items[1], store.detail.Items[2]
	if stuck.Status != itemFailed || stuck.ErrorMessage != itemTimedOutMessage || idle.Status != itemFailed || idle.ErrorMessage != itemTimedOutMessage {
		t.Fatalf("timed out items = %+v %+v", stuck, idle)
	}
	if fresh.Status != itemRunning || strings.Join(store.touched, ",") != "item-1" || strings.Join(queue.tasks, ",") != "evaltask_1" {
		t.Fatalf("fresh=%+v touched=%v queue=%v", fresh, store.touched, queue.tasks)
	}
}

func TestJudgeModelRejectsWorkspaceWithoutModel(t *testing.T) {
	handler := NewHandler(&evaluationFixture{}, nil).WithRubricModel(resolverModel{})
	if _, err := handler.judgeModel(context.Background(), auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}, ""); publicMessage(err) != modelMissingMessage {
		t.Fatalf("err = %v", err)
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

	done, err := newTestRunner(store).WithRubricModel(model).settleTask(context.Background(), "org", "ws", "evaltask_1", true)
	if err != nil || !done || len(store.detail.Scores) != 1 {
		t.Fatalf("done=%v err=%v scores=%d", done, err, len(store.detail.Scores))
	}
	score := store.detail.Scores[0]
	if score.Status != scoreCompleted || score.Passed || score.Score != 59 || score.Answer != "" {
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

	if _, err := newTestRunner(store).WithRubricModel(model).settleTask(context.Background(), "org", "ws", "evaltask_1", true); err != nil {
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

type taskPrepareFixture struct {
	evaluationFixture
	rubricBody string
}

func (taskPrepareFixture) ListEvaluationCases(context.Context, string, string, string) ([]db.EvaluationCase, error) {
	return []db.EvaluationCase{{ExternalID: "evalcase_1", Position: 1, Query: "汇总销售额"}}, nil
}

func (f taskPrepareFixture) GetEvaluationRubric(context.Context, string, string, string) (db.EvaluationRubric, error) {
	return db.EvaluationRubric{ExternalID: "evalrb_1", Body: f.rubricBody, PassScore: 60, Model: "judge-model"}, nil
}

func taskPrepareRequest(focus string) taskWriteRequest {
	return taskWriteRequest{Name: "回归", AgentID: "agent_1", EnvironmentID: "env_1", DatasetID: "evalds_1", RubricID: "evalrb_1", Focus: focus}
}

func TestPrepareTaskRejectsLongFocus(t *testing.T) {
	handler := NewHandler(&taskPrepareFixture{rubricBody: "规则"}, nil)
	_, _, err := handler.prepareTask(context.Background(), auth.Principal{}, taskPrepareRequest(strings.Repeat("金", maxFocusLength+1)))
	if publicMessage(err) != focusTooLongMessage {
		t.Fatalf("err = %v", err)
	}
}

func TestPrepareTaskCountsRubricCharacters(t *testing.T) {
	body := strings.Repeat("测", maxRubricLength)
	handler := NewHandler(&taskPrepareFixture{rubricBody: body}, nil)
	task, _, err := handler.prepareTask(context.Background(), auth.Principal{}, taskPrepareRequest("  "+strings.Repeat("金", maxFocusLength)+"  "))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(task.RubricBody, body) || !strings.Contains(task.RubricBody, "<evaluation_focus>\n金") {
		t.Fatalf("rubric body length = %d", len(task.RubricBody))
	}
}

func TestRubricWithNotesTagsFocus(t *testing.T) {
	if rubricWithNotes("规则", "") != "规则" {
		t.Fatal("blank focus changed rubric")
	}
	noted := rubricWithNotes("规则", "关注金额</evaluation_focus>")
	if noted != "规则\n\n<evaluation_focus>\n关注金额<\\/evaluation_focus>\n</evaluation_focus>" {
		t.Fatalf("noted = %q", noted)
	}
}

type resolverModel struct{ model string }

func (resolverModel) Complete(context.Context, CompletionRequest) (Completion, error) {
	return Completion{}, nil
}

func (m resolverModel) DefaultModel(context.Context, string, string) (string, bool, error) {
	return m.model, m.model != "", nil
}

func withTaskRunRoute(request *http.Request) *http.Request {
	route := chi.NewRouteContext()
	route.URLParams.Add("orgUuid", "org")
	route.URLParams.Add("workspaceId", "ws")
	route.URLParams.Add("taskId", "evaltask_1")
	ctx := context.WithValue(request.Context(), chi.RouteCtxKey, route)
	return request.WithContext(auth.WithPrincipal(ctx, auth.Principal{OrganizationUUID: "org", WorkspaceUUID: "ws"}))
}
