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
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"

	"github.com/go-chi/chi/v5"
)

const maxMCPProxyURLBytes = 2048

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
	if !mcpProxyMethodAllowed(r.Method) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusMethodNotAllowed, "invalid_request_error", "MCP proxy only supports GET, POST, and DELETE"))
		return
	}
	target, rawTarget, err := parseMCPProxyTarget(r.URL.RawQuery)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	identity := upstreamProxyIdentity{
		codeSessionExternalID: claims.SessionID,
		organizationUUID:      claims.OrganizationUUID,
		workspaceUUID:         claims.WorkspaceUUID,
	}
	if !h.authorizeMCPProxyTarget(r.Context(), identity, target, rawTarget) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "MCP upstream is not allowed"))
		return
	}
	h.forwardMCPProxy(w, r, claims, target, "")
}

func (h *Handler) handleNamedMCPProxy(w http.ResponseWriter, r *http.Request) {
	codeSessionID := strings.TrimSpace(chi.URLParam(r, "code_session_id"))
	serverName := namedMCPServerName(r)
	if codeSessionID == "" || !canonicalMCPServerName(serverName) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
		return
	}
	claims, ok := h.authenticateMCPProxyRequest(w, r, codeSessionID)
	if !ok {
		return
	}
	if !mcpProxyMethodAllowed(r.Method) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusMethodNotAllowed, "invalid_request_error", "MCP proxy only supports GET, POST, and DELETE"))
		return
	}
	identity := upstreamProxyIdentity{
		codeSessionExternalID: claims.SessionID,
		organizationUUID:      claims.OrganizationUUID,
		workspaceUUID:         claims.WorkspaceUUID,
	}
	policyContext, err := h.loadMCPPolicyContext(r.Context(), identity)
	if err != nil {
		h.logMCPProxyPolicyUnavailable(r.Context(), identity, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "MCP upstream is not allowed"))
		return
	}
	rawTarget, found := policyContext.policy.MCPServerURL(serverName)
	if !found {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "MCP server is not configured"))
		return
	}
	target, err := parseMCPProxyURL(rawTarget)
	if err != nil || !h.namedTargetIsTunnel(target) {
		h.writeNamedTunnelNotFound(w, r)
		return
	}
	if !h.authorizeLoadedMCPProxyTarget(r.Context(), policyContext, identity, target, rawTarget) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "MCP upstream is not allowed"))
		return
	}
	h.forwardMCPProxy(w, r, claims, target, serverName)
}

// HandleMCPProtectedResource serves RFC 9728 metadata only for a named Tunnel
// target. Ordinary MCP servers keep their original direct runtime path.
func (h *Handler) HandleMCPProtectedResource(w http.ResponseWriter, r *http.Request) {
	codeSessionID := strings.TrimSpace(chi.URLParam(r, "code_session_id"))
	serverName := namedMCPServerName(r)
	if codeSessionID == "" || !canonicalMCPServerName(serverName) || r.Method != http.MethodGet {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
		return
	}
	claims, ok := h.authenticateMCPProxyRequest(w, r, codeSessionID)
	if !ok {
		return
	}
	identity := upstreamProxyIdentity{
		codeSessionExternalID: claims.SessionID,
		organizationUUID:      claims.OrganizationUUID,
		workspaceUUID:         claims.WorkspaceUUID,
	}
	policyContext, err := h.loadMCPPolicyContext(r.Context(), identity)
	if err != nil {
		h.logMCPProxyPolicyUnavailable(r.Context(), identity, err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "MCP upstream is not allowed"))
		return
	}
	rawTarget, found := policyContext.policy.MCPServerURL(serverName)
	if !found {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "MCP server is not configured"))
		return
	}
	target, err := parseMCPProxyURL(rawTarget)
	if err != nil || !h.namedTargetIsTunnel(target) {
		h.writeNamedTunnelNotFound(w, r)
		return
	}
	if !h.authorizeLoadedMCPProxyTarget(r.Context(), policyContext, identity, target, rawTarget) {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusForbidden, "permission_error", "MCP upstream is not allowed"))
		return
	}
	headers := sanitizedMCPProxyHeaders(r.Header)
	request := r.Clone(r.Context())
	request.Header = headers
	if h.tunnelInvoker != nil {
		captured := newMCPDiscoveryCapture()
		if h.tunnelInvoker.ServeTunnelOAuthDiscovery(
			captured, request, claims.OrganizationUUID, claims.WorkspaceUUID, target,
		) {
			writeCapturedMCPDiscovery(
				w,
				r,
				captured,
				runtimeMCPResourceURL(r, codeSessionID, serverName),
				runtimeMCPMetadataURL(r, codeSessionID, serverName),
			)
			return
		}
	}
	h.writeNamedTunnelNotFound(w, r)
}

