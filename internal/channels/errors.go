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

func invalidEnvironment(cause error) error {
	return apperr.New(apperr.InvalidArgument, "请选择当前工作区中未归档的执行环境", cause)
}

func environmentRequired() error {
	return apperr.New(apperr.InvalidArgument, "请先选择执行环境，再保存渠道", nil)
}

func credentialInactive(cause error) error {
	return apperr.New(apperr.Conflict, "渠道专用 API Key 已停用，请在 API 密钥管理中恢复", cause)
}

func credentialProvisionFailed(cause error) error {
	return apperr.New(apperr.Unavailable, "创建渠道工作区凭据失败，请联系管理员", cause)
}

func credentialSyncUnavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "同步渠道工作区凭据失败，请检查桥接服务", cause)
}

func credentialSyncRejected(cause error) error {
	return apperr.New(apperr.Unavailable, "同步渠道工作区凭据失败，请检查桥接版本与配置", cause)
}

func credentialUnreadable(cause error) error {
	return apperr.New(apperr.Unavailable, "渠道工作区凭据无法解密，请联系管理员检查主密钥或恢复备份", cause)
}

func credentialWorkspaceNotFound(cause error) error {
	return apperr.New(apperr.NotFound, "工作区不存在或已归档", cause)
}

func invalidChannelUpdate(cause error) error {
	return apperr.New(apperr.InvalidArgument, "渠道更新请求正文无效", cause)
}
