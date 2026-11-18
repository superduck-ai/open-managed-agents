package channels

import (
	"context"
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const selfPairingAction = "pair_identity"

func (w *channelWorker) promptSelfPairing(ctx context.Context, msg *channelMessage, scopeKey string) (bool, error) {
	if w.supervisor.ingress == nil || w.record.ChannelType != "feishu" || msg.ChatType != "p2p" {
		return false, nil
	}
	pairingID, err := w.supervisor.ingress.PrepareSelfPairing(ctx, channelIngress(w.record, msg, scopeKey))
	if err != nil || pairingID == "" {
		return false, err
	}
	if err := w.supervisor.store.HoldChannelInputForPairing(ctx, w.inputScope(), msg.Input.UUID, w.deliveryOwner, pairingID); err != nil {
		return true, err
	}
	stream, err := w.adapter.OpenReply(ctx, msg, contracts.Reply{
		Status: "绑定执行身份",
		Blocks: []contracts.ReplyBlock{{Text: "绑定后继续处理这条消息。"}},
		PairingValue: &channelActionValue{
			Kind: selfPairingAction, ChannelID: w.record.ExternalID, ScopeKey: scopeKey,
			OpenID: msg.UserID, PairingID: pairingID, SourceID: msg.MessageID,
		},
	})
	if err != nil {
		return true, err
	}
	return true, stream.Close(ctx)
}

func (w *channelWorker) confirmSelfPairing(ctx context.Context, event *contracts.ActionEvent, value channelActionValue) error {
	if w.supervisor.ingress == nil || value.ChannelID != w.record.ExternalID || value.PairingID == "" || value.SourceID == "" ||
		event.OpenID == "" || value.OpenID != event.OpenID || event.ChatID == "" ||
		value.ScopeKey != "chat:"+event.ChatID || event.MessageID == "" {
		return invalidChannelCardReceipt()
	}
	record, err := w.supervisor.store.GetChannelRecord(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, w.record.AgentExternalID, w.record.ExternalID)
	if err != nil {
		return err
	}
	if !record.Enabled || record.DeletedAt != nil || record.BindingStatus != "bound" || record.AppID != w.record.AppID || record.ChannelType != "feishu" {
		return invalidChannelCardReceipt()
	}
	msg := &channelMessage{ChatID: event.ChatID, ChatType: "p2p", UserID: event.OpenID}
	decision, err := w.admission(ctx, msg)
	if err != nil {
		return err
	}
	if decision != pairingApproved {
		w.notifyChat(ctx, event.ChatID, "当前私聊未获渠道授权，无法绑定执行身份。")
		return nil
	}
	var version *int
	if fixed := parseChannelMounts(record.Mounts).AgentVersion; fixed > 0 {
		version = &fixed
	}
	err = w.supervisor.ingress.ConfirmSelfPairing(ctx, channelIngress(record, msg, value.ScopeKey), value.PairingID, version)
	if public, ok := errors.AsType[*apperr.Error](err); ok {
		return w.replyText(ctx, msg, public.PublicMessage)
	}
	if err != nil {
		return err
	}
	id, resumed, err := w.supervisor.store.ResumeChannelInputAfterPairing(ctx, w.inputScope(), db.ChannelPairingResume{
		MessageID: value.SourceID, ScopeKey: value.ScopeKey, ActorID: event.OpenID, PairingID: value.PairingID,
	})
	if err != nil {
		return err
	}
	if resumed {
		if err := w.claimAndDispatchInput(ctx, id); err != nil {
			return err
		}
	}
	return w.adapter.ResumeReply(event.MessageID).Update(ctx, contracts.Reply{
		Status: "绑定成功", Ended: true,
		Blocks: []contracts.ReplyBlock{{Text: "已继续处理刚才的消息。"}},
	})
}
