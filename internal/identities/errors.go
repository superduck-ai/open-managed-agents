package identities

import (
	"errors"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

var errCredentialUnavailable = errors.New("shared credential unavailable")

func invalid(message string, cause error) error {
	return apperr.New(apperr.InvalidArgument, message, cause)
}

func notFound(message string, cause error) error {
	return apperr.New(apperr.NotFound, message, cause)
}

func conflict(message string, cause error) error {
	return apperr.New(apperr.Conflict, message, cause)
}

func unauthenticated() error {
	return apperr.New(apperr.Unauthenticated, "请先登录", nil)
}

func unavailable(cause error) error {
	return apperr.New(apperr.Unavailable, "执行身份服务暂不可用，请稍后重试", cause)
}

func mapStore(err error, missing string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNotFound) {
		return notFound(missing, err)
	}
	if errors.Is(err, db.ErrDuplicate) {
		return conflict("相同业务主体或生效配置已存在", err)
	}
	if errors.Is(err, db.ErrVersionConflict) {
		return conflict("修订已变化，请读取最新内容后重试", err)
	}
	if errors.Is(err, db.ErrInvalidState) {
		return conflict("当前状态不能完成该操作", err)
	}
	return unavailable(err)
}
