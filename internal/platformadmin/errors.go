package platformadmin

import (
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"net/http"
)

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

var (
	ErrRateLimited           = &Error{http.StatusTooManyRequests, "rate_limited", "查询过于频繁，请稍后重试"}
	ErrDenied                = &Error{http.StatusForbidden, "platform_access_denied", "仅超级管理员可访问管理后台"}
	ErrUnauthenticated       = &Error{http.StatusUnauthorized, "unauthenticated", "请先登录"}
	ErrVerifiedLogin         = &Error{http.StatusForbidden, "verified_login_required", "请使用真实邮箱验证码重新登录"}
	ErrCSRF                  = &Error{http.StatusForbidden, "invalid_csrf", "请求验证失败，请刷新页面"}
	ErrLastAdmin             = &Error{http.StatusConflict, "last_platform_admin", "必须保留至少一位超级管理员"}
	ErrLastOrganizationAdmin = &Error{http.StatusConflict, "last_organization_admin", "必须保留至少一位组织管理员"}
	ErrVersion               = &Error{http.StatusConflict, "version_conflict", "组织信息已变更，请刷新后重试"}
	ErrIdempotency           = &Error{http.StatusConflict, "idempotency_conflict", "相同幂等键不能用于不同的创建请求"}
	ErrState                 = &Error{http.StatusConflict, "state_conflict", "当前状态不允许此操作"}
	ErrNotFound              = &Error{http.StatusNotFound, "not_found", "目标不存在"}
	ErrUnavailable           = &Error{http.StatusServiceUnavailable, "unavailable", "管理服务暂不可用"}
)

func invalid(message string) error { return &Error{http.StatusBadRequest, "invalid_request", message} }

func publicError(err error) *Error {
	if e, ok := errors.AsType[*Error](err); ok {
		return e
	}
	if errors.Is(err, db.ErrNotFound) {
		return ErrNotFound
	}
	return ErrUnavailable
}
