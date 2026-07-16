package codesessions

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

const (
	upstreamProxyTLSHandshakeTimeout = 10 * time.Second
	upstreamProxyMITMHeaderTimeout   = 15 * time.Second
	upstreamProxyMITMIdleTimeout     = 2 * time.Minute
)

// serveUpstreamProxyMITM 把 CCR 的 framed WebSocket 转换为 TLS server，并用独立、严格校验的 TLS client 连接真实网站。
// 客户端侧只协商 HTTP/1.1，避免在尚未实现 HTTP/2 server 语义时错误宣告 h2；ReverseProxy 仍支持流式响应和 HTTP Upgrade。
func (h *Handler) serveUpstreamProxyMITM(connection *websocket.Conn, target string, resolvedTarget string) {
	authority, err := h.loadUpstreamProxyCA()
	if err != nil {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadGateway)
		return
	}
	targetHost, _, err := net.SplitHostPort(target)
	if err != nil {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadRequest)
		return
	}
	targetHost = canonicalUpstreamProxyHostname(targetHost)
	serverTLSConfig, err := newUpstreamProxyMITMServerTLSConfig(authority, targetHost, time.Now().UTC())
	if err != nil {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadGateway)
		return
	}
	// newUpstreamProxyRuntime 总是安装生产拨号实现，因此无需 nil 兜底；dialTLS 同时用于首条握手与后续 Transport 拨号。
	dialTLS := h.upstreamProxy.dialTLS
	upstreamConnection, err := dialTLS(connection.Request().Context(), resolvedTarget, targetHost)
	if err != nil {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadGateway)
		return
	}
	dialer := newUpstreamProxyMITMTLSDialer(upstreamConnection, dialTLS, resolvedTarget, targetHost)
	defer dialer.Close()
	transport := newUpstreamProxyMITMTransport(dialer)
	defer transport.CloseIdleConnections()

	// 只有 CA、leaf 和真实上游 TLS 都准备成功后才向本地 relay 返回 200，防止客户端进入一个必然失败的 TLS 隧道。
	if err := sendUpstreamProxyHTTPStatus(connection, http.StatusOK); err != nil {
		return
	}
	clientConnection := tls.Server(newUpstreamProxyChunkConn(connection), serverTLSConfig)
	defer clientConnection.Close()
	handshakeContext, cancel := context.WithTimeout(connection.Request().Context(), upstreamProxyTLSHandshakeTimeout)
	defer cancel()
	if err := clientConnection.HandshakeContext(handshakeContext); err != nil {
		return
	}
	_ = serveUpstreamProxyMITMHTTP(clientConnection, transport, target, targetHost)
}

func newUpstreamProxyMITMServerTLSConfig(authority *upstreamProxyCertificateAuthority, targetHost string, now time.Time) (*tls.Config, error) {
	leafCertificate, err := authority.certificateForHost(targetHost, now)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"http/1.1"},
		Certificates: []tls.Certificate{*leafCertificate},
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			// CONNECT 目标、SNI 与后续 HTTP Host 必须指向同一主机，不能借一个已授权隧道访问另一个域名。
			serverName := canonicalUpstreamProxyHostname(hello.ServerName)
			if serverName != "" && serverName != targetHost {
				return nil, fmt.Errorf("TLS server name %q does not match CONNECT target", hello.ServerName)
			}
			return nil, nil
		},
	}, nil
}

func dialUpstreamProxyTLSTarget(ctx context.Context, resolvedTarget string, serverName string) (net.Conn, error) {
	connection, err := dialUpstreamProxyTarget(ctx, resolvedTarget)
	if err != nil {
		return nil, err
	}
	tlsConnection := tls.Client(connection, &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: canonicalUpstreamProxyHostname(serverName),
		NextProtos: []string{"http/1.1"},
	})
	handshakeContext, cancel := context.WithTimeout(ctx, upstreamProxyTLSHandshakeTimeout)
	defer cancel()
	if err := tlsConnection.HandshakeContext(handshakeContext); err != nil {
		_ = connection.Close()
		return nil, err
	}
	return tlsConnection, nil
}

