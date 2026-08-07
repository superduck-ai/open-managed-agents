package codesessions

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"

	"github.com/go-chi/chi/v5"
)

const maxMCPProxyURLBytes = 2048

// mcpProxyHeaderInjector 是服务端 MCP 凭证注入边界。默认实现不注入任何凭证；
// 后续可在这里按已验签的 session claims 和目标 URL 从 vault 解析上游 header。
type mcpProxyHeaderInjector func(context.Context, SessionCredentialClaims, *url.URL, http.Header) error

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
	if h.injectMCPProxyHeaders != nil {
		if err := h.injectMCPProxyHeaders(r.Context(), claims, target, headers); err != nil {
			h.logger.ErrorContext(r.Context(), "inject MCP proxy credentials", "code_session_id", codeSessionID, "host", target.Hostname(), "error", err)
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "MCP upstream credentials are unavailable"))
			return
		}
	}
	request := r.Clone(r.Context())
	request.Header = headers
	h.serveMCPProxy(w, request, target, codeSessionID)
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

func (h *Handler) serveMCPProxy(w http.ResponseWriter, r *http.Request, target *url.URL, codeSessionID string) {
	transport := h.mcpProxyTransport
	if transport == nil {
		transport = newMCPProxyTransport(h.cfg.CodeSession.UpstreamProxyDisableSSRFProtection)
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
			if request.Context().Err() == nil {
				h.logger.WarnContext(request.Context(), "proxy MCP upstream request", "code_session_id", codeSessionID, "host", target.Hostname(), "error", err)
			}
			httpapi.WriteError(writer, request, httpapi.NewError(http.StatusBadGateway, "api_error", "MCP upstream is unavailable"))
		},
	}
	proxy.ServeHTTP(w, r)
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
