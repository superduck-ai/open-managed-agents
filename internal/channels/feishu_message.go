package channels

import (
	"context"
	"errors"
	"strings"
	"time"

	channeltypes "github.com/larksuite/oapi-sdk-go/v3/channel/types"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (w *channelWorker) handleMessage(ctx context.Context, msg *channeltypes.NormalizedMessage) error {
	if msg == nil || msg.UserID == "" || msg.ChatID == "" {
		return nil
	}
	if !w.active(ctx) {
		return nil
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" && len(msg.Resources) == 0 {
		return w.replyText(ctx, msg, "请发送文本、图片或 PDF 附件。")
	}
	scopeKey := channelScopeKey(msg)
	switch text {
	case "/new", "/stop":
		go w.runCommand(w.baseContext(), scopeKey, msg, text)
		return nil
	}
	allowed, err := w.authorize(ctx, msg)
	if err != nil || !allowed {
		return err
	}
	sessionID, err := w.sessionFor(ctx, msg, scopeKey)
	if err != nil {
		w.supervisor.logger.WarnContext(ctx, "open channel session", "channel_id", w.record.ExternalID, "error", err)
		if errors.Is(err, db.ErrNotFound) {
			return w.replyText(ctx, msg, "当前渠道尚未配置执行环境，请在 OMA 渠道设置中完成配置。")
		}
		return w.replyText(ctx, msg, "OMA 会话创建失败，请稍后重试。")
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return err
	}
	task := w.startReplyTask(w.baseContext(), scopeKey, sessionID)
	go w.deliver(task, msg, apiKey)
	return nil
}

func (w *channelWorker) baseContext() context.Context {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ctx != nil {
		return w.ctx
	}
	return context.Background()
}

func (w *channelWorker) startReplyTask(ctx context.Context, scopeKey, sessionID string) *channelReplyTask {
	taskCtx, cancel := context.WithCancel(ctx)
	task := &channelReplyTask{
		scopeKey:  scopeKey,
		sessionID: sessionID,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	task.ctx = taskCtx
	w.mu.Lock()
	w.replies[scopeKey] = append(w.replies[scopeKey], task)
	w.mu.Unlock()
	return task
}

func (w *channelWorker) stopAllReplies() {
	w.mu.Lock()
	tasks := make([]*channelReplyTask, 0, len(w.replies))
	for _, scoped := range w.replies {
		tasks = append(tasks, scoped...)
	}
	w.replies = make(map[string][]*channelReplyTask)
	w.mu.Unlock()
	for _, task := range tasks {
		task.cancel()
	}
}

func (w *channelWorker) finishReply(task *channelReplyTask) {
	task.cancel()
	w.mu.Lock()
	tasks := w.replies[task.scopeKey]
	kept := tasks[:0]
	for _, item := range tasks {
		if item != task {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		delete(w.replies, task.scopeKey)
	} else {
		w.replies[task.scopeKey] = kept
	}
	w.mu.Unlock()
	close(task.done)
}

func (w *channelWorker) cancelReplies(scopeKey string) {
	w.mu.Lock()
	tasks := append([]*channelReplyTask{}, w.replies[scopeKey]...)
	w.mu.Unlock()
	for _, task := range tasks {
		task.cancel()
	}
	deadline := time.After(3 * time.Second)
	for _, task := range tasks {
		select {
		case <-task.done:
		case <-deadline:
			return
		}
	}
}

func (w *channelWorker) deliver(task *channelReplyTask, msg *channeltypes.NormalizedMessage, apiKey string) {
	defer w.finishReply(task)
	if task.ctx.Err() != nil {
		return
	}
	content, skipped, err := w.messageContent(task.ctx, msg, strings.TrimSpace(msg.Content))
	if err != nil {
		_ = w.replyText(task.ctx, msg, "附件读取失败，请重新发送："+err.Error())
		return
	}
	if len(content) == 0 {
		_ = w.replyText(task.ctx, msg, "附件格式暂不支持，目前仅支持图片、PDF 与纯文本文件。")
		return
	}
	if len(skipped) > 0 {
		_, _ = w.channel.Send(task.ctx, &channeltypes.SendInput{ChatID: msg.ChatID, Markdown: "以下附件暂不支持，已跳过：" + strings.Join(skipped, "、")})
	}
	ids, err := w.supervisor.oma.sendContent(task.ctx, apiKey, task.sessionID, content)
	if err != nil {
		w.supervisor.logger.WarnContext(task.ctx, "send channel message", "channel_id", w.record.ExternalID, "error", err)
		_ = w.replyText(task.ctx, msg, "消息发送失败，请稍后重试。")
		return
	}
	task.inputEventID = lastEventID(ids)
	if task.inputEventID == "" {
		events, listErr := w.supervisor.oma.listEvents(task.ctx, apiKey, task.sessionID)
		if listErr == nil {
			task.inputEventID = lastEventID(eventIDs(events))
		}
	}
	if err := w.streamReply(task.ctx, task, msg, apiKey); err != nil && task.ctx.Err() == nil {
		w.supervisor.logger.WarnContext(task.ctx, "stream channel reply", "channel_id", w.record.ExternalID, "error", err)
	}
}

func (w *channelWorker) runCommand(ctx context.Context, scopeKey string, msg *channeltypes.NormalizedMessage, command string) {
	if command == "/new" {
		w.cancelReplies(scopeKey)
		if sessionID := w.currentSession(ctx, scopeKey); sessionID != "" {
			if apiKey, err := w.supervisor.apiKey(ctx, w.record); err == nil {
				if err := w.supervisor.oma.interrupt(ctx, apiKey, sessionID); err != nil {
					w.supervisor.logger.DebugContext(ctx, "interrupt channel session", "channel_id", w.record.ExternalID, "error", err)
				}
			}
		}
		if err := w.supervisor.store.DeleteChannelSessionBinding(ctx, w.record.ExternalID, scopeKey); err != nil {
			w.supervisor.logger.WarnContext(ctx, "reset channel session", "channel_id", w.record.ExternalID, "error", err)
			_ = w.replyText(ctx, msg, "开启新会话失败，请稍后重试。")
			return
		}
		_ = w.replyText(ctx, msg, "已开启新会话，下一条消息会创建新的会话。")
		return
	}
	sessionID := w.currentSession(ctx, scopeKey)
	if sessionID == "" {
		_ = w.replyText(ctx, msg, "当前没有进行中的会话。")
		return
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return
	}
	if err := w.supervisor.oma.interrupt(ctx, apiKey, sessionID); err != nil {
		_ = w.replyText(ctx, msg, "停止失败，请稍后重试。")
		return
	}
	_ = w.replyText(ctx, msg, "已请求停止当前任务。")
}

func lastEventID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
}

func eventIDs(events []omaEvent) []string {
	ids := make([]string, 0, len(events))
	for _, event := range events {
		ids = append(ids, event.ID)
	}
	return ids
}