func canonicalMCPServerName(name string) bool {
	return name != "" && strings.TrimSpace(name) == name
}

func namedMCPServerName(r *http.Request) string {
	if name := chi.URLParam(r, "*"); name != "" {
		if decoded, err := url.PathUnescape(name); err == nil {
			return decoded
		}
		return name
	}
	return chi.URLParam(r, "server_name")
}

func mcpProxyMethodAllowed(method string) bool {
	return method == http.MethodGet || method == http.MethodPost || method == http.MethodDelete
}

func (h *Handler) forwardMCPProxy(
	w http.ResponseWriter,
	r *http.Request,
	claims SessionCredentialClaims,
	target *url.URL,
	serverName string,
) {
	logTarget := *target
	logTarget.RawQuery = ""
	logTarget.ForceQuery = false
	attrs := []any{
		"code_session_id", claims.SessionID,
		"method", r.Method,
		"mcp_url", logTarget.String(),
		"content_type", strings.TrimSpace(r.Header.Get("Content-Type")),
		"content_length", r.ContentLength,
	}
	if serverName != "" {
		attrs = append(attrs, "server_name", serverName)
	}
	h.logger.InfoContext(r.Context(), "MCP proxy request received", attrs...)

	headers := sanitizedMCPProxyHeaders(r.Header)
	request := r.Clone(r.Context())
	request.Header = headers
	if serverName == "" {
		if h.tunnelInvoker != nil && h.tunnelInvoker.ServeTunnel(
			w,
			request,
			claims.OrganizationUUID,
			claims.WorkspaceUUID,
			target,
		) {
			return
		}
		h.serveMCPProxy(w, request, target, claims.SessionID, claims)
		return
	}
	responseWriter := newMCPGatewayResponseWriter(w, runtimeMCPMetadataURL(r, claims.SessionID, serverName))
	if h.tunnelInvoker != nil {
		if h.tunnelInvoker.ServeTunnel(
			responseWriter,
			request,
			claims.OrganizationUUID,
			claims.WorkspaceUUID,
			target,
		) {
			return
		}
	}
	h.writeNamedTunnelNotFound(w, r)
}

func (h *Handler) writeNamedTunnelNotFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "MCP Tunnel is not configured"))
}

func (h *Handler) namedTargetIsTunnel(target *url.URL) bool {
	_, recognized, err := tunnels.RecognizeTarget(target, h.cfg.Tunnel)
	return recognized && err == nil
}

func sanitizedMCPProxyHeaders(source http.Header) http.Header {
	headers := source.Clone()
	for _, name := range []string{"Authorization", "X-Api-Key", "Proxy-Authorization", "Proxy-Connection"} {
		headers.Del(name)
	}
	return headers
}

func runtimeMCPResourceURL(r *http.Request, codeSessionID, serverName string) string {
	return strings.TrimRight(httpapi.RequestBaseURL(r), "/") +
		"/v2/ccr-sessions/" + url.PathEscape(codeSessionID) +
		"/mcp/" + url.PathEscape(serverName)
}

func runtimeMCPMetadataURL(r *http.Request, codeSessionID, serverName string) string {
	return strings.TrimRight(httpapi.RequestBaseURL(r), "/") +
		"/.well-known/oauth-protected-resource/v2/ccr-sessions/" + url.PathEscape(codeSessionID) +
		"/mcp/" + url.PathEscape(serverName)
}

type mcpGatewayResponseWriter struct {
	http.ResponseWriter
	metadataURL string
	wroteHeader bool
}

func (w *mcpGatewayResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func newMCPGatewayResponseWriter(w http.ResponseWriter, metadataURL string) http.ResponseWriter {
	return &mcpGatewayResponseWriter{ResponseWriter: w, metadataURL: metadataURL}
}

func (w *mcpGatewayResponseWriter) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.rewriteAuthenticateHeader()
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *mcpGatewayResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *mcpGatewayResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *mcpGatewayResponseWriter) rewriteAuthenticateHeader() {
	values := w.Header().Values("WWW-Authenticate")
	if len(values) == 0 {
		return
	}
	w.Header().Del("WWW-Authenticate")
	for _, value := range values {
		w.Header().Add("WWW-Authenticate", rewriteMCPGatewayResourceMetadata(value, w.metadataURL))
	}
}