type upstreamProxyMITMTLSDialer struct {
	mu             sync.Mutex
	first          net.Conn
	dial           func(context.Context, string, string) (net.Conn, error)
	resolvedTarget string
	serverName     string
}

func newUpstreamProxyMITMTLSDialer(first net.Conn, dial func(context.Context, string, string) (net.Conn, error), resolvedTarget string, serverName string) *upstreamProxyMITMTLSDialer {
	return &upstreamProxyMITMTLSDialer{first: first, dial: dial, resolvedTarget: resolvedTarget, serverName: serverName}
}

func (d *upstreamProxyMITMTLSDialer) DialTLSContext(ctx context.Context, _, _ string) (net.Conn, error) {
	// CONNECT 建立阶段已经完成了第一条真实上游 TLS 握手；把它交给 Transport，既能提前失败，也避免重复拨号。
	d.mu.Lock()
	if d.first != nil {
		connection := d.first
		d.first = nil
		d.mu.Unlock()
		return connection, nil
	}
	d.mu.Unlock()
	return d.dial(ctx, d.resolvedTarget, d.serverName)
}

func (d *upstreamProxyMITMTLSDialer) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.first != nil {
		_ = d.first.Close()
		d.first = nil
	}
}

func newUpstreamProxyMITMTransport(dialer *upstreamProxyMITMTLSDialer) *http.Transport {
	return &http.Transport{
		Proxy:               nil,
		DialTLSContext:      dialer.DialTLSContext,
		ForceAttemptHTTP2:   false,
		DisableCompression:  true,
		MaxIdleConns:        1,
		MaxIdleConnsPerHost: 1,
		// IdleConnTimeout 只回收已经空闲的 keep-alive 连接；真实目标完成 TLS 后如果一直
		// 不返回 HTTP 响应头，请求仍属于活跃状态。单独限制响应头等待时间，避免恶意或
		// 故障目标长期占用 tunnel、连接和 goroutine，同时不限制随后可能长期存在的 SSE body。
		ResponseHeaderTimeout: upstreamProxyMITMHeaderTimeout,
		IdleConnTimeout:       upstreamProxyMITMIdleTimeout,
	}
}

func serveUpstreamProxyMITMHTTP(connection net.Conn, transport http.RoundTripper, target string, targetHost string) error {
	targetURL := &url.URL{Scheme: "https", Host: target}
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(targetURL)
			request.Out.Host = targetHost
			// Proxy-Authorization 只属于本地 CONNECT 鉴权，绝不能穿透到真实网站。
			request.Out.Header.Del("Proxy-Authorization")
			request.Out.Header.Del("Proxy-Connection")
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodConnect {
			http.Error(w, "nested CONNECT is not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !upstreamProxyHTTPRequestMatchesTarget(request, targetHost) {
			http.Error(w, "request host does not match CONNECT target", http.StatusMisdirectedRequest)
			return
		}
		proxy.ServeHTTP(w, request)
	})
	listener := newUpstreamProxySingleConnectionListener(connection)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: upstreamProxyMITMHeaderTimeout,
		IdleTimeout:       upstreamProxyMITMIdleTimeout,
		MaxHeaderBytes:    maxUpstreamProxyConnectHeadBytes * 8,
	}
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func upstreamProxyHTTPRequestMatchesTarget(request *http.Request, targetHost string) bool {
	if canonicalUpstreamProxyHTTPHost(request.Host) != targetHost {
		return false
	}
	// TLS 隧道内通常使用 origin-form（URL.Scheme/Host 为空）；若客户端发送 absolute-form，
	// 只接受 https 且仍要求 URL host 与 CONNECT 目标相同，避免借已授权隧道转发明文语义或跨域请求。
	if request.URL.Scheme != "" && !strings.EqualFold(request.URL.Scheme, "https") {
		return false
	}
	return request.URL.Host == "" || canonicalUpstreamProxyHTTPHost(request.URL.Host) == targetHost
}

