package channels

import (
	"context"
	"encoding/json/v2"
	"errors"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type durableReplyStream struct {
	worker *channelWorker
	task   *channelReplyTask
	stream channelReplyStream
}

func (s *durableReplyStream) Receipt() string { return s.stream.Receipt() }
func (s *durableReplyStream) Update(ctx context.Context, reply contracts.Reply) error {
	if err := s.worker.renewReplyDelivery(ctx, s.task); err != nil {
		return err
	}
	return s.stream.Update(ctx, reply)
}
func (s *durableReplyStream) Close(ctx context.Context) error {
	if err := s.stream.Close(ctx); err != nil {
		return err
	}
	if s.task.terminal {
		var artifacts channelArtifactDelivery
		if s.task.delivery.ArtifactDelivery != "" {
			if err := json.Unmarshal([]byte(s.task.delivery.ArtifactDelivery), &artifacts); err != nil {
				return err
			}
		}
		if s.task.normalEnd && artifacts.Baseline != nil {
			return nil
		}
		return s.worker.supervisor.store.CompleteChannelReplyDelivery(ctx, s.worker.deliveryScope(), s.task.inputEventID, s.worker.deliveryOwner)
	}
	if s.worker.baseContext().Err() != nil {
		return s.worker.supervisor.store.ReleaseChannelReplyDelivery(ctx, s.worker.deliveryScope(), s.task.inputEventID, s.worker.deliveryOwner)
	}
	return nil
}

func (w *channelWorker) deliveryScope() db.ChannelDeliveryScope {
	return db.ChannelDeliveryScope{OrganizationUUID: w.record.OrganizationUUID, WorkspaceUUID: w.record.WorkspaceUUID, ChannelExternalID: w.record.ExternalID}
}

func (w *channelWorker) renewReplyDelivery(ctx context.Context, task *channelReplyTask) error {
	err := w.supervisor.store.RenewChannelReplyDelivery(ctx, w.deliveryScope(), task.inputEventID, w.deliveryOwner)
	if err == nil {
		task.deliveryRenewedAt = time.Now()
	}
	return err
}

func (w *channelWorker) openReply(ctx context.Context, task *channelReplyTask, msg *channelMessage) (channelReplyStream, error) {
	if task.inputEventID == "" {
		return w.adapter.OpenReply(ctx, msg)
	}
	scope := w.deliveryScope()
	record, err := w.prepareReplyDelivery(ctx, task, msg)
	if err != nil {
		return nil, err
	}
	claimed, ok, err := w.supervisor.store.ClaimChannelReplyDelivery(ctx, scope, record.InputEventID, w.deliveryOwner)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errChannelDeliveryUnconfirmed
	}
	task.delivery = &claimed
	var stream channelReplyStream
	if claimed.State == "sent" {
		stream = w.adapter.ResumeReply(claimed.ReplyMessageID)
	} else {
		stream, err = w.adapter.OpenReply(ctx, msg)
		if err != nil {
			return nil, errors.Join(errChannelDeliveryUnconfirmed, err, w.supervisor.store.RetireChannelReplyDelivery(ctx, scope, task.inputEventID, w.deliveryOwner))
		}
		if stream.Receipt() == "" {
			return nil, errors.Join(errChannelDeliveryUnconfirmed, w.supervisor.store.RetireChannelReplyDelivery(ctx, scope, task.inputEventID, w.deliveryOwner))
		}
		if err := w.supervisor.store.SaveChannelReplyReceipt(ctx, scope, task.inputEventID, w.deliveryOwner, stream.Receipt()); err != nil {
			return nil, err
		}
		task.delivery.ReplyMessageID = stream.Receipt()
	}
	if err := w.completeInput(ctx, msg); err != nil {
		return nil, err
	}
	task.deliveryRenewedAt = time.Now()
	return &durableReplyStream{worker: w, task: task, stream: stream}, nil
}

func (w *channelWorker) prepareReplyDelivery(ctx context.Context, task *channelReplyTask, msg *channelMessage) (db.ChannelReplyDelivery, error) {
	artifacts, err := json.Marshal(channelArtifactDelivery{Baseline: task.artifactBaseline}, json.FormatNilMapAsNull(true))
	if err != nil {
		return db.ChannelReplyDelivery{}, err
	}
	return w.supervisor.store.PrepareChannelReplyDelivery(ctx, w.deliveryScope(), db.ChannelReplyDelivery{
		ArtifactDelivery:  string(artifacts),
		ChannelExternalID: w.record.ExternalID, ChannelType: w.record.ChannelType, AccountID: w.record.AppID, InputEventID: task.inputEventID, SessionExternalID: task.sessionID, ScopeKey: task.scopeKey,
		MessageID: msg.MessageID, ChatID: msg.ChatID, ChatType: msg.ChatType, ActorID: msg.UserID, RootID: msg.RootID, ThreadID: msg.ThreadID,
		IncludeThinking: w.record.IncludeThinking, IncludeToolCalls: w.record.IncludeToolCalls,
	})
}

func (w *channelWorker) restoreReplies(ctx context.Context) {
	if !w.online.Load() || !w.active(ctx) {
		return
	}
	rows, err := w.supervisor.store.ListChannelReplyDeliveries(ctx, w.deliveryScope())
	if err != nil {
		w.supervisor.logger.WarnContext(ctx, "read channel reply receipts", "channel_id", w.record.ExternalID, "error", err)
		return
	}
	for _, row := range rows {
		sessionID, bindErr := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, row.ScopeKey)
		if bindErr != nil && !errors.Is(bindErr, db.ErrNotFound) {
			continue
		}
		if row.State == "sending" || row.ChannelType != w.record.ChannelType || row.AccountID != w.record.AppID || sessionID != row.SessionExternalID {
			if err := w.supervisor.store.RetireChannelReplyDelivery(ctx, w.deliveryScope(), row.InputEventID, w.deliveryOwner); err != nil {
				w.supervisor.logger.WarnContext(ctx, "retire channel reply", "channel_id", w.record.ExternalID, "error", err)
			}
			continue
		}
		w.resumeReplyDelivery(row)
	}
}

func (w *channelWorker) resumeReplyDelivery(row db.ChannelReplyDelivery) {
	task := w.startListener(row.ScopeKey, row.SessionExternalID)
	if task == nil {
		return
	}
	task.listener = false
	task.inputEventID = row.InputEventID
	task.delivery = &row
	msg := &channelMessage{MessageID: row.MessageID, ChatID: row.ChatID, ChatType: row.ChatType, UserID: row.ActorID, RootID: row.RootID, ThreadID: row.ThreadID}
	go func() {
		defer w.finishReply(task)
		key, err := w.supervisor.apiKey(task.ctx, w.record)
		if err != nil {
			return
		}
		if err := w.streamReply(task.ctx, task, msg, key); err != nil && task.ctx.Err() == nil {
			w.supervisor.logger.WarnContext(task.ctx, "restore channel reply", "channel_id", w.record.ExternalID, "session_id", task.sessionID, "error", err)
			return
		}
		if task.normalEnd && w.sessionBound(task.ctx, task) {
			w.sendArtifacts(task.ctx, task, msg, key)
		}
	}()
}
