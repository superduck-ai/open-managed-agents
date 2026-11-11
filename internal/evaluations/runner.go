package evaluations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

const launchConcurrency = 4

type RunnerStore interface {
	GetEvaluationTask(context.Context, string, string, string) (db.EvaluationTaskDetail, error)
	ListRunningEvaluationItemsBySession(context.Context, string, string, string) ([]db.EvaluationTaskItem, error)
	ClaimEvaluationItem(context.Context, string, string, string, time.Time) (bool, error)
	AttachEvaluationItemSession(context.Context, string, string, string, string, time.Time) (bool, error)
	AbandonEvaluationItem(context.Context, string, string, string, string, time.Time) (bool, error)
	FinishRunningEvaluationItem(context.Context, string, string, string, string, string, time.Time) (bool, error)
	TouchRunningEvaluationItem(context.Context, string, string, string, time.Time) error
	ListStaleRunningEvaluationItems(context.Context, time.Time, int) ([]db.EvaluationTaskItem, error)
	ListUnstartedEvaluationItems(context.Context, time.Time, int) ([]db.EvaluationTaskItem, error)
	ListSettleableEvaluationTasks(context.Context, time.Time, int) ([]db.EvaluationTaskRef, error)
	CancelEvaluationTask(context.Context, string, string, string, time.Time) ([]db.EvaluationTaskItem, error)
	FinishEvaluationTask(context.Context, string, string, string, string, time.Time) (bool, error)
	CreateEvaluationTaskScore(context.Context, db.EvaluationTaskScore) (db.EvaluationTaskScore, error)
	GetSession(context.Context, string, string) (db.Session, bool, error)
	ListSessionEventsPage(context.Context, db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)
}

type SessionControl interface {
	Start(context.Context, SessionInput) (string, error)
	Archive(context.Context, auth.Principal, string) error
}

type SettleQueue interface {
	EnqueueSettle(ctx context.Context, organizationUUID, workspaceUUID, taskID string) error
}

type TaskRunner struct {
	store    RunnerStore
	logger   *slog.Logger
	sessions SessionControl
	model    RubricModel
	queue    SettleQueue
	spawn    func(func())
}

func NewTaskRunner(store RunnerStore, logger *slog.Logger) *TaskRunner {
	return &TaskRunner{store: store, logger: logging.LoggerOrDefault(logger), spawn: func(run func()) { go run() }}
}

func (r *TaskRunner) WithSessions(sessions SessionControl) *TaskRunner {
	r.sessions = sessions
	return r
}

func (r *TaskRunner) WithRubricModel(model RubricModel) *TaskRunner {
	r.model = model
	return r
}

func (r *TaskRunner) WithSettleQueue(queue SettleQueue) *TaskRunner {
	r.queue = queue
	return r
}

func (r *TaskRunner) Launch(ctx context.Context, principal auth.Principal, task db.EvaluationTask, items []db.EvaluationTaskItem) {
	pending := make([]db.EvaluationTaskItem, 0, len(items))
	for _, item := range items {
		if item.Status == itemPending {
			pending = append(pending, item)
		}
	}
	r.runDetached(ctx, "evaluation launch panicked", func(ctx context.Context) {
		r.startItems(ctx, principal, task, pending)
		r.requestSettle(ctx, task.OrganizationUUID, task.WorkspaceUUID, task.ExternalID)
	})
}

func (r *TaskRunner) Cancel(ctx context.Context, principal auth.Principal, task db.EvaluationTask) error {
	items, err := r.store.CancelEvaluationTask(ctx, task.OrganizationUUID, task.WorkspaceUUID, task.UUID, time.Now().UTC())
	if err != nil {
		return cancelStoreError(err)
	}
	var sessions []string
	for _, item := range items {
		if item.SessionExternalID != "" {
			sessions = append(sessions, item.SessionExternalID)
		}
	}
	if len(sessions) == 0 {
		return nil
	}
	r.runDetached(ctx, "evaluation cancel panicked", func(ctx context.Context) {
		for _, sessionID := range sessions {
			r.archiveSession(ctx, principal, sessionID)
		}
	})
	return nil
}

