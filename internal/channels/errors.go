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

func invalidAppCredentials() error {
	return apperr.New(apperr.InvalidArgument, "应用密钥校验未通过", nil)
}

func invalidCredentialCheckBody(cause error) error {
	return apperr.New(apperr.InvalidArgument, "应用凭据校验请求正文无效", cause)
}

func credentialCheckUnavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "无法连接飞书校验应用密钥", cause)
}

func invalidChannelPage(cause error) error {
	return apperr.New(apperr.InvalidArgument, "渠道列表分页参数无效", cause)
}

func invalidAuthorizationID() error {
	return apperr.New(apperr.InvalidArgument, "渠道授权编号无效", nil)
}

func invalidMemoryMount(cause error) error {
	return apperr.New(apperr.InvalidArgument, "请选择当前工作区中未归档的 Memory", cause)
}
func invalidVaultMount(cause error) error {
	return apperr.New(apperr.InvalidArgument, "请选择当前工作区中未归档的 Vault", cause)
}
