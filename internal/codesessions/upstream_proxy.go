package codesessions

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"golang.org/x/net/websocket"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	// chunk 上限与 Claude relay 的 512 KiB 分片约定一致；额外 16 字节留给 protobuf tag 和 varint 长度。
	maxUpstreamProxyChunkBytes = 512 << 10
	// 旧解码器最多消费 5 字节的长度 varint；保留该边界，避免迁移到 protowire 后放宽畸形输入的接受范围。
	maxUpstreamProxyChunkLengthVarintBytes = 5
	// CONNECT 首包只允许小型 HTTP head，避免客户端在鉴权和目标校验前占用大量内存。
	maxUpstreamProxyConnectHeadBytes = 8 << 10
	upstreamProxyDialTimeout         = 10 * time.Second
	// UpstreamProxyChunk 对应 message UpstreamProxyChunk { bytes data = 1; }。
	upstreamProxyChunkDataFieldNumber protowire.Number = 1
)

// blockedUpstreamProxyNetworks 是默认 SSRF 边界：拒绝本机、私网、链路本地、
// CGNAT、benchmark/fake-IP、文档保留段、组播及 IPv6 ULA 等非公网目标。
// 198.18.0.0/15 在标准语义中是 benchmark 网段，但本地 Clash/TUN 也可能将其用作
// fake-IP；此冲突只能通过显式的临时配置开关绕过，不能改变默认安全边界。
var blockedUpstreamProxyNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
	netip.MustParsePrefix("2001:db8::/32"),
}

type upstreamProxyConnectRequest struct {
	// Target 保留 CONNECT 行中的 host:port；SessionID 与 Token 来自 Basic 凭证的两部分。
	Target    string
	SessionID string
	Token     string
}

type upstreamProxyTargetError struct {
	// status 是通过 protobuf chunk 回给本地 relay 的 HTTP 状态，而不是 WebSocket 握手状态。
	status int
	cause  error
}

func (e *upstreamProxyTargetError) Error() string {
	return e.cause.Error()
}

func (h *Handler) handleUpstreamProxyCACertificate(w http.ResponseWriter, r *http.Request) {
	// Claude 下载 CA 时不会携带 session token；CA 是公开材料，因此该端点有意不鉴权。
	authority, err := h.loadUpstreamProxyCA()
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Could not prepare upstream proxy CA certificate"))
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(authority.certificatePEM)
}

func (h *Handler) handleUpstreamProxyWebSocket(w http.ResponseWriter, r *http.Request) {
	// 必须在升级协议前完成 Bearer/X-Api-Key 鉴权，避免未授权客户端占用长连接。
	codeSessionID, sessionIngressToken, ok := h.authenticateRuntimeSession(w, r)
	if !ok {
		return
	}
	// relay 使用二进制 UpstreamProxyChunk wire format。升级前固定 Content-Type，
	// 让协议不匹配的客户端在占用长连接前得到明确的 415，而不是进入后续帧解析。
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/proto" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnsupportedMediaType, "invalid_request_error", "Content-Type must be application/proto"))
		return
	}
	server := websocket.Server{
		Handshake: func(*websocket.Config, *http.Request) error { return nil },
		Handler: func(connection *websocket.Conn) {
			connection.MaxPayloadBytes = maxUpstreamProxyChunkBytes + 16
			// CONNECT Basic 用户名绑定 code-session ID，密码必须与 WebSocket Bearer 是同一个 ingress JWT。
			h.serveUpstreamProxyTunnel(connection, codeSessionID, sessionIngressToken)
		},
	}
	server.ServeHTTP(w, r)
}

