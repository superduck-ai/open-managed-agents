package channels

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/identities"
)

func (w *channelWorker) runCommand(ctx context.Context, scopeKey string, msg *channelMessage, command string) bool {
	return w.runScopedCommand(ctx, &scopeTicket{scopeKey: scopeKey}, msg, command)
}

func (w *channelWorker) runScopedCommand(ctx context.Context, ticket *scopeTicket, msg *channelMessage, command string) bool {
	scopeKey := ticket.scopeKey
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "/help":
		_ = w.replyText(ctx, msg, "支持文字、图片与文件对话。\n/new 或 /reset：新建会话\n/status：当前会话状态\n/resume：列出本话题历史会话\n/resume 会话ID：恢复历史会话\n/stop：停止当前任务")
		return true
	case "/resume":
		w.resumeCommand(ctx, scopeKey, msg, parts[1:])
		return true
	case "/status":
		w.statusCommand(ctx, scopeKey, msg)
		return true
	case "/new", "/reset", "/stop":
	default:
		return false
	}
	sessionID := w.currentSession(ctx, scopeKey)
	if sessionID == "" && parts[0] == "/stop" {
		_ = w.replyText(ctx, msg, "当前没有进行中的会话。")
		return true
	}
	var apiKey string
	if sessionID != "" {
		var err error
		apiKey, err = w.supervisor.apiKey(ctx, w.record)
		if err != nil {
			_ = w.replyText(ctx, msg, "无法读取会话凭据，请稍后重试。")
			return true
		}
		if err := w.supervisor.oma.interruptAndWait(ctx, apiKey, sessionID); err != nil {
			w.finishInterruptedReplies(ticket.interrupted, apiKey, false)
			w.supervisor.logger.WarnContext(ctx, "confirm channel session stop", "channel_id", w.record.ExternalID, "session_id", sessionID, "error", err)
			_ = w.replyText(w.baseContext(), msg, "远端停止状态未确认，保留当前会话，请稍后用 /status 核对。")
			return true
		}
	}
	w.finishInterruptedReplies(ticket.interrupted, apiKey, true)
	if parts[0] == "/stop" {
		_ = w.replyText(ctx, msg, "当前任务已停止。")
		return true
	}
	w.cancelReplies(scopeKey)
	if err := w.supervisor.store.DeleteChannelSessionBinding(ctx, w.record.ExternalID, scopeKey); err != nil {
		w.supervisor.logger.WarnContext(ctx, "reset channel session", "channel_id", w.record.ExternalID, "error", err)
		_ = w.replyText(ctx, msg, "开启新会话失败，请稍后重试。")
		return true
	}
	_ = w.replyText(ctx, msg, "已开启新会话，下一条消息会创建新的会话。")
	return true
}

func (w *channelWorker) statusCommand(ctx context.Context, scopeKey string, msg *channelMessage) {
	sessionID := w.currentSession(ctx, scopeKey)
	if sessionID == "" {
		_ = w.replyText(ctx, msg, "当前尚未创建会话。")
		return
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		_ = w.replyText(ctx, msg, "读取会话状态失败，请稍后重试。")
		return
	}
	session, err := w.supervisor.oma.getSession(ctx, apiKey, sessionID)
	if err != nil {
		_ = w.replyText(ctx, msg, "读取会话状态失败，请稍后重试。")
		return
	}
	w.mu.Lock()
	pending := len(w.replies[scopeKey])
	w.mu.Unlock()
	_ = w.replyText(ctx, msg, fmt.Sprintf("会话：%s\n状态：%s\n智能体：%s（版本 %d）\n环境：%s\n待完成回复：%d", session.ID, session.Status, session.Agent.ID, session.Agent.Version, session.EnvironmentID, pending))
}

