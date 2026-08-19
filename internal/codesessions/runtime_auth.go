package codesessions

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) authenticateRuntimeSession(w http.ResponseWriter, r *http.Request) (SessionCredentialClaims, string, bool) {
	token := auth.ExtractAPIKey(r)
	if token == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing code session token"))
		return SessionCredentialClaims{}, "", false
	}
	claims, err := h.service.AuthenticateSessionIngress(token, "")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid code session token"))
		return SessionCredentialClaims{}, "", false
	}
	return claims, token, true
}

func (h *Handler) authorizeSessionIngress(w http.ResponseWriter, r *http.Request, codeSessionID string) bool {
	if err := h.authorizeSessionIngressRequest(r, codeSessionID); err != nil {
		h.errorAdapter.Write(w, r, err)
		return false
	}
	return true
}

func (h *Handler) authorizeSessionIngressRequest(r *http.Request, codeSessionID string) error {
	// 校验 URL 中的 codeSessionID，为空时返回 404，避免处理没有明确 session 归属的请求。
	if strings.TrimSpace(codeSessionID) == "" {
		return codeSessionRouteNotFound()
	}
	token := auth.ExtractAPIKey(r)
	if token == "" {
		return sessionIngressTokenRequired()
	}
	if _, err := h.service.AuthenticateSessionIngress(token, codeSessionID); err != nil {
		return sessionIngressTokenInvalid(err)
	}
	return nil
}
