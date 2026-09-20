package dreams

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
)

// RunningWorker turns observed Session completion into Dream terminal states.
// It also enforces the runtime budget: a Dream running longer than runTimeout
// since started_at fails with `timeout` and its internal Session is
// interrupted. The Session and sandbox stay available by default so the user
// can inspect the live environment; archiving the Dream archives the Session.
// keep_runtime=false re-enables leftover sandbox Kill and Session archive.
type RunningWorker struct {
	database     *db.DB
	codeSessions *codesessions.Service
	reclaimer    *terminalRuntimeReclaimer
	archiver     *InternalSessionArchiver
	runTimeout   time.Duration
	logger       *slog.Logger
}

func NewRunningWorker(database *db.DB, codeSessions *codesessions.Service, runtime sessionRuntime, runTimeout time.Duration, keepRuntime bool, logger *slog.Logger) *RunningWorker {
	worker := &RunningWorker{
		database:     database,
		codeSessions: codeSessions,
		runTimeout:   runTimeout,
		logger:       logging.LoggerOrDefault(logger),
	}
	if !keepRuntime {
		worker.reclaimer = newTerminalRuntimeReclaimer(database, runtime)
		worker.archiver = NewInternalSessionArchiver(database)
	}
	return worker
}

func (w *RunningWorker) Start(ctx context.Context) {
	if w == nil || w.database == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(dreamWorkerInterval)
		defer ticker.Stop()
		for {
			if err := w.RunOnce(ctx); err != nil {
				w.logger.ErrorContext(ctx, "dream running worker", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (w *RunningWorker) RunOnce(ctx context.Context) error {
	dreams, err := w.database.ListDreamsByStatus(ctx, "running", dreamWorkerBatch)
	if err != nil {
		return err
	}
	errs := make([]error, 0)
	for _, dream := range dreams {
		if err := w.observe(ctx, dream); err != nil {
			w.logger.ErrorContext(ctx, "observe running dream", "dream_id", dream.ExternalID, "error", err)
			errs = append(errs, err)
		}
	}
	if err := w.retryStoppedDreamInterrupts(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := w.reclaimer.RunOnce(ctx); err != nil {
		errs = append(errs, err)
	}
	if w.archiver == nil {
		return errors.Join(errs...)
	}
	if err := w.archiver.RunOnce(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (w *RunningWorker) observe(ctx context.Context, dream db.Dream) error {
	output, err := dreamOutput(dream.Outputs)
	if err != nil {
		return w.fail(ctx, dream, nil, err, nil)
	}
	session, found, err := w.database.GetSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
	if err != nil {
		return err
	}
	if !found || session.ArchivedAt != nil {
		return w.fail(ctx, dream, nil, errors.New("internal Dream session is unavailable"), nil)
	}
	if session.Status == "terminated" {
		return w.fail(ctx, dream, &session, errors.New("internal Dream session terminated"), session.Usage)
	}
	events, err := loadLifecycleEvents(ctx, w.database, dream, session)
	if err != nil {
		return err
	}
	usage := aggregateDreamUsage(events, session.Usage)
	if failure := terminalSessionFailure(events); failure != "" {
		return w.fail(ctx, dream, &session, errors.New(failure), usage)
	}
	if dreamSessionFinished(session, events) {
		_, _, err = w.database.MarkDreamTerminal(ctx, dream.WorkspaceUUID, dream.ExternalID, "completed", json.RawMessage(`null`), usage, time.Now().UTC())
		return err
	}
	// Still in flight. A finished Session above always wins over the budget so
	// a Dream that completed moments before the deadline is not reported as
	// timed out; anything still running past the budget is stopped here.
	if exceeded, elapsed := dreamRunTimeoutExceeded(dream, w.runTimeout, time.Now().UTC()); exceeded {
		return w.fail(ctx, dream, &session, permanentDreamErrorOfType(dreamErrorTimeout, fmt.Errorf("Dream exceeded its %s runtime budget after %s", w.runTimeout, elapsed.Round(time.Second))), usage)
	}
	if err := w.persistRunningUsage(ctx, dream, usage); err != nil {
		return err
	}
	if !containsModelRequestStart(events) {
		return w.retryDreamCommand(ctx, dream, session)
	}
	return nil
}

func (w *RunningWorker) persistRunningUsage(ctx context.Context, dream db.Dream, usage json.RawMessage) error {
	if dreamUsageUnchanged(dream.Usage, usage) {
		return nil
	}
	_, _, err := w.database.UpdateRunningDreamUsage(ctx, dream.WorkspaceUUID, dream.ExternalID, usage, time.Now().UTC())
	return err
}

// dreamRunTimeoutExceeded measures the runtime budget from started_at, which
// StartDream sets in the same transaction that exposes running. A zero timeout
// disables the budget; a Dream without started_at cannot be judged.
func dreamRunTimeoutExceeded(dream db.Dream, timeout time.Duration, now time.Time) (bool, time.Duration) {
	if timeout <= 0 || dream.StartedAt == nil {
		return false, 0
	}
	elapsed := now.Sub(*dream.StartedAt)
	return elapsed > timeout, elapsed
}

type dreamLifecycleEventStore interface {
	ListSessionEventsPage(ctx context.Context, params db.ListSessionEventsPageParams) ([]db.SessionEvent, bool, error)
}

func loadLifecycleEvents(ctx context.Context, store dreamLifecycleEventStore, dream db.Dream, session db.Session) ([]db.SessionEvent, error) {
	const pageSize = 500
	var cursor *db.SessionEventPageCursor
	var events []db.SessionEvent
	for {
		page, more, err := store.ListSessionEventsPage(ctx, db.ListSessionEventsPageParams{
			WorkspaceUUID: dream.WorkspaceUUID, SessionExternalID: session.ExternalID, PrimaryOnly: true,
			Limit: pageSize, Cursor: cursor, Types: []string{"span.model_request_start", "span.model_request_end", "system.message", "agent.message"},
		})
		if err != nil {
			return nil, err
		}
		events = append(events, page...)
		if !more || len(page) == 0 {
			return events, nil
		}
		last := page[len(page)-1]
		cursor = &db.SessionEventPageCursor{CreatedAt: last.CreatedAt, UUID: last.UUID}
	}
}

func (w *RunningWorker) retryDreamCommand(ctx context.Context, dream db.Dream, session db.Session) error {
	if w.codeSessions == nil {
		return nil
	}
	event, err := w.database.GetSessionEvent(ctx, dream.WorkspaceUUID, session.ExternalID, dreamCommandEventID(dream))
	if err != nil {
		return fmt.Errorf("load durable Dream command: %w", err)
	}
	if err := w.codeSessions.QueuePublicSessionEvents(ctx, session, []db.SessionEvent{event}); err != nil {
		return fmt.Errorf("retry Dream command dispatch: %w", err)
	}
	return nil
}

// retryStoppedDreamInterrupts re-delivers the stable user.interrupt for
// canceled or failed (including timed-out) Dreams whose internal Session is
// still running because the first publish did not reach the Code Session.
func (w *RunningWorker) retryStoppedDreamInterrupts(ctx context.Context) error {
	if w.codeSessions == nil {
		return nil
	}
	dreams, err := w.database.ListStoppedDreamsWithActiveSession(ctx, dreamWorkerBatch)
	if err != nil {
		return err
	}
	var errs []error
	for _, dream := range dreams {
		output, outputErr := dreamOutput(dream.Outputs)
		if outputErr != nil {
			continue
		}
		session, found, loadErr := w.database.GetSession(ctx, dream.WorkspaceUUID, output.InternalSessionID)
		if loadErr != nil {
			errs = append(errs, loadErr)
			continue
		}
		if !found || session.ArchivedAt != nil || (session.Status != "running" && session.Status != "rescheduling") {
			continue
		}
		if interruptErr := interruptDreamSession(ctx, w.database, w.codeSessions, dream); interruptErr != nil {
			errs = append(errs, fmt.Errorf("retry stopped Dream interrupt %s: %w", dream.ExternalID, interruptErr))
		}
	}
	return errors.Join(errs...)
}

// fail closes the Dream as failed, then asks the still-active internal Session
// to interrupt. The Session stays available for inspection unless
// keep_runtime=false. Interrupt delivery failures are retried by
// retryStoppedDreamInterrupts on later ticks, so they are logged, not returned.
func (w *RunningWorker) fail(ctx context.Context, dream db.Dream, session *db.Session, cause error, usage json.RawMessage) error {
	if len(usage) == 0 && session != nil {
		usage = session.Usage
	}
	won, err := failDreamContract(ctx, w.database, dream, cause, usage)
	if err != nil {
		return err
	}
	if won && session != nil && w.codeSessions != nil && (session.Status == "running" || session.Status == "rescheduling") {
		if interruptErr := interruptDreamSession(ctx, w.database, w.codeSessions, dream); interruptErr != nil {
			w.logger.WarnContext(ctx, "defer failed dream interrupt", "dream_id", dream.ExternalID, "error", interruptErr)
		}
	}
	if w.archiver == nil {
		return nil
	}
	return w.archiver.RunOnce(ctx)
}
