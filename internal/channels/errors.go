package channels

import (
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/channels/contracts"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const channelExecutionFailedMessage = "本轮执行失败，请稍后重试或在 OMA 会话中查看错误。"

var (
	errChannelSubscriptionUnavailable = errors.New("内部会话订阅尚未配置")
	errChannelDeliveryUnconfirmed     = errors.New("渠道卡片交付未确认")
	errChannelSessionUnconfirmed      = errors.New("渠道会话状态未确认")
	errChannelExecutionHeld           = errors.New("渠道执行已拦截")
	errOMAPaginationCycle             = errors.New("OMA 分页游标重复")
	errChannelInputUnconfirmed        = errors.New("渠道输入提交结果未确认")
	errOMARequestRejected             = errors.New("OMA 明确拒绝请求")
	errChannelMediaUploadRejected     = contracts.ErrMediaUploadRejected
)

func omaResponseError(status int) error {
	if status >= 400 && status < 500 && status != 408 {
		return fmt.Errorf("OMA 请求返回 HTTP %d: %w", status, errOMARequestRejected)
	}
	return fmt.Errorf("OMA 请求返回 HTTP %d", status)
}

func invalidQuestionDefinition() error {
	return apperr.New(apperr.InvalidArgument, "问卷结构无效，请在 OMA 控制台处理", nil)
}

func questionAnswerRequired(number int) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("请回答第 %d 个问题", number), nil)
}

func invalidQuestionOption(number int) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("第 %d 个问题的选项无效", number), nil)
}

func questionAnswerTooLong(number int) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("第 %d 个问题的回答超过 10000 字符", number), nil)
}

func questionInteractionUnavailable() error {
	return apperr.New(apperr.Conflict, "该问卷已失效，请在 OMA 会话中核对", nil)
}

func unavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "渠道服务暂不可用，请稍后重试", cause)
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

func invalidMemoryMount(cause error) error {
	return apperr.New(apperr.InvalidArgument, "请选择当前工作区中未归档的 Memory", cause)
}

func invalidChannelMounts(cause error) error {
	return apperr.New(apperr.InvalidArgument, "渠道挂载配置无效", cause)
}
func invalidVaultMount(cause error) error {
	return apperr.New(apperr.InvalidArgument, "请选择当前工作区中未归档的 Vault", cause)
}

func channelCredentialError(cause error) error {
	switch {
	case cause == nil:
		return nil
	case errors.Is(cause, db.ErrChannelCredentialInactive):
		return credentialInactive(cause)
	case errors.Is(cause, db.ErrChannelCredentialUnreadable):
		return credentialUnreadable(cause)
	case errors.Is(cause, db.ErrChannelCredentialUserRequired):
		return apperr.New(apperr.Conflict, "渠道运行身份缺失，请由已登录用户重新保存渠道配置", cause)
	case errors.Is(cause, db.ErrNotFound):
		return credentialWorkspaceNotFound(cause)
	default:
		return credentialProvisionFailed(cause)
	}
}

func unsupportedChannelType(channelType string) error {
	return apperr.New(apperr.InvalidArgument, fmt.Sprintf("不支持的渠道类型：%s", channelType), nil)
}

func invalidChannelCardReceipt() error {
	return apperr.New(apperr.PermissionDenied, "该卡片已失效或不属于当前对话", nil)
}