func (h *Handler) serveUpstreamProxyTunnel(connection *websocket.Conn, sessionID string, sessionIngressToken string) {
	// 首个 WebSocket message 必须完整承载 CONNECT head。后续 message 才视为原始 TLS 字节流。
	firstChunk, err := receiveUpstreamProxyChunk(connection)
	if err != nil || len(firstChunk) > maxUpstreamProxyConnectHeadBytes {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadRequest)
		return
	}
	connectRequest, err := parseUpstreamProxyConnectRequest(firstChunk)
	if err != nil {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadRequest)
		return
	}
	// WebSocket Bearer 鉴权之后仍校验 CONNECT Basic 的用户名和密码，确保 relay 不能把
	// 一个 session 的已升级连接改造成另一个 session 的出口。
	if !upstreamProxyCredentialsMatch(connectRequest, sessionID, sessionIngressToken) {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusProxyAuthRequired)
		return
	}
	// 先解析并锁定目标 IP，再拨号该 IP；不要在校验后重新按域名解析，否则会留下 DNS rebinding 窗口。
	resolvedTarget, err := resolveUpstreamProxyTarget(
		connection.Request().Context(),
		connectRequest.Target,
		h.cfg.CodeSession.UpstreamProxyDisableSSRFProtection,
	)
	if err != nil {
		status := http.StatusBadGateway
		var targetError *upstreamProxyTargetError
		if errors.As(err, &targetError) {
			status = targetError.status
		}
		_ = sendUpstreamProxyHTTPStatus(connection, status)
		return
	}
	if h.cfg.CodeSession.UpstreamProxyMITMEnabled {
		h.serveUpstreamProxyMITM(connection, connectRequest.Target, resolvedTarget)
		return
	}
	targetConnection, err := h.upstreamProxy.dial(connection.Request().Context(), resolvedTarget)
	if err != nil {
		_ = sendUpstreamProxyHTTPStatus(connection, http.StatusBadGateway)
		return
	}
	defer targetConnection.Close()
	if err := sendUpstreamProxyHTTPStatus(connection, http.StatusOK); err != nil {
		return
	}

	// 两个方向独立阻塞复制；任一方向结束后设置 deadline，立即唤醒另一侧并释放 goroutine。
	result := make(chan error, 2)
	go func() { result <- copyUpstreamProxyWebSocketToTarget(connection, targetConnection) }()
	go func() { result <- copyUpstreamProxyTargetToWebSocket(targetConnection, connection) }()
	<-result
	_ = targetConnection.SetDeadline(time.Now())
	_ = connection.SetDeadline(time.Now())
}

func parseUpstreamProxyConnectRequest(data []byte) (upstreamProxyConnectRequest, error) {
	// 仅接受一个完整、以 CRLFCRLF 结束的 HTTP/1.x CONNECT head；不容忍多个认证头，
	// 避免不同代理层对重复 header 采用不同解释造成 request smuggling/鉴权歧义。
	if len(data) == 0 || len(data) > maxUpstreamProxyConnectHeadBytes || !strings.HasSuffix(string(data), "\r\n\r\n") {
		return upstreamProxyConnectRequest{}, errors.New("invalid CONNECT request")
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\r\n\r\n"), "\r\n")
	requestLine := strings.Fields(lines[0])
	if len(requestLine) != 3 || !strings.EqualFold(requestLine[0], http.MethodConnect) ||
		(requestLine[2] != "HTTP/1.1" && requestLine[2] != "HTTP/1.0") {
		return upstreamProxyConnectRequest{}, errors.New("invalid CONNECT request line")
	}
	authorization := ""
	for _, line := range lines[1:] {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return upstreamProxyConnectRequest{}, errors.New("invalid CONNECT header")
		}
		if strings.EqualFold(strings.TrimSpace(name), "Proxy-Authorization") {
			if authorization != "" {
				return upstreamProxyConnectRequest{}, errors.New("duplicate proxy authorization")
			}
			authorization = strings.TrimSpace(value)
		}
	}
	sessionID, token, err := parseUpstreamProxyAuthorization(authorization)
	if err != nil {
		return upstreamProxyConnectRequest{}, err
	}
	return upstreamProxyConnectRequest{Target: requestLine[1], SessionID: sessionID, Token: token}, nil
}

func parseUpstreamProxyAuthorization(value string) (string, string, error) {
	// relay 固定编码 Basic base64(sessionID:SessionIngressAuthToken)。这里只拆分第一个冒号，
	// token 的具体合法性由外层与已认证 session 做常量时间比较决定。
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Basic") {
		return "", "", errors.New("missing proxy authorization")
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", errors.New("invalid proxy authorization")
	}
	sessionID, token, ok := strings.Cut(string(decoded), ":")
	if !ok || sessionID == "" || token == "" {
		return "", "", errors.New("invalid proxy credentials")
	}
	return sessionID, token, nil
}