func rewriteMCPGatewayResourceMetadata(value, metadataURL string) string {
	for _, quote := range []string{"\"", ""} {
		prefix := "resource_metadata=" + quote
		start := strings.Index(value, prefix)
		if start == -1 {
			continue
		}
		valueStart := start + len(prefix)
		valueEnd := len(value)
		if quote != "" {
			if end := strings.Index(value[valueStart:], quote); end >= 0 {
				valueEnd = valueStart + end
			}
		} else if end := strings.IndexAny(value[valueStart:], ", "); end >= 0 {
			valueEnd = valueStart + end
		}
		return value[:valueStart] + metadataURL + value[valueEnd:]
	}
	return value
}

type mcpDiscoveryCapture struct {
	header http.Header
	status int
	body   strings.Builder
}

func newMCPDiscoveryCapture() *mcpDiscoveryCapture {
	return &mcpDiscoveryCapture{header: make(http.Header)}
}

func (c *mcpDiscoveryCapture) Header() http.Header { return c.header }

func (c *mcpDiscoveryCapture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *mcpDiscoveryCapture) Write(body []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.body.Len()+len(body) > 1<<20 {
		return 0, errors.New("MCP OAuth metadata is too large")
	}
	return c.body.Write(body)
}

func writeCapturedMCPDiscovery(
	w http.ResponseWriter,
	r *http.Request,
	captured *mcpDiscoveryCapture,
	publicResourceURL string,
	publicMetadataURL string,
) {
	status := captured.status
	if status == 0 {
		status = http.StatusOK
	}
	var metadata map[string]any
	if json.Unmarshal([]byte(captured.body.String()), &metadata) != nil || metadata == nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "MCP OAuth metadata is invalid"))
		return
	}
	for name, values := range captured.header {
		for _, value := range values {
			if strings.EqualFold(name, "WWW-Authenticate") {
				value = rewriteMCPGatewayResourceMetadata(value, publicMetadataURL)
			}
			w.Header().Add(name, value)
		}
	}
	metadata["resource"] = publicResourceURL
	w.Header().Set("Content-Type", "application/json")
	httpapi.WriteJSON(w, status, metadata)
}

func (h *Handler) authorizeMCPProxyTarget(ctx context.Context, identity upstreamProxyIdentity, target *url.URL, rawTarget string) bool {
	policyContext, err := h.loadMCPPolicyContext(ctx, identity)
	if err != nil {
		h.logMCPProxyPolicyUnavailable(ctx, identity, err)
		return false
	}
	return h.authorizeLoadedMCPProxyTarget(ctx, policyContext, identity, target, rawTarget)
}

func (h *Handler) logMCPProxyPolicyUnavailable(ctx context.Context, identity upstreamProxyIdentity, err error) {
	h.logger.WarnContext(ctx, "MCP proxy policy denied", "organization_uuid", identity.organizationUUID, "workspace_uuid", identity.workspaceUUID, "code_session_id", identity.codeSessionExternalID, "reason", string(networkpolicy.ReasonPolicyUnavailable), "error", err)
}

func (h *Handler) authorizeLoadedMCPProxyTarget(ctx context.Context, policyContext mcpProxyPolicyContext, identity upstreamProxyIdentity, target *url.URL, rawTarget string) bool {
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
			if errors.Is(err, vaults.ErrInjectionRejected) {
				h.logger.ErrorContext(request.Context(), "inject MCP proxy credentials", "code_session_id", codeSessionID, "host", target.Hostname(), "error", err)
				httpapi.WriteError(writer, request, httpapi.NewError(http.StatusBadGateway, "api_error", vaults.InjectionUnavailablePublicMessage))
				return
			}
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
	target, err := parseMCPProxyURL(rawTarget)
	return target, rawTarget, err
}

func parseMCPProxyURL(rawTarget string) (*url.URL, error) {
	if rawTarget == "" || len(rawTarget) > maxMCPProxyURLBytes {
		return nil, errors.New("MCP URL is invalid")
	}
	target, err := url.Parse(rawTarget)
	if err != nil || !target.IsAbs() || target.Host == "" || target.Hostname() == "" {
		return nil, errors.New("MCP URL must be an absolute HTTP(S) URL")
	}
	target.Scheme = strings.ToLower(target.Scheme)
	if (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.Fragment != "" {
		return nil, errors.New("MCP URL must be an absolute HTTP(S) URL without credentials or fragments")
	}
	return target, nil
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