func (r *TaskRunner) startItems(ctx context.Context, principal auth.Principal, task db.EvaluationTask, items []db.EvaluationTaskItem) {
	queue := make(chan db.EvaluationTaskItem)
	var workers sync.WaitGroup
	for range min(launchConcurrency, len(items)) {
		workers.Go(func() {
			for item := range queue {
				r.guard(ctx, "evaluation item start panicked", func() { r.startItem(ctx, principal, task, item) })
			}
		})
	}
	for _, item := range items {
		queue <- item
	}
	close(queue)
	workers.Wait()
}

func (r *TaskRunner) startItem(ctx context.Context, principal auth.Principal, task db.EvaluationTask, item db.EvaluationTaskItem) {
	claimed, err := r.store.ClaimEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, time.Now().UTC())
	if err != nil {
		r.logger.ErrorContext(ctx, "claim evaluation item", "item_id", item.ExternalID, "error", err)
		return
	}
	if !claimed {
		return
	}
	if r.sessions == nil {
		r.failItem(ctx, item, starterMissingMessage)
		return
	}
	sessionID, err := r.sessions.Start(ctx, sessionInput(principal, task, item))
	if err != nil {
		r.logger.WarnContext(ctx, "start evaluation session", "item_id", item.ExternalID, "error", err)
		r.failItem(ctx, item, clipError(err.Error()))
		return
	}
	attached, err := r.store.AttachEvaluationItemSession(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, sessionID, time.Now().UTC())
	if err != nil {
		r.logger.ErrorContext(ctx, "attach evaluation session", "item_id", item.ExternalID, "session_id", sessionID, "error", err)
	}
	if err != nil || !attached {
		r.archiveSession(ctx, principal, sessionID)
	}
}

func sessionInput(principal auth.Principal, task db.EvaluationTask, item db.EvaluationTaskItem) SessionInput {
	return SessionInput{
		Principal: principal, AgentExternalID: task.AgentExternalID, AgentVersion: task.AgentVersion,
		EnvironmentExternalID: task.EnvironmentExternalID, Resources: task.Resources, VaultIDs: vaultList(task.VaultIDs),
		Query: item.Query, Content: item.Inputs, Title: clip(task.Name+" #"+strconv.Itoa(item.Position), 120),
		Metadata: map[string]string{"evaluation_task_id": task.ExternalID, "evaluation_item_id": item.ExternalID},
	}
}

func (r *TaskRunner) failItem(ctx context.Context, item db.EvaluationTaskItem, message string) {
	if _, err := r.store.FinishRunningEvaluationItem(ctx, item.OrganizationUUID, item.WorkspaceUUID, item.UUID, itemFailed, message, time.Now().UTC()); err != nil {
		r.logger.ErrorContext(ctx, "fail evaluation item", "item_id", item.ExternalID, "error", err)
	}
}

func (r *TaskRunner) archiveSession(ctx context.Context, principal auth.Principal, sessionID string) {
	if r.sessions == nil {
		return
	}
	if err := r.sessions.Archive(ctx, principal, sessionID); err != nil && !errors.Is(err, db.ErrNotFound) {
		r.logger.WarnContext(ctx, "archive evaluation session", "session_id", sessionID, "error", err)
	}
}

func (r *TaskRunner) runDetached(ctx context.Context, panicMessage string, run func(context.Context)) {
	detached := context.WithoutCancel(ctx)
	r.background(detached, panicMessage, func() { run(detached) })
}

func (r *TaskRunner) background(ctx context.Context, panicMessage string, run func()) {
	r.spawn(func() { r.guard(ctx, panicMessage, run) })
}

func (r *TaskRunner) guard(ctx context.Context, panicMessage string, run func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.logger.ErrorContext(ctx, panicMessage, "error", fmt.Sprint(recovered), "stack", string(debug.Stack()))
		}
	}()
	run()
}