func (w *channelWorker) resumeCommand(ctx context.Context, scopeKey string, msg *channelMessage, args []string) {
	if len(args) > 1 {
		_ = w.replyText(ctx, msg, "用法：/resume 或 /resume 会话ID")
		return
	}
	apiKey, err := w.supervisor.apiKey(ctx, w.record)
	if err != nil {
		_ = w.replyText(ctx, msg, "读取历史会话失败，请稍后重试。")
		return
	}
	target, err := w.resolveRecoveryTarget(ctx, msg, scopeKey)
	if err != nil {
		_ = w.replyText(ctx, msg, "读取当前执行配置失败，请稍后重试。")
		return
	}
	if target.decision.Mode == "hold" {
		_ = w.replyText(ctx, msg, target.decision.Notice)
		return
	}
	if len(args) == 0 {
		w.listHistory(ctx, scopeKey, msg, apiKey, target)
		return
	}
	w.mu.Lock()
	pending := len(w.replies[scopeKey])
	w.mu.Unlock()
	if pending > 0 {
		_ = w.replyText(ctx, msg, "当前还有未完成的回复，请等待结束或先 /stop。")
		return
	}
	if current := w.currentSession(ctx, scopeKey); current != "" {
		session, err := w.supervisor.oma.getSession(ctx, apiKey, current)
		if err != nil || session.Status != "idle" {
			_ = w.replyText(ctx, msg, "当前会话状态尚未确认空闲，请先用 /status 核对。")
			return
		}
	}
	session, err := w.supervisor.oma.getSession(ctx, apiKey, args[0])
	compatible, boundErr := w.recoverableSession(ctx, session, scopeKey, w.legacyGroupScope(msg), target)
	if boundErr != nil {
		_ = w.replyText(ctx, msg, "读取会话身份失败，请稍后重试。")
		return
	}
	if err != nil || !compatible {
		_ = w.replyText(ctx, msg, "该会话不属于当前渠道/话题，或配置不兼容、当前不可恢复。")
		return
	}
	err = w.supervisor.store.SetChannelSessionBinding(ctx, w.record.ExternalID, scopeKey, session.ID, target.decision.PairingExternalID, target.decision.BindingRevision)
	if errors.Is(err, db.ErrVersionConflict) {
		_ = w.replyText(ctx, msg, "当前配对已变化，请重新选择要恢复的会话。")
		return
	}
	if err != nil {
		_ = w.replyText(ctx, msg, "恢复会话失败，请稍后重试。")
		return
	}
	_ = w.replyText(ctx, msg, "已恢复 OMA 会话，请继续发送消息。")
}

func (w *channelWorker) compatibleSession(session omaSession, scopeKey, legacyScope, environmentID, personalID string) bool {
	return w.compatibleSessionConfiguration(session, environmentID, personalID) && w.sessionMatchesChannel(session, scopeKey, legacyScope)
}

func (w *channelWorker) sessionMatchesChannel(session omaSession, scopeKey, legacyScope string) bool {
	chatID, isChat := strings.CutPrefix(scopeKey, "chat:")
	legacyDirect := isChat && session.Metadata["chat_type"] == "p2p" && strings.HasPrefix(session.Metadata["scope_id"], "thread:"+chatID+":")
	return session.ID != "" && session.Status == "idle" && session.ArchivedAt == nil &&
		session.Metadata["source"] == w.record.ChannelType && session.Metadata["channel_id"] == w.record.ExternalID &&
		session.Metadata["app_id"] == w.record.AppID &&
		(session.Metadata["scope_id"] == scopeKey || legacyDirect || legacyScope != "" && session.Metadata["scope_id"] == legacyScope)
}

type recoveryTarget struct {
	decision      identities.Decision
	environmentID string
}

func (w *channelWorker) resolveRecoveryTarget(ctx context.Context, msg *channelMessage, scopeKey string) (recoveryTarget, error) {
	decision, err := w.executionDecision(ctx, msg, scopeKey, false)
	if err != nil || decision.Mode == "hold" {
		return recoveryTarget{decision: decision}, err
	}
	if decision.Mode == "legacy" {
		decision.AgentExternalID = w.record.AgentExternalID
	}
	environmentID, err := w.supervisor.agentEnvironment(ctx, w.record.OrganizationUUID, w.record.WorkspaceUUID, decision.AgentExternalID)
	return recoveryTarget{decision: decision, environmentID: environmentID}, err
}

func (w *channelWorker) recoverableSession(ctx context.Context, session omaSession, scopeKey, legacyScope string, target recoveryTarget) (bool, error) {
	if target.decision.Mode == "legacy" {
		return session.Metadata["identity_id"] == "" && w.compatibleSession(session, scopeKey, legacyScope, target.environmentID, target.decision.PersonalVaultID), nil
	}
	if target.decision.Mode != "execute" || !w.sessionMatchesChannel(session, scopeKey, legacyScope) ||
		!w.decisionCompatible(session, target.decision, target.environmentID, scopeKey) {
		return false, nil
	}
	return w.sessionIdentityBound(ctx, session.ID, target.decision, scopeKey)
}

