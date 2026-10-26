package channels

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func unavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "飞书渠道服务不可用，请检查桥接服务配置", cause)
}

func unauthenticated() error {
	return apperr.New(apperr.Unauthenticated, "请先登录", nil)
}

func agentNotFound() error {
	return apperr.New(apperr.NotFound, "Agent 不存在", nil)
}

func archivedAgent() error {
	return apperr.New(apperr.Conflict, "已归档 Agent 无法修改渠道", nil)
}

func csrfRejected() error {
	return apperr.New(apperr.PermissionDenied, "Invalid CSRF token", nil)
}
