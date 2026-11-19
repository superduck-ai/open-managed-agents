package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/sessions"
)

type replyRunner struct {
	worker       *channelWorker
	presentation db.ChannelRecord
	task         *channelReplyTask
	msg          *channelMessage
	reply        channelReply
	stream       channelReplyStream
	seen         map[string]struct{}
	closed       bool
	oauthCards   map[string]string
	previewDirty bool
	lastUpdate   time.Time
}

func newReplyRunner(worker *channelWorker, task *channelReplyTask, msg *channelMessage) *replyRunner {
	value := worker.cardValue(task, task.inputEventID, "stop", msg.UserID, "")
	value.ChatType = msg.ChatType
	presentation := worker.record
	if task.delivery != nil {
		presentation.IncludeThinking = task.delivery.IncludeThinking
		presentation.IncludeToolCalls = task.delivery.IncludeToolCalls
	}
	return &replyRunner{
		worker:       worker,
		presentation: presentation,
		task:         task,
		msg:          msg,
		reply:        channelReply{thinking: presentation.IncludeThinking, stopValue: &value, oauthOrigin: worker.supervisor.oauthOrigin},
		seen:         make(map[string]struct{}),
	}
}

func (w *channelWorker) streamReply(ctx context.Context, task *channelReplyTask, msg *channelMessage, apiKey string) (resultErr error) {
	runner := newReplyRunner(w, task, msg)
	task.runner = runner
	defer func() { resultErr = errors.Join(resultErr, runner.close(ctx)) }()
	if !task.listener {
		if err := runner.open(ctx); err != nil {
			return err
		}
	}
	if !w.sessionBound(ctx, task) {
		return nil
	}
	if w.supervisor.subscribeEvents == nil {
		return errChannelSubscriptionUnavailable
	}
	cursor := task.inputEventID
	waitForInput := cursor != "" && !task.listener
	for {
		if !w.sessionBound(ctx, task) {
			return nil
		}
		sub, err := w.supervisor.subscribeEvents(ctx, apiKey, task.sessionID, sessions.StreamOptions{AfterEventID: cursor, WaitForInput: waitForInput, DeltaTypes: map[string]struct{}{"agent.message": {}, "agent.thinking": {}}})
		if err != nil {
			return err
		}
		done, nextCursor, started, err := runner.receiveSubscription(ctx, sub, cursor, waitForInput)
		sub.Close()
		cursor, waitForInput = nextCursor, !started && waitForInput
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.supervisor.shutdown:
			return nil
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (r *replyRunner) receiveSubscription(ctx context.Context, sub SessionEventSubscription, cursor string, waitForInput bool) (bool, string, bool, error) {
	started := !waitForInput
	ticker := time.NewTicker(channelPreviewUpdateInterval)
	defer ticker.Stop()
	lastMaintenance := time.Now()
	for {
		event, err := sub.ReceiveUntil(ctx, ticker.C)
		if errors.Is(err, sessions.ErrStreamHeartbeat) {
			if r.previewDirty && r.stream != nil {
				if err := r.update(ctx); err != nil {
					return false, cursor, started, err
				}
			}
			if time.Since(lastMaintenance) < channelReplyMaintenanceInterval {
				continue
			}
			lastMaintenance = time.Now()
			if !r.worker.sessionBound(ctx, r.task) {
				return true, cursor, started, nil
			}
			if r.task.delivery != nil && time.Since(r.task.deliveryRenewedAt) > 10*time.Second {
				if err := r.worker.renewReplyDelivery(ctx, r.task); err != nil {
					return false, cursor, started, err
				}
			}
			select {
			case <-r.worker.supervisor.shutdown:
				return true, cursor, started, nil
			default:
			}
			continue
		}
		if errors.Is(err, sessions.ErrStreamReset) || errors.Is(err, io.EOF) {
			r.resetPreviews()
			return false, cursor, started, nil
		}
		if err != nil {
			return false, cursor, started, err
		}
		if event.EventType == "event_start" || event.EventType == "event_delta" {
			if err := r.consumePreview(ctx, event); err != nil {
				return false, cursor, started, err
			}
			continue
		}
		var value omaEvent
		if err := json.Unmarshal(event.Payload, &value); err != nil {
			return false, cursor, started, err
		}
		value.ID, value.Type = event.ExternalID, event.EventType
		value.ProcessedAt = []byte("null")
		if !event.ProcessedAt.IsZero() {
			value.ProcessedAt, err = json.Marshal(event.ProcessedAt)
			if err != nil {
				return false, cursor, started, err
			}
		}
		if value.ID == r.task.inputEventID {
			started = true
			continue
		}
		done, err := r.consumeEvent(ctx, value)
		if err != nil || done {
			return done, cursor, started, err
		}
		if value.Type != "user.message" || !event.ProcessedAt.IsZero() {
			cursor = value.ID
		}
	}
}

func (r *replyRunner) open(ctx context.Context) error {
	if r.stream != nil {
		return nil
	}
	opened, err := r.worker.openReply(ctx, r.task, r.msg)
	if err != nil {
		return err
	}
	r.stream = opened
	if r.task.delivery != nil && r.task.delivery.ArtifactDelivery != "" {
		var receipts channelArtifactDelivery
		if err := json.Unmarshal([]byte(r.task.delivery.ArtifactDelivery), &receipts); err != nil {
			return err
		}
		r.oauthCards = receipts.OAuthCards
	}
	if r.task.delivery != nil {
		r.presentation.IncludeThinking = r.task.delivery.IncludeThinking
		r.presentation.IncludeToolCalls = r.task.delivery.IncludeToolCalls
	}
	return r.update(ctx)
}

func (r *replyRunner) close(ctx context.Context) error {
	if r.stream == nil || r.closed {
		return nil
	}
	r.closed = true
	return r.stream.Close(context.WithoutCancel(ctx))
}

func (r *replyRunner) consumeEvent(ctx context.Context, event omaEvent) (bool, error) {
	if r.task.inputEventID != "" && event.Type == "user.message" {
		if event.ID == r.task.inputEventID {
			return false, nil
		}
		if !channelOwnsInput(r.worker.record, r.msg.ChatID, event) {
			r.task.terminal = true
			r.task.normalEnd = false
			r.reply.status = "已暂停"
			r.reply.finish(channelForeignInputMessage)
			if r.stream == nil {
				return true, r.open(ctx)
			}
			return true, r.update(ctx)
		}
		if len(event.ProcessedAt) == 0 || string(event.ProcessedAt) == "null" {
			return false, nil
		}
		r.task.terminal = true
		r.task.normalEnd = !r.reply.interrupted
		r.reply.status = "已完成"
		if r.reply.interrupted {
			r.reply.status = "已停止"
		}
		r.reply.finish("")
		if r.stream != nil {
			return true, r.update(ctx)
		}
		return true, nil
	}
	if _, ok := r.seen[event.ID]; ok {
		return false, nil
	}
	r.seen[event.ID] = struct{}{}
	if terminalEvent(event) {
		r.task.terminal = true
	}
	if err := r.write(ctx, event); err != nil {
		return false, err
	}
	if terminalEvent(event) {
		task := r.task
		task.normalEnd = event.Type == "session.status_idle" && stopReason(event) == "end_turn" && !r.reply.interrupted
		return true, nil
	}
	return false, nil
}

func (r *replyRunner) write(ctx context.Context, event omaEvent) error {
	switch event.Type {
	case "agent.tool_use", "agent.mcp_tool_use", "agent.custom_tool_use":
		r.task.usedTools = true
	}
	if err := r.settleOAuthCards(ctx, event); err != nil {
		return err
	}
	_, changed := r.reply.consume(r.presentation, event)
	if !changed {
		return nil
	}
	if err := r.open(ctx); err != nil {
		return err
	}
	return r.update(ctx)
}

func (r *replyRunner) update(ctx context.Context) error {
	for attempt := range channelAskAttempts {
		view := r.reply.view()
		if err := r.detachFridayCard(ctx, &view); err != nil {
			return err
		}
		err := r.stream.Update(ctx, view)
		if err == nil {
			r.lastUpdate = time.Now()
			r.previewDirty = false
			return nil
		}
		if attempt == channelAskAttempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(channelAskRetryBase * time.Duration(1<<attempt)):
		}
	}
	return nil
}

func terminalEvent(event omaEvent) bool {
	switch event.Type {
	case "session.status_idle":
		reason := stopReason(event)
		return reason == "end_turn" || reason == "interrupted" || reason == "retries_exhausted"
	case "session.status_terminated", "session.error":
		return true
	default:
		return false
	}
}

func (r *replyRunner) consumeHistory(ctx context.Context, events []omaEvent) (bool, error) {
	started := r.task.inputEventID == ""
	for _, event := range events {
		if !started {
			if event.ID == r.task.inputEventID && string(event.ProcessedAt) != "null" {
				started = true
			}
			continue
		}
		done, err := r.consumeEvent(ctx, event)
		if err != nil || done {
			return done, err
		}
	}
	return false, nil
}
