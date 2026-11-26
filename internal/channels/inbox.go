package channels

import (
	"context"
	"encoding/json/v2"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
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
	msg.Input = &row
	if row.State == "buffering" {
		if msg.PairingCard != "" {
			if err := w.adapter.ResumeReply(msg.PairingCard).Update(ctx, contracts.Reply{Status: "已连接", Blocks: []contracts.ReplyBlock{{Text: "已连接"}}, Ended: true}); err != nil {
				w.supervisor.logger.WarnContext(ctx, "更新配对授权卡失败", "input_uuid", row.UUID, "error", err)
			}
		}
		if msg.ChatType == "p2p" && msg.PairingID == "" && parseChannelMounts(w.record.Mounts).PairingProvider != "" {
			held, err := w.supervisor.pairingPrompt(ctx, w, &msg)
			if err != nil || held {
				return err
			}
		}
		if adapter, ok := w.adapter.(interface {
			QueueInput(context.Context, *channelMessage) error
		}); ok {
			return adapter.QueueInput(ctx, &msg)
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
	msg.Input = &row
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
	if msg.Input == nil {
		return nil
	}
	row, err := w.supervisor.store.AdvanceChannelInput(ctx, w.inputScope(), msg.Input.UUID, w.deliveryOwner, change)
	if err == nil {
		msg.Input = &row
	}
	return err
}

func (w *channelWorker) completeInput(ctx context.Context, msg *channelMessage) error {
	if msg.Input == nil || msg.Input.State == "handed_off" {
		return nil
	}
	err := w.supervisor.store.CompleteChannelInput(ctx, w.inputScope(), msg.Input.UUID, w.deliveryOwner)
	if err == nil {
		msg.Input.State = "handed_off"
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
	if msg.Input == nil || msg.Input.State == "handed_off" || ctx.Err() != nil {
		return
	}
	if err := w.supervisor.store.ReleaseChannelInput(ctx, w.inputScope(), msg.Input.UUID, w.deliveryOwner); err != nil {
		w.supervisor.logger.WarnContext(ctx, "release channel input", "channel_id", w.record.ExternalID, "input_uuid", msg.Input.UUID, "error", err)
	}
}
