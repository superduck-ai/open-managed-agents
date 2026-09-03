package codesessions

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"

	"github.com/go-chi/chi/v5"
)

const maxMCPProxyURLBytes = 2048

// mcpErrorEventPublishTimeout 限制错误事件发布的同步等待上界，避免拖慢 502 响应。
const mcpErrorEventPublishTimeout = 2 * time.Second

// mcpProxyTransportWrapper wraps the MCP upstream RoundTripper for vault
// inject + mcp_oauth 401 refresh retry. This is the sole production credential
// injection seam (tests may substitute a fake wrapper).
type mcpProxyTransportWrapper func(context.Context, SessionCredentialClaims, *url.URL, http.RoundTripper) http.RoundTripper

func (h *Handler) handleMCPProxy(w http.ResponseWriter, r *http.Request) {
	codeSessionID := strings.TrimSpace(chi.URLParam(r, "code_session_id"))
	claims, _, ok := h.authenticateRuntimeSession(w, r)
	if !ok {
		return
	}
	if codeSessionID == "" || claims.SessionID != codeSessionID {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid code session token"))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusMethodNotAllowed, "invalid_request_error", "MCP proxy only supports GET, POST, and DELETE"))
		return
	}
	target, rawTarget, err := parseMCPProxyTarget(r.URL.RawQuery)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	logTarget := *target
	logTarget.RawQuery = ""
	logTarget.ForceQuery = false
	h.logger.InfoContext(r.Context(), "MCP proxy request received",
		"code_session_id", codeSessionID,
		"method", r.Method,
		"mcp_url", logTarget.String(),
		"content_type", strings.TrimSpace(r.Header.Get("Content-Type")),
		"content_length", r.ContentLength,
	)
	identity := upstreamProxyIdentity{
		codeSessionExternalID: claims.SessionID,
		organizationUUID:      claims.OrganizationUUID,
		workspaceUUID:         claims.WorkspaceUUID,
	}
	if !h.authorizeMCPProxyTarget(r.Context(), identity, target, rawTarget) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "MCP upstream is not allowed"))
		return
	}

	headers := r.Header.Clone()
	for _, name := range []string{"Authorization", "X-Api-Key", "Proxy-Authorization", "Proxy-Connection"} {
		headers.Del(name)
	}
	request := r.Clone(r.Context())
	request.Header = headers
	h.serveMCPProxy(w, request, target, codeSessionID, claims)
}

func (h *Handler) authorizeMCPProxyTarget(ctx context.Context, identity upstreamProxyIdentity, target *url.URL, rawTarget string) bool {
	policyContext, err := h.loadMCPPolicyContext(ctx, identity)
	if err != nil {
		h.logger.WarnContext(ctx, "MCP proxy policy denied", "organization_uuid", identity.organizationUUID, "workspace_uuid", identity.workspaceUUID, "code_session_id", identity.codeSessionExternalID, "reason", string(networkpolicy.ReasonPolicyUnavailable), "host", target.Hostname(), "error", err)
		return false
	}
	decision := policyContext.policy.AuthorizeMCPURL(rawTarget)
	if !decision.Allow {
		h.logger.WarnContext(ctx, "MCP proxy policy denied", "organization_uuid", policyContext.organizationUUID, "workspace_uuid", policyContext.workspaceUUID, "environment_id", policyContext.environmentExternalID, "code_session_id", identity.codeSessionExternalID, "reason", string(decision.Reason), "host", decision.Host)
		return false
	}
	h.logger.DebugContext(ctx, "MCP proxy policy allowed", "organization_uuid", policyContext.organizationUUID, "workspace_uuid", policyContext.workspaceUUID, "environment_id", policyContext.environmentExternalID, "code_session_id", identity.codeSessionExternalID, "reason", string(decision.Reason), "host", decision.Host)
	return true
}

