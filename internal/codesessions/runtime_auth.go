package codesessions

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (h *Handler) authorizeRuntimeSession(w http.ResponseWriter, r *http.Request) (SessionCredentialClaims, string, bool) {
	token := auth.ExtractAPIKey(r)
	if token == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing code session token"))
		return SessionCredentialClaims{}, "", false
	}
	claims, err := h.service.authenticateRuntimeCredential(r.Context(), token, "")
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid code session token"))
		return SessionCredentialClaims{}, "", false
	}
	if claims.Role == "worker" {
		if err := h.service.authorizeWorkerClaims(r.Context(), claims); err != nil {
			h.errorAdapter.Write(w, r, workerIngressAuthorizationFailure(err))
			return SessionCredentialClaims{}, "", false
		}
	}
	return claims, token, true
}

func (h *Handler) authenticateMCPProxyRequest(w http.ResponseWriter, r *http.Request, codeSessionID string) (SessionCredentialClaims, bool) {
	token := auth.ExtractAPIKey(r)
	if token == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing MCP proxy token"))
		return SessionCredentialClaims{}, false
	}
	claims, err := h.service.AuthenticateSessionIngress(r.Context(), token, codeSessionID)
	if err != nil || claims.SessionID != codeSessionID || claims.WorkerEpoch <= 0 {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid MCP proxy token"))
		return SessionCredentialClaims{}, false
	}
	if err := h.service.authorizeWorkerClaims(r.Context(), claims); err != nil {
		h.errorAdapter.Write(w, r, workerIngressAuthorizationFailure(err))
		return SessionCredentialClaims{}, false
	}
	return claims, true
}

func (h *Handler) authorizeSessionIngressClaims(w http.ResponseWriter, r *http.Request, codeSessionID string) (SessionCredentialClaims, bool) {
	return h.sessionIngressAccessClaims(w, r, codeSessionID, true)
}

func (h *Handler) sessionIngressAccessClaims(w http.ResponseWriter, r *http.Request, codeSessionID string, requireCurrentWorker bool) (SessionCredentialClaims, bool) {
	claims, err := h.sessionIngressClaims(r, codeSessionID)
	if err != nil {
		h.errorAdapter.Write(w, r, err)
		return SessionCredentialClaims{}, false
	}
	if requireCurrentWorker {
		err = h.service.authorizeWorkerClaims(r.Context(), claims)
	} else {
		err = h.service.authorizeIngressIdentity(r.Context(), claims)
	}
	if err != nil {
		h.errorAdapter.Write(w, r, workerIngressAuthorizationFailure(err))
		return SessionCredentialClaims{}, false
	}
	return claims, true
}

// sessionIngressClaims 返回鉴权后的 claims；OTLP ingress 需要 claims 并以 OTLP
// 状态体回写错误，因此返回 apperr 交由调用方按各自的传输格式适配。
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
