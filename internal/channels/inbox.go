package channels

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (w *channelWorker) inputScope() db.ChannelInputScope {
	return db.ChannelInputScope{OrganizationUUID: w.record.OrganizationUUID, WorkspaceUUID: w.record.WorkspaceUUID, ChannelUUID: w.record.UUID, ChannelType: w.record.ChannelType, AccountID: w.record.AppID}
}

func (w *channelWorker) receiveChannelMessage(ctx context.Context, msg *channelMessage) error {
	if msg.MessageID == "" || msg.ChatID == "" || msg.UserID == "" {
		return errors.New("渠道消息缺少消息、聊天或操作者标识")
	}
	if len(msg.MessageIDs) == 0 {
		msg.MessageIDs = []string{msg.MessageID}
	}
	scopeKey, err := w.messageScopeKey(ctx, msg)
	if err != nil {
		return err
	}
	allowed, err := w.authorize(ctx, msg)
	if err != nil || !allowed {
		return err
	}
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	row, fresh, err := w.supervisor.store.ReceiveChannelInput(ctx, w.inputScope(), db.ChannelInput{MessageID: msg.MessageID, ScopeKey: scopeKey, Payload: string(payload)})
	if err != nil || !fresh {
		return err
	}
	return w.claimAndDispatchInput(ctx, row.UUID)
}

func (w *channelWorker) claimAndDispatchInput(ctx context.Context, inputUUID string) error {
	row, claimed, err := w.supervisor.store.ClaimChannelInput(ctx, w.inputScope(), inputUUID, w.deliveryOwner)
	if err != nil || !claimed {
		return err
	}
	var msg channelMessage
	if err := json.Unmarshal([]byte(row.Payload), &msg); err != nil {
		return err
	}
	msg.input = &row
	if row.State == "buffering" {
		if adapter, ok := w.adapter.(interface {
			queueInput(context.Context, *channelMessage) error
		}); ok {
			return adapter.queueInput(ctx, &msg)
		}
		return w.prepareInputMessage(ctx, &msg, row.ScopeKey)
	}
	return w.handleMessage(ctx, &msg, row.ScopeKey)
}

func (w *channelWorker) prepareInputMessage(ctx context.Context, msg *channelMessage, scopeKey string) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	row, err := w.supervisor.store.PrepareChannelInputBatch(ctx, w.inputScope(), w.deliveryOwner, db.ChannelInputBatch{MessageIDs: msg.MessageIDs, ScopeKey: scopeKey, Payload: string(payload)})
	if err != nil {
		return err
	}
	msg.input = &row
	return w.handleMessage(ctx, msg, scopeKey)
}

func (w *channelWorker) restoreInputs(ctx context.Context) {
	rows, err := w.supervisor.store.ListRecoverableChannelInputs(ctx, w.inputScope())
	if err != nil {
		w.supervisor.logger.WarnContext(ctx, "read pending channel inputs", "channel_id", w.record.ExternalID, "error", err)
		return
	}
	for _, row := range rows {
		if err := w.claimAndDispatchInput(ctx, row.UUID); err != nil && ctx.Err() == nil {
			w.supervisor.logger.WarnContext(ctx, "restore channel input", "channel_id", w.record.ExternalID, "input_uuid", row.UUID, "error", err)
		}
	}
}

func (w *channelWorker) advanceInput(ctx context.Context, msg *channelMessage, change db.ChannelInputAdvance) error {
	if msg.input == nil {
		return nil
	}
	row, err := w.supervisor.store.AdvanceChannelInput(ctx, w.inputScope(), msg.input.UUID, w.deliveryOwner, change)
	if err == nil {
		msg.input = &row
	}
	return err
}

func (w *channelWorker) completeInput(ctx context.Context, msg *channelMessage) error {
	if msg.input == nil || msg.input.State == "handed_off" {
		return nil
	}
	err := w.supervisor.store.CompleteChannelInput(ctx, w.inputScope(), msg.input.UUID, w.deliveryOwner)
	if err == nil {
		msg.input.State = "handed_off"
	}
	return err
}

func (w *channelWorker) closeInterruptedInput(task *channelReplyTask, msg *channelMessage) {
	if task.ctx.Err() == nil || w.baseContext().Err() != nil {
		return
	}
	if err := w.completeInput(w.baseContext(), msg); err != nil {
		w.supervisor.logger.WarnContext(w.baseContext(), "close interrupted channel input", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) releaseInput(ctx context.Context, msg *channelMessage) {
	if msg.input == nil || msg.input.State == "handed_off" || ctx.Err() != nil {
		return
	}
	if err := w.supervisor.store.ReleaseChannelInput(ctx, w.inputScope(), msg.input.UUID, w.deliveryOwner); err != nil {
		w.supervisor.logger.WarnContext(ctx, "release channel input", "channel_id", w.record.ExternalID, "input_uuid", msg.input.UUID, "error", err)
	}
}
