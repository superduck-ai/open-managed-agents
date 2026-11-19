package channels

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (w *channelWorker) handleMessage(ctx context.Context, msg *channelMessage, scopeKey string) error {
	if msg == nil || msg.UserID == "" || msg.ChatID == "" || !w.active(ctx) {
		return nil
	}
	text := msg.Content
	if text == "" && len(msg.Resources) == 0 && (msg.Input == nil || msg.Input.State != "accepted") {
		if err := w.replyText(ctx, msg, "请发送文本、图片或文件。"); err != nil {
			return err
		}
		return w.completeInput(ctx, msg)
	}
	allowed, err := w.authorize(ctx, msg)
	if err != nil || !allowed {
		if err == nil {
			return w.completeInput(ctx, msg)
		}
		w.releaseInput(ctx, msg)
		return err
	}
	ticket := w.enqueueScope(scopeKey)
	parts := strings.Fields(text)
	if len(parts) > 0 && (parts[0] == "/new" || parts[0] == "/reset" || parts[0] == "/stop") {
		ticket.interrupted = w.interruptScope(scopeKey)
	}
	go w.runScoped(w.baseContext(), ticket, msg, text)
	return nil
}

func (w *channelWorker) runScoped(ctx context.Context, ticket *scopeTicket, msg *channelMessage, text string) {
	defer w.releaseInput(ctx, msg)
	if err := w.waitScope(ctx, ticket); err != nil {
		return
	}
	defer w.releaseScope(ticket)
	if ctx.Err() != nil || !w.active(ctx) {
		return
	}
	allowed, err := w.authorize(ctx, msg)
	if err != nil || !allowed {
		return
	}
	parts := strings.Fields(text)
	if len(parts) > 0 && slices.Contains([]string{"/help", "/resume", "/status", "/new", "/reset", "/stop"}, parts[0]) {
		if err := w.advanceInput(ctx, msg, db.ChannelInputAdvance{State: "command_sending"}); err != nil {
			return
		}
		w.runScopedCommand(ctx, ticket, msg, text)
		if err := w.completeInput(ctx, msg); err != nil {
			w.supervisor.logger.WarnContext(ctx, "complete channel command receipt", "channel_id", w.record.ExternalID, "error", err)
		}
		return
	}
	sessionID, err := w.inputSession(ctx, msg, ticket.scopeKey)
	if errors.Is(err, errChannelExecutionHeld) {
		if err := w.completeInput(ctx, msg); err != nil {
			w.supervisor.logger.WarnContext(ctx, "complete rejected channel input", "channel_id", w.record.ExternalID, "error", err)
		}
		return
	}
	if err != nil {
		w.supervisor.logger.WarnContext(ctx, "open channel session", "channel_id", w.record.ExternalID, "error", err)
		if errors.Is(err, errChannelSessionUnconfirmed) {
			_ = w.replyText(ctx, msg, "原会话状态尚未确认，本轮未提交，也未创建替代会话；请稍后重试或使用 /status 核对。")
		} else if errors.Is(err, db.ErrNotFound) {
			_ = w.replyText(ctx, msg, "当前渠道尚未配置执行环境，请在 OMA 渠道设置中完成配置。")
		} else {
			_ = w.replyText(ctx, msg, "OMA 会话创建失败，请稍后重试。")
		}
		return
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		return
	}
	w.stopListeners(ticket.scopeKey, sessionID)
	task := w.startReplyTask(ctx, ticket.scopeKey, sessionID)
	w.deliver(task, msg, apiKey)
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

func (w *channelWorker) stopAllReplies() []*channelReplyTask {
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
	return tasks
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

func (w *channelWorker) deliver(task *channelReplyTask, msg *channelMessage, apiKey string) {
	defer w.finishReply(task)
	defer w.closeInterruptedInput(task, msg)
	if !w.sessionBound(task.ctx, task) {
		return
	}
	if msg.Input != nil && msg.Input.State == "accepted" {
		if err := w.resumeAcceptedInput(task, msg, apiKey); err != nil && task.ctx.Err() == nil {
			w.supervisor.logger.WarnContext(task.ctx, "restore accepted channel input", "channel_id", w.record.ExternalID, "error", err)
		}
		return
	}
	foreign, err := w.checkInputHistory(task.ctx, apiKey, task.sessionID, msg.ChatID)
	if err != nil {
		_ = w.replyText(task.ctx, msg, "读取会话事件失败，本轮尚未提交，请稍后重试。")
		return
	}
	if foreign {
		_ = w.replyText(task.ctx, msg, channelForeignInputMessage)
		_ = w.completeInput(task.ctx, msg)
		return
	}
	baseline, err := w.resourceBaseline(task.ctx, apiKey, task.sessionID)
	if err != nil {
		w.supervisor.logger.WarnContext(task.ctx, "snapshot channel resources", "channel_id", w.record.ExternalID, "session_id", task.sessionID, "error", err)
		_ = w.replyText(task.ctx, msg, "会话资源读取失败，本轮尚未提交，请稍后重试。")
		return
	}
	baseline, err = w.captureArtifactBaseline(task.ctx, msg, task.sessionID, baseline)
	if err != nil {
		w.notify(task.ctx, msg, "保存文件交付基线失败，本轮尚未提交。")
		return
	}
	task.artifactBaseline = baseline
	content, mounted, err := w.inputContent(task.ctx, apiKey, task.sessionID, msg)
	if err != nil {
		_ = w.replyText(task.ctx, msg, "附件准备失败，本轮尚未提交："+err.Error())
		return
	}
	task.inbound = mounted
	if len(content) == 0 {
		_ = w.replyText(task.ctx, msg, "未取得可提交的消息内容，请重新发送。")
		_ = w.completeInput(task.ctx, msg)
		return
	}
	if !w.sessionBound(task.ctx, task) {
		return
	}
	var metadata *channelInputMetadata
	if msg.MessageID != "" {
		metadata = new(channelInputMetadata{ChannelID: w.record.ExternalID, Type: w.record.ChannelType, AccountID: w.record.AppID, MessageID: msg.MessageID, MessageIDs: msg.MessageIDs, ChatID: msg.ChatID, ActorID: msg.UserID})
	}
	ids, err := w.supervisor.oma.sendContent(task.ctx, apiKey, task.sessionID, content, metadata)
	if err != nil {
		w.supervisor.logger.WarnContext(task.ctx, "send channel message", "channel_id", w.record.ExternalID, "error", err)
		if errors.Is(err, errChannelInputUnconfirmed) {
			_ = w.replyText(task.ctx, msg, "消息提交结果尚未确认，已保留原会话，不会自动重发；请在 OMA 会话中核对。")
		} else {
			_ = w.replyText(task.ctx, msg, "消息发送失败，请稍后重试。")
		}
		return
	}
	if inputID := lastEventID(ids); inputID != "" {
		task.inputEventID = inputID
	}
	if _, err := w.prepareReplyDelivery(task.ctx, task, msg); err != nil {
		w.supervisor.logger.WarnContext(task.ctx, "prepare channel delivery handoff", "channel_id", w.record.ExternalID, "error", err)
		return
	}
	if err := w.advanceInput(task.ctx, msg, db.ChannelInputAdvance{State: "accepted", SessionExternalID: task.sessionID, InputEventID: task.inputEventID}); err != nil {
		w.supervisor.logger.WarnContext(task.ctx, "save channel input receipt", "channel_id", w.record.ExternalID, "error", err)
		return
	}
	if err := w.streamReply(task.ctx, task, msg, apiKey); err != nil && task.ctx.Err() == nil {
		w.supervisor.logger.WarnContext(task.ctx, "stream channel reply", "channel_id", w.record.ExternalID, "error", err)
		return
	}
	if task.normalEnd && w.sessionBound(task.ctx, task) {
		w.sendArtifacts(task.ctx, task, msg, apiKey)
	}
}

func lastEventID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[len(ids)-1]
}

func (w *channelWorker) interruptScope(scopeKey string) []*channelReplyTask {
	w.mu.Lock()
	tasks := append([]*channelReplyTask{}, w.replies[scopeKey]...)
	w.mu.Unlock()
	for _, task := range tasks {
		task.cancel()
	}
	return tasks
}

func (w *channelWorker) startListener(scopeKey, sessionID string) *channelReplyTask {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, task := range w.replies[scopeKey] {
		if task.sessionID == sessionID {
			return nil
		}
	}
	parent := w.ctx
	if parent == nil {
		parent = context.Background()
	}
	taskCtx, cancel := context.WithCancel(parent)
	task := &channelReplyTask{
		scopeKey:  scopeKey,
		sessionID: sessionID,
		listener:  true,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	task.ctx = taskCtx
	w.replies[scopeKey] = append(w.replies[scopeKey], task)
	return task
}

func (w *channelWorker) stopListeners(scopeKey, sessionID string) {
	w.mu.Lock()
	tasks := make([]*channelReplyTask, 0, 1)
	for _, task := range w.replies[scopeKey] {
		if task.listener && task.sessionID == sessionID {
			tasks = append(tasks, task)
		}
	}
	w.mu.Unlock()
	if len(tasks) == 0 {
		return
	}
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