func canonicalUpstreamProxyHTTPHost(authority string) string {
	authority = strings.TrimSpace(authority)
	if host, port, err := net.SplitHostPort(authority); err == nil {
		if port != "443" {
			return ""
		}
		return canonicalUpstreamProxyHostname(host)
	}
	return canonicalUpstreamProxyHostname(authority)
}

// upstreamProxyChunkConn 将每个 UpstreamProxyChunk 重新拼成连续字节流，使 crypto/tls 不感知 WebSocket 消息边界。
type upstreamProxyChunkConn struct {
	connection *websocket.Conn
	readMu     sync.Mutex
	writeMu    sync.Mutex
	readBuffer []byte
}

func newUpstreamProxyChunkConn(connection *websocket.Conn) *upstreamProxyChunkConn {
	return &upstreamProxyChunkConn{connection: connection}
}

func (c *upstreamProxyChunkConn) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.readBuffer) == 0 {
		chunk, err := receiveUpstreamProxyChunk(c.connection)
		if err != nil {
			return 0, err
		}
		if len(chunk) != 0 {
			c.readBuffer = chunk
		}
	}
	readBytes := copy(buffer, c.readBuffer)
	c.readBuffer = c.readBuffer[readBytes:]
	return readBytes, nil
}

func (c *upstreamProxyChunkConn) Write(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for offset := 0; offset < len(data); offset += maxUpstreamProxyChunkBytes {
		end := min(offset+maxUpstreamProxyChunkBytes, len(data))
		if err := websocket.Message.Send(c.connection, encodeUpstreamProxyChunk(data[offset:end])); err != nil {
			return offset, err
		}
	}
	return len(data), nil
}

func (c *upstreamProxyChunkConn) Close() error { return c.connection.Close() }

// x/net/websocket 的服务端 RemoteAddr 可能包含没有 URL 的 websocket.Addr；net/http 会调用
// Addr.String，因此这里提供稳定的逻辑地址，避免把消息通道误当成底层 TCP 地址。
func (c *upstreamProxyChunkConn) LocalAddr() net.Addr  { return upstreamProxyTunnelAddr("ccrv2-server") }
func (c *upstreamProxyChunkConn) RemoteAddr() net.Addr { return upstreamProxyTunnelAddr("ccrv2-relay") }

func (c *upstreamProxyChunkConn) SetDeadline(t time.Time) error { return c.connection.SetDeadline(t) }
func (c *upstreamProxyChunkConn) SetReadDeadline(t time.Time) error {
	return c.connection.SetReadDeadline(t)
}
func (c *upstreamProxyChunkConn) SetWriteDeadline(t time.Time) error {
	return c.connection.SetWriteDeadline(t)
}

type upstreamProxyTunnelAddr string

func (a upstreamProxyTunnelAddr) Network() string { return "ccrv2" }
func (a upstreamProxyTunnelAddr) String() string  { return string(a) }

type upstreamProxySingleConnectionListener struct {
	connection net.Conn
	mu         sync.Mutex
	accepted   bool
	closeOnce  sync.Once
	closed     chan struct{}
}

type upstreamProxyObservedConn struct {
	net.Conn
	onClose func()
}

func newUpstreamProxySingleConnectionListener(connection net.Conn) *upstreamProxySingleConnectionListener {
	return &upstreamProxySingleConnectionListener{connection: connection, closed: make(chan struct{})}
}

func (l *upstreamProxySingleConnectionListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	if !l.accepted {
		l.accepted = true
		connection := &upstreamProxyObservedConn{Conn: l.connection, onClose: l.signalClosed}
		l.mu.Unlock()
		return connection, nil
	}
	l.mu.Unlock()
	<-l.closed
	return nil, net.ErrClosed
}

func (l *upstreamProxySingleConnectionListener) Close() error {
	l.signalClosed()
	return l.connection.Close()
}

func (l *upstreamProxySingleConnectionListener) Addr() net.Addr { return l.connection.LocalAddr() }

func (l *upstreamProxySingleConnectionListener) signalClosed() {
	l.closeOnce.Do(func() { close(l.closed) })
}

func (c *upstreamProxyObservedConn) Close() error {
	c.onClose()
	return c.Conn.Close()
}