func (h *Handler) serveMCPProxy(w http.ResponseWriter, r *http.Request, target *url.URL, codeSessionID string, claims SessionCredentialClaims) {
	transport := h.mcpProxyTransport
	if transport == nil {
		transport = newMCPProxyTransport(h.cfg.CodeSession.UpstreamProxyDisableSSRFProtection)
	}
	if h.wrapMCPVaultTransport != nil {
		transport = h.wrapMCPVaultTransport(r.Context(), claims, target, transport)
	}
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(request *httputil.ProxyRequest) {
			upstreamURL := *target
			request.Out.URL = &upstreamURL
			request.Out.Host = target.Host
		},
		ErrorHandler: func(writer http.ResponseWriter, request *http.Request, err error) {
			// 带内部 cause 的注入拒绝（vault 读取/解密故障）是内部错误，不误报为认证失败。
			if mcpAuthRejected(err) {
				h.logger.ErrorContext(request.Context(), "inject MCP proxy credentials", "code_session_id", codeSessionID, "host", target.Hostname(), "error", err)
				h.publishMCPErrorEvent(request.Context(), codeSessionID, "mcp_authentication_failed_error", target.Hostname(), "not_retryable", vaults.InjectionUnavailablePublicMessage)
				httpapi.WriteError(writer, request, httpapi.NewError(http.StatusBadGateway, "api_error", vaults.InjectionUnavailablePublicMessage))
				return
			}
			// 客户端主动断开不是上游故障，与日志守卫一致，不发错误事件。
			if request.Context().Err() == nil {
				h.logger.WarnContext(request.Context(), "proxy MCP upstream request", "code_session_id", codeSessionID, "host", target.Hostname(), "error", err)
				h.publishMCPErrorEvent(request.Context(), codeSessionID, "mcp_connection_failed_error", target.Hostname(), "retryable", "MCP upstream is unavailable")
			}
			httpapi.WriteError(writer, request, httpapi.NewError(http.StatusBadGateway, "api_error", "MCP upstream is unavailable"))
		},
	}
	proxy.ServeHTTP(w, r)
}

// mcpAuthRejected 判定注入拒绝是否为认证失败：Cause 为 nil 表示「host 需要注入但无匹配凭证」，
// 带内部 cause（vault 读取/解密/refresh 故障）的拒绝是内部错误，不误报为认证失败。
func mcpAuthRejected(err error) bool {
	rejected, ok := errors.AsType[*vaults.InjectionRejectedError](err)
	return ok && rejected.Cause() == nil
}

// publishMCPErrorEvent 在 MCP 代理失败时向 session 事件流发布 session.error（best-effort，
// 2 秒超时上界，不显著阻塞 502 响应）。走 publishPublicPayloads → PublishCodeSessionEvents。
func (h *Handler) publishMCPErrorEvent(ctx context.Context, codeSessionID, errorType, serverName, retryStatus, message string) {
	payload, err := mcpSessionErrorPayload(errorType, serverName, retryStatus, message)
	if err != nil {
		h.logger.ErrorContext(ctx, "build MCP session error payload", "code_session_id", codeSessionID, "error", err)
		return
	}
	publishCtx, cancel := context.WithTimeout(ctx, mcpErrorEventPublishTimeout)
	defer cancel()
	if err := h.service.publishPublicPayloads(publishCtx, codeSessionID, []json.RawMessage{payload}); err != nil {
		h.logger.ErrorContext(ctx, "publish MCP session error event", "code_session_id", codeSessionID, "error", err)
	}
}

// mcpSessionErrorPayload 构造 MCP 失败事件的 session.error payload（官方契约：type + mcp_server_name + retry_status + message）。
func mcpSessionErrorPayload(errorType, serverName, retryStatus, message string) (json.RawMessage, error) {
	return marshalRaw(map[string]any{
		"type": "session.error",
		"error": map[string]any{
			"type":            errorType,
			"mcp_server_name": serverName,
			"retry_status":    retryStatus,
			"message":         message,
		},
	})
}

func parseMCPProxyTarget(rawQuery string) (*url.URL, string, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, "", errors.New("mcp_url query parameter is invalid")
	}
	values := query["mcp_url"]
	if len(values) != 1 {
		return nil, "", errors.New("exactly one mcp_url query parameter is required")
	}
	rawTarget := strings.TrimSpace(values[0])
	if rawTarget == "" || len(rawTarget) > maxMCPProxyURLBytes {
		return nil, "", errors.New("mcp_url query parameter is invalid")
	}
	target, err := url.Parse(rawTarget)
	if err != nil || !target.IsAbs() || target.Host == "" || target.Hostname() == "" {
		return nil, "", errors.New("mcp_url must be an absolute HTTP(S) URL")
	}
	target.Scheme = strings.ToLower(target.Scheme)
	if (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.Fragment != "" {
		return nil, "", errors.New("mcp_url must be an absolute HTTP(S) URL without credentials or fragments")
	}
	return target, rawTarget, nil
}

func newMCPProxyTransport(disableSSRFProtection bool) http.RoundTripper {
	transport := &http.Transport{DisableCompression: true, MaxIdleConnsPerHost: 32}
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok && defaultTransport != nil {
		transport = defaultTransport.Clone()
		transport.Proxy = nil
		transport.DisableCompression = true
		transport.MaxIdleConnsPerHost = 32
	}
	dialer := net.Dialer{Timeout: upstreamProxyDialTimeout, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		resolved, err := resolveProxyTarget(ctx, address, disableSSRFProtection, false)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, "tcp", resolved)
	}
	return transport
}