func upstreamProxyCredentialsMatch(request upstreamProxyConnectRequest, expectedSessionID string, expectedToken string) bool {
	// 对等长凭证使用常量时间比较，避免在首个不同字节处提前返回。
	// code-session external ID 的长度不作为秘密，因此长度不一致仍会立即失败。
	return subtle.ConstantTimeCompare([]byte(request.SessionID), []byte(expectedSessionID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(request.Token), []byte(expectedToken)) == 1
}

func resolveUpstreamProxyTarget(ctx context.Context, target string, disableSSRFProtection bool) (string, error) {
	// 即使临时关闭 SSRF 地址过滤，也继续限制为 443：当前协议只支持 HTTPS CONNECT，
	// 不能把该开关扩展成任意端口的通用 TCP 转发器。
	host, port, err := net.SplitHostPort(target)
	if err != nil || strings.TrimSpace(host) == "" {
		return "", &upstreamProxyTargetError{status: http.StatusBadRequest, cause: errors.New("invalid CONNECT target")}
	}
	if port != "443" {
		return "", &upstreamProxyTargetError{status: http.StatusForbidden, cause: errors.New("only HTTPS targets are allowed")}
	}
	// IP 字面量无需 DNS；默认必须是公网地址，临时开关开启时才原样放行。
	if parsed, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !disableSSRFProtection && !publicUpstreamProxyIP(parsed) {
			return "", &upstreamProxyTargetError{status: http.StatusForbidden, cause: errors.New("target address is not public")}
		}
		return net.JoinHostPort(parsed.Unmap().String(), port), nil
	}
	// 域名使用服务端系统 resolver。Clash/TUN 的 fake-IP DNS 可能返回 198.18/15，
	// 因此本地排障模式允许选择这样的首个有效地址；默认模式仍逐个寻找安全公网地址。
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", &upstreamProxyTargetError{status: http.StatusBadGateway, cause: err}
	}
	for _, address := range addresses {
		parsed, ok := netip.AddrFromSlice(address.IP)
		if ok && (disableSSRFProtection || publicUpstreamProxyIP(parsed)) {
			return net.JoinHostPort(parsed.Unmap().String(), port), nil
		}
	}
	return "", &upstreamProxyTargetError{status: http.StatusForbidden, cause: errors.New("target has no public address")}
}

func publicUpstreamProxyIP(address netip.Addr) bool {
	// Unmap 将 IPv4-mapped IPv6 统一成 IPv4，保证 ::ffff:127.0.0.1 等形式不能绕过网段检查。
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return false
	}
	for _, network := range blockedUpstreamProxyNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return true
}

func dialUpstreamProxyTarget(ctx context.Context, target string) (net.Conn, error) {
	// target 已经是校验后的 IP:443，直接 dial 可避免再次 DNS 解析。
	dialer := net.Dialer{Timeout: upstreamProxyDialTimeout, KeepAlive: 30 * time.Second}
	return dialer.DialContext(ctx, "tcp", target)
}

func sendUpstreamProxyHTTPStatus(connection *websocket.Conn, status int) error {
	// HTTP 状态必须继续包在 UpstreamProxyChunk 中；直接写 WebSocket text frame 与 relay 协议不兼容。
	reason := http.StatusText(status)
	if status == http.StatusOK {
		reason = "Connection Established"
	}
	response := fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, reason)
	if status == http.StatusProxyAuthRequired {
		response += "Proxy-Authenticate: Basic realm=\"ccrv2\"\r\n"
	}
	response += "\r\n"
	return websocket.Message.Send(connection, encodeUpstreamProxyChunk([]byte(response)))
}

