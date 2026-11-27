package channels

import (
	"cmp"
	"context"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (w *channelWorker) messageScopeKey(ctx context.Context, msg *channelMessage) (string, error) {
	if msg.ChatType == "p2p" {
		if !w.record.DirectInThread {
			return "chat:" + msg.ChatID, nil
		}
		if msg.ThreadID != "" {
			if msg.RootID == "" {
				return "", errors.New("飞书单聊话题缺少根消息 ID")
			}
			return "thread:" + msg.ChatID + ":" + msg.RootID, nil
		}
		if isChannelCommand(msg.Content) {
			return "chat:" + msg.ChatID, nil
		}
		return "thread:" + msg.ChatID + ":" + msg.MessageID, nil
	}
	if msg.ChatType == "group" && !w.record.ReplyInThread {
		return "chat:" + msg.ChatID, nil
	}
	scopeKey := channelScopeKey(msg)
	legacy := w.legacyGroupScope(msg)
	if legacy == "" || legacy == scopeKey {
		return scopeKey, nil
	}
	_, err := w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey)
	if err == nil {
		return scopeKey, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return "", err
	}
	_, err = w.supervisor.store.GetChannelSessionBinding(ctx, w.record.ExternalID, legacy)
	if err == nil {
		return legacy, nil
	}
	if !errors.Is(err, db.ErrNotFound) {
		return "", err
	}
	return scopeKey, nil
}

func isDirectTopicScope(chatType, scopeKey string) bool {
	return chatType == "p2p" && strings.HasPrefix(scopeKey, "thread:")
}

func (w *channelWorker) legacyGroupScope(msg *channelMessage) string {
	if msg.ChatType != "group" || !w.record.ReplyInThread {
		return ""
	}
	if threadID := msg.ThreadID; threadID != "" {
		return "thread:" + msg.ChatID + ":" + threadID
	}
	return ""
}

func channelScopeKey(msg *channelMessage) string {
	if msg.ChatType == "group" {
		if root := cmp.Or(msg.RootID, msg.ThreadID, msg.MessageID); root != "" {
			return "thread:" + msg.ChatID + ":" + root
		}
	}
	if threadID := msg.ThreadID; threadID != "" {
		return "thread:" + msg.ChatID + ":" + threadID
	}
	return "chat:" + msg.ChatID
}
