package codesessions

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"
	"golang.org/x/net/websocket"
)

func (h *Handler) serveUpstreamProxyHTTP(connection *websocket.Conn, identity upstreamProxyIdentity, token string, first []byte) {
	stream := newUpstreamProxyChunkConn(connection)
	stream.readBuffer = first
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			h.forwardUpstreamProxyHTTP(w, request, identity, token)
		}),
		ReadHeaderTimeout: upstreamProxyMITMHeaderTimeout,
		IdleTimeout:       upstreamProxyMITMIdleTimeout,
		MaxHeaderBytes:    maxUpstreamProxyConnectHeadBytes,
	}
	_ = server.Serve(newUpstreamProxySingleConnectionListener(stream))
}

func (h *Handler) forwardUpstreamProxyHTTP(w http.ResponseWriter, request *http.Request, identity upstreamProxyIdentity, token string) {
	request.Close = true
	w.Header().Set("Connection", "close")
	authRequest := &http.Request{Header: http.Header{"Authorization": request.Header.Values("Proxy-Authorization")}}
	sessionID, suppliedToken, ok := authRequest.BasicAuth()
	if !ok || len(request.Header.Values("Proxy-Authorization")) != 1 || !upstreamProxyCredentialsMatch(upstreamProxyConnectRequest{SessionID: sessionID, Token: suppliedToken}, identity.codeSessionExternalID, token) {
		http.Error(w, "Proxy authentication required", http.StatusProxyAuthRequired)
		return
	}
	if request.Method == http.MethodConnect || request.URL.Scheme != "http" || request.URL.Host == "" || !strings.EqualFold(request.Host, request.URL.Host) {
		http.Error(w, "Invalid HTTP proxy target", http.StatusBadRequest)
		return
	}
	policy, err := h.loadUpstreamPolicyContext(request.Context(), identity)
	if err != nil || !policy.policy.AuthorizeHTTPURL(request.URL).Allow {
		http.Error(w, "HTTP proxy target is not allowed", http.StatusForbidden)
		return
	}
	host := request.URL.Hostname()
	port := request.URL.Port()
	if port == "" {
		port = "80"
	}
	resolved, err := resolveProxyTarget(request.Context(), net.JoinHostPort(host, port), h.cfg.CodeSession.UpstreamProxyDisableSSRFProtection, false)
	if err != nil {
		http.Error(w, "HTTP proxy target is not allowed", http.StatusForbidden)
		return
	}
	if h.cfg.CodeSession.UpstreamProxyMITMEnabled && h.mitmEgress != nil {
		err = h.mitmEgress.SubstituteHTTP(request.Context(), vaults.EgressSession{CodeSessionExternalID: identity.codeSessionExternalID, OrganizationUUID: identity.organizationUUID, WorkspaceUUID: identity.workspaceUUID}, request, host, port)
		if err != nil {
			http.Error(w, "Vault substitution rejected", http.StatusBadGateway)
			return
		}
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return h.upstreamProxy.dial(ctx, resolved)
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: upstreamProxyMITMHeaderTimeout,
	}
	defer transport.CloseIdleConnections()
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			proxyRequest.Out.Header.Del("Proxy-Authorization")
			proxyRequest.Out.Header.Del("Proxy-Connection")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "HTTP proxy request failed", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, request)
}