// encodeUpstreamProxyChunk 编码等价于以下 protobuf 消息的 wire layout：
// message UpstreamProxyChunk { bytes data = 1; }
// field 1 的 bytes tag 是 (1 << 3) | 2 = 0x0a，后接数据长度 varint 与原始字节；
// 例如 data="ABC" 会编码为 0a 03 41 42 43。
func encodeUpstreamProxyChunk(data []byte) []byte {
	// 使用 protobuf 官方 wire 编码器生成 field 1 的 tag、长度 varint 和原始数据。
	// 预先按精确 wire 大小分配容量，整个消息只需要一次目标切片分配。
	encoded := make(
		[]byte,
		0,
		protowire.SizeTag(upstreamProxyChunkDataFieldNumber)+protowire.SizeBytes(len(data)),
	)
	encoded = protowire.AppendTag(encoded, upstreamProxyChunkDataFieldNumber, protowire.BytesType)
	return protowire.AppendBytes(encoded, data)
}

func receiveUpstreamProxyChunk(connection *websocket.Conn) ([]byte, error) {
	var encoded []byte
	if err := websocket.Message.Receive(connection, &encoded); err != nil {
		return nil, err
	}
	return decodeUpstreamProxyChunk(encoded)
}

func decodeUpstreamProxyChunk(encoded []byte) ([]byte, error) {
	// 空 WebSocket payload 被视为 keepalive；非空 payload 必须恰好包含一个 field 1，
	// 既拒绝截断，也拒绝尾随字段，避免协议双方对边界产生不同理解。
	if len(encoded) == 0 {
		return []byte{}, nil
	}
	// fieldNumber：字段编号，这里是 1
	// wireType：wire 类型，这里是 protowire.BytesType，值为 2
	// tagBytes：tag 占用的字节数，这里是 1
	fieldNumber, wireType, tagBytes := protowire.ConsumeTag(encoded)
	if tagBytes < 0 ||
		fieldNumber != upstreamProxyChunkDataFieldNumber ||
		wireType != protowire.BytesType ||
		tagBytes != protowire.SizeTag(upstreamProxyChunkDataFieldNumber) {
		return nil, errors.New("invalid upstream proxy chunk tag")
	}

	// ConsumeBytes 返回的 consumedBytes 同时包含长度 varint 和 data；data 本身是 encoded 的
	// 子切片，不发生额外复制。成功解析后仍要求完整消费 WebSocket payload，继续拒绝未知或重复字段。
	data, consumedBytes := protowire.ConsumeBytes(encoded[tagBytes:])
	if consumedBytes < 0 {
		return nil, errors.New("invalid upstream proxy chunk length")
	}
	lengthVarintBytes := consumedBytes - len(data)
	if lengthVarintBytes > maxUpstreamProxyChunkLengthVarintBytes ||
		len(data) > maxUpstreamProxyChunkBytes ||
		tagBytes+consumedBytes != len(encoded) {
		return nil, errors.New("invalid upstream proxy chunk length")
	}
	return data, nil
}

func copyUpstreamProxyWebSocketToTarget(source *websocket.Conn, destination net.Conn) error {
	// relay 发出的空 data chunk 是保活信号，不应写入目标 TCP 流。
	for {
		chunk, err := receiveUpstreamProxyChunk(source)
		if err != nil {
			return err
		}
		if len(chunk) == 0 {
			continue
		}
		// net.Conn.Write 允许短写，循环消费剩余切片，保证 TLS record 不丢字节。
		for len(chunk) > 0 {
			written, writeErr := destination.Write(chunk)
			if writeErr != nil {
				return writeErr
			}
			if written == 0 {
				return io.ErrShortWrite
			}
			chunk = chunk[written:]
		}
	}
}

func copyUpstreamProxyTargetToWebSocket(source net.Conn, destination *websocket.Conn) error {
	// 每次 TCP read 都编码成独立 protobuf binary message；32 KiB 小于协议 512 KiB 上限。
	buffer := make([]byte, 32<<10)
	for {
		readBytes, err := source.Read(buffer)
		if readBytes > 0 {
			if sendErr := websocket.Message.Send(destination, encodeUpstreamProxyChunk(buffer[:readBytes])); sendErr != nil {
				return sendErr
			}
		}
		if err != nil {
			return err
		}
	}
}
