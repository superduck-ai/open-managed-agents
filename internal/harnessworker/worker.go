package harnessworker

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/cloudharness"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const (
	leaseDuration   = 75 * time.Second
	heartbeatPeriod = 10 * time.Second
	settleTimeout   = 5 * time.Second
	retryDelay      = 5 * time.Second
)

var errNeedsRedelivery = errors.New("harness turn needs redelivery")

type Worker struct {
	database *db.DB
	queue    *Queue
	apiURL   string
	logger   *slog.Logger
}

func New(database *db.DB, queue *Queue, apiURL string, logger *slog.Logger) *Worker {
	return &Worker{database: database, queue: queue, apiURL: apiURL, logger: logger}
}

func (w *Worker) RunOnce(ctx context.Context, wait, timeout time.Duration) (bool, error) {
	message, err := w.queue.fetchOne(ctx, wait)
	if err != nil || message == nil {
		return false, err
	}
	return true, w.executeDelivery(ctx, message, timeout, nil)
}

func (w *Worker) executeDelivery(ctx context.Context, message jetstream.Msg, timeout time.Duration, loop *deliveryLoop) error {
	execute, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return w.runDelivery(execute, message, loop)
}

func (w *Worker) runDelivery(ctx context.Context, message jetstream.Msg, loop *deliveryLoop) error {
	delivery, err := decodeDelivery(message.Data())
	if err != nil {
		return errors.Join(err, message.Term())
	}
	ref, work := delivery.Turn, delivery.claim()
	if kind := delivery.resumeKind(); kind != "" {
		task, found, err := w.database.HarnessResumeWork(ctx, ref, kind)
		if err != nil {
			return redeliver(message, err)
		}
		if !found {
			return message.DoubleAck(ctx)
		}
		ref = task.Ref
		work = nextWork(work, task)
	}
	for ctx.Err() == nil {
		next, err := w.runWork(ctx, message, ref, work, delivery, loop)
		if err != nil || next == nil {
			return err
		}
		work = nextWork(work, *next)
	}
	return redeliver(message, ctx.Err())
}

func nextWork(delivery db.HarnessTurnDelivery, task db.HarnessTask) db.HarnessTurnDelivery {
	delivery.TaskUUID, delivery.ToolUUID, delivery.Kind = task.UUID, task.ToolUUID, task.Kind
	delivery.LegacyInput = false
	return delivery
}

func (w *Worker) runWork(ctx context.Context, message jetstream.Msg, ref db.HarnessTurnRef, work db.HarnessTurnDelivery, input deliveryEnvelope, loop *deliveryLoop) (*db.HarnessTask, error) {
	token, err := codesessions.NewModelAccessToken()
	if err != nil {
		return nil, redeliver(message, err)
	}
	claim, err := w.database.ClaimHarnessTurn(ctx, ref, work, auth.HashAPIKey(token), time.Now().Add(leaseDuration))
	if errors.Is(err, db.ErrNotFound) || errors.Is(err, db.ErrInvalidState) {
		return nil, message.DoubleAck(ctx)
	}
	if err != nil {
		return nil, redeliver(message, err)
	}
	if claim.Superseded {
		return &claim.Task, nil
	}
	if terminal(claim.State) {
		if err := message.DoubleAck(ctx); err != nil {
			return nil, err
		}
		w.logger.InfoContext(ctx, "harness settled delivery acknowledged", "turn_id", ref.UUID, "state", claim.State)
		return nil, nil
	}
	loop.logClaim(ctx, w.logger,
		slog.String("task_id", cmp.Or(claim.Task.UUID, claim.Lease.Ref.UUID)),
		slog.String("task_kind", cmp.Or(claim.Task.Kind, db.HarnessModelTask)),
		slog.String("turn_id", claim.Lease.Ref.UUID), slog.String("session_id", claim.CodeSession.SessionExternalID))
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go w.heartbeat(runCtx, cancel, claim.Lease, message, done)
	runErr := w.executeWork(runCtx, claim, token, input)
	cancel()
	heartbeatErr := <-done
	return w.settle(ctx, claim, message, errors.Join(runErr, heartbeatErr))
}

func (w *Worker) heartbeat(ctx context.Context, cancel context.CancelFunc, lease db.HarnessTurnLease, message jetstream.Msg, done chan<- error) {
	ticker := time.NewTicker(heartbeatPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			err := w.database.RenewHarnessTurn(ctx, lease, time.Now().Add(leaseDuration))
			if err == nil {
				err = message.InProgress()
			}
			if err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func (w *Worker) settle(ctx context.Context, claim db.HarnessTurnClaim, message jetstream.Msg, runErr error) (*db.HarnessTask, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), settleTimeout)
	defer cancel()
	lease := claim.Lease
	task, state, err := w.database.HarnessTurnTask(ctx, lease.Ref)
	if err != nil {
		return nil, redeliver(message, runErr, err)
	}
	if terminal(state) {
		return nil, w.acknowledge(ctx, message, lease.Ref, state)
	}
	if task.UUID != claim.Task.UUID {
		return &task, nil
	}
	if state == "running" {
		err = w.database.ReleaseHarnessTurn(ctx, lease)
	}
	return nil, redeliver(message, runErr, err)
}

func terminal(state string) bool {
	return state == "completed" || state == "failed" || state == db.HarnessWaitingChoice ||
		state == db.HarnessWaitingTool || state == db.HarnessInterrupted
}

func (w *Worker) executeWork(ctx context.Context, claim db.HarnessTurnClaim, token string, delivery deliveryEnvelope) error {
	if claim.Task.Kind == db.HarnessToolTask {
		tool, err := w.database.ActiveHarnessTool(ctx, claim.Lease.Ref, claim.Task.UUID, claim.Lease.Epoch)
		if err != nil {
			return err
		}
		threadID := claim.Lease.Ref.SessionUUID
		if claim.Subagent != nil {
			threadID = claim.Subagent.UUID
		}
		return stepClaimedTask(ctx, cloudharness.ClaimedTask{
			Work: cloudharness.ToolWork{Call: cloudharness.ToolCall{
				ID: cloudharness.ToolCallID(tool.CallID), Name: tool.Name, Arguments: string(tool.Arguments),
			}},
			DispatchTool: func(ctx context.Context, _ cloudharness.ToolWork) error {
				return w.executeTool(ctx, claim, token)
			},
		}, cloudharness.StepInput{Message: cloudharness.InputMessage{
			GroupID: cloudharness.ThreadID(threadID), Content: cloudharness.ResumeThread{},
		}})
	}
	return w.executeTextTurn(ctx, claim, token, delivery)
}

func stepClaimedTask(ctx context.Context, task cloudharness.ClaimedTask, input cloudharness.StepInput) error {
	system := cloudharness.NewClaimedAgentSystem(task)
	_, err := system.Step(ctx, []cloudharness.StepInput{input}, nil, nil)
	return err
}

func (w *Worker) acknowledge(ctx context.Context, message jetstream.Msg, ref db.HarnessTurnRef, state string) error {
	if err := message.DoubleAck(ctx); err != nil {
		return err
	}
	w.logger.InfoContext(ctx, "harness turn acknowledged", "turn_id", ref.UUID, "state", state)
	if state == "failed" {
		return errors.New("harness text turn failed")
	}
	return nil
}

func redeliver(message jetstream.Msg, errs ...error) error {
	return errors.Join(errNeedsRedelivery, errors.Join(errs...), message.NakWithDelay(retryDelay))
}
