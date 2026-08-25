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
	claims, err := h.service.AuthenticateSessionIngress(r.Context(), token, "")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid code session token"))
		return SessionCredentialClaims{}, "", false
	}
	return claims, token, true
}

func (h *Handler) authorizeSessionIngress(w http.ResponseWriter, r *http.Request, codeSessionID string) bool {
	_, ok := h.authorizeSessionIngressClaims(w, r, codeSessionID)
	return ok
}

func (h *Handler) authorizeSessionIngressClaims(w http.ResponseWriter, r *http.Request, codeSessionID string) (SessionCredentialClaims, bool) {
	claims, err := h.sessionIngressClaims(r, codeSessionID)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return SessionCredentialClaims{}, false
	}
	return claims, true
}

func (h *Handler) authorizeSessionIngressRequest(r *http.Request, codeSessionID string) error {
	_, err := h.sessionIngressClaims(r, codeSessionID)
	return err
}

func (h *Handler) sessionIngressClaims(r *http.Request, codeSessionID string) (SessionCredentialClaims, error) {
	// 校验 URL 中的 codeSessionID，为空时返回 404，避免处理没有明确 session 归属的请求。
	if strings.TrimSpace(codeSessionID) == "" {
		return SessionCredentialClaims{}, codeSessionRouteNotFound()
	}
	token := auth.ExtractAPIKey(r)
	if token == "" {
		return SessionCredentialClaims{}, sessionIngressTokenRequired()
	}
	claims, err := h.service.AuthenticateSessionIngress(r.Context(), token, codeSessionID)
	if err != nil {
		return SessionCredentialClaims{}, sessionIngressTokenInvalid(err)
	}
	return claims, nil
}

func codeSessionStreamRouteFromClaims(claims SessionCredentialClaims) CodeSessionStreamRoute {
	return CodeSessionStreamRoute{
		CodeSessionID:     claims.SessionID,
		WorkspaceUUID:     claims.WorkspaceUUID,
		SessionExternalID: claims.PublicSessionID,
	}
}