func (w *channelWorker) listHistory(ctx context.Context, scopeKey string, msg *channelMessage, apiKey string, target recoveryTarget) {
	path := "/v1/sessions?agent_id=" + url.QueryEscape(target.decision.AgentExternalID) + "&limit=100&order=desc"
	lines := make([]string, 0, 20)
	pages := make(map[string]struct{})
	for {
		var page struct {
			Data     []omaSession `json:"data"`
			NextPage string       `json:"next_page"`
		}
		if err := w.supervisor.oma.request(ctx, apiKey, http.MethodGet, path, nil, &page); err != nil {
			_ = w.replyText(ctx, msg, "读取历史会话失败，请稍后重试。")
			return
		}
		for _, session := range page.Data {
			compatible, boundErr := w.recoverableSession(ctx, session, scopeKey, w.legacyGroupScope(msg), target)
			if boundErr != nil {
				_ = w.replyText(ctx, msg, "读取会话身份失败，请稍后重试。")
				return
			}
			if compatible {
				lines = append(lines, fmt.Sprintf("/resume %s（版本 %d）", session.ID, session.Agent.Version))
				if len(lines) == 20 {
					break
				}
			}
		}
		if page.NextPage == "" || len(page.Data) == 0 || len(lines) == 20 {
			break
		}
		if _, seen := pages[page.NextPage]; seen {
			_ = w.replyText(ctx, msg, "历史会话分页异常，本次未恢复会话，请稍后重试。")
			return
		}
		pages[page.NextPage] = struct{}{}
		path = "/v1/sessions?agent_id=" + url.QueryEscape(target.decision.AgentExternalID) + "&limit=100&order=desc&page=" + url.QueryEscape(page.NextPage)
	}
	if len(lines) == 0 {
		_ = w.replyText(ctx, msg, "没有兼容的 OMA 历史会话。")
		return
	}
	_ = w.replyText(ctx, msg, "最近可恢复的会话：\n"+strings.Join(lines, "\n"))
}

func (w *channelWorker) finishInterruptedReplies(tasks []*channelReplyTask, apiKey string, confirmed bool) {
	ctx, cancel := context.WithTimeout(w.baseContext(), 5*time.Second)
	defer cancel()
	for _, task := range tasks {
		select {
		case <-task.done:
		case <-ctx.Done():
			return
		}
		runner := task.runner
		if runner == nil || runner.stream == nil || runner.reply.ended {
			continue
		}
		runner.reply.interrupted = true
		runner.reply.thinking = false
		runner.reply.status = "停止待确认"
		if confirmed {
			events, err := w.supervisor.oma.listEvents(ctx, apiKey, task.sessionID)
			if err != nil {
				w.supervisor.logger.WarnContext(ctx, "read interrupted channel events", "channel_id", w.record.ExternalID, "session_id", task.sessionID, "error", err)
			} else if _, err := runner.consumeHistory(ctx, events); err != nil {
				w.supervisor.logger.WarnContext(ctx, "deliver interrupted channel events", "channel_id", w.record.ExternalID, "session_id", task.sessionID, "error", err)
			}
			runner.reply.status = "已停止"
			runner.reply.finish("已停止本轮任务。")
		}
		if err := runner.update(ctx); err != nil {
			w.supervisor.logger.WarnContext(ctx, "update interrupted channel reply", "channel_id", w.record.ExternalID, "session_id", task.sessionID, "error", err)
		}
	}
}

func (w *channelWorker) compatibleSessionConfiguration(session omaSession, environmentID, personalID string) bool {
	version := parseChannelMounts(w.record.Mounts).AgentVersion
	policy := ""
	if version > 0 {
		policy = strconv.Itoa(version)
	}
	return session.Agent.ID == w.record.AgentExternalID && session.Metadata["agent_version"] == policy && (version == 0 || session.Agent.Version == version) && session.EnvironmentID == environmentID && w.compatibleMounts(session, personalID)
}
