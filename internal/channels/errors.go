package channels

import "github.com/superduck-ai/open-managed-agents/internal/apperr"

func unavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "飞书渠道服务不可用，请检查桥接服务配置", cause)
}

func rejected(kind apperr.Kind, message string) error {
	return apperr.New(kind, message, nil)
}
