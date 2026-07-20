package messages

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

// maxRequestBodyBytes 是流式读取上限；MaxBytesReader 不会据此预分配 32 MiB 内存。
const maxRequestBodyBytes int64 = 32 << 20

// requestHeadersToRemove 包含 hop-by-hop header、调用方凭证和不可由 sandbox 伪造的租户 header。
var requestHeadersToRemove = map[string]struct{}{
	"Authorization":       {},
	"Connection":          {},
	"Cookie":              {},
	"Host":                {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
	"X-Api-Key":           {},
	"X-Organization-Uuid": {},
	"X-Workspace-Id":      {},
}

// responseHeadersToRemove 防止把仅对上游连接有效的 hop-by-hop header 返回给客户端。
var responseHeadersToRemove = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Proxy-Connection":    {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// Handler 将 Anthropic Messages 请求流式转发到真实上游，不解析或持久化请求正文。
type Handler struct {
	cfg    config.Config
	client *http.Client
}

// flushingResponseWriter 在每次复制一块响应后主动 flush，避免 SSE 被 net/http 缓冲。
type flushingResponseWriter struct {
	writer     io.Writer
	controller *http.ResponseController
}

// NewHandler 创建复用连接池的 Messages 代理 handler。
func NewHandler(cfg config.Config) *Handler {
	return &Handler{cfg: cfg, client: &http.Client{Transport: newProxyTransport()}}
}

func newProxyTransport() http.RoundTripper {
	// 复用默认 Transport 的代理、TLS 和连接池配置，只提高同主机空闲连接容量。
	// Client 不设置整体 Timeout，SSE 生命周期由请求 context 和上游关闭控制。
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		cloned.MaxIdleConnsPerHost = 32
		return cloned
	}
	return &http.Transport{MaxIdleConnsPerHost: 32}
}

// Create 处理 canonical POST /v1/messages，并以有界内存完成请求和响应的双向流式转发。
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	// 鉴权已由 API middleware 完成；这里只确认 Principal 存在，避免 handler 被错误地裸挂载。
	_, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key"))
		return
	}
	if strings.TrimSpace(h.cfg.AnthropicUpstream.APIKey) == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, "api_error", "anthropic_upstream.api_key is required for Messages"))
		return
	}
	if r.ContentLength > maxRequestBodyBytes {
		writeRequestTooLarge(w, r)
		return
	}
	target, err := messagesEndpoint(h.cfg.AnthropicUpstream.BaseURL, r.URL.RawQuery)
	if err != nil {
		log.Printf("build messages upstream endpoint: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable"))
		return
	}
	// MaxBytesReader 包装原始网络流，不缓存完整 JSON；未知 Content-Length 超限时会在读取中报错。
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	upstreamRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, r.Body)
	if err != nil {
		log.Printf("build messages upstream request: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable"))
		return
	}
	upstreamRequest.ContentLength = r.ContentLength
	// 先清除客户端鉴权和租户 header，再注入只存在于 OMA 服务端的真实上游 key。
	upstreamRequest.Header = sanitizedRequestHeaders(r.Header)
	upstreamRequest.Header.Set("X-Api-Key", strings.TrimSpace(h.cfg.AnthropicUpstream.APIKey))
	upstreamResponse, err := h.client.Do(upstreamRequest)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeRequestTooLarge(w, r)
			return
		}
		log.Printf("proxy messages upstream request: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Messages upstream is unavailable"))
		return
	}
	defer upstreamResponse.Body.Close()
	if err := writeProxyResponse(w, upstreamResponse); err != nil && r.Context().Err() == nil {
		log.Printf("stream Messages upstream response: %v", err)
	}
}

func writeRequestTooLarge(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "request_too_large", "Request body exceeds maximum size"))
}

func messagesEndpoint(baseURL string, rawQuery string) (string, error) {
	// base URL 可以带部署前缀，但最终资源始终规范化为其下的 /v1/messages。
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("messages upstream base URL must be absolute")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	parsed.RawPath = ""
	parsed.RawQuery = rawQuery
	return parsed.String(), nil
}

func sanitizedRequestHeaders(source http.Header) http.Header {
	// 先按 Connection header 声明删除动态 hop-by-hop 字段，再删除固定敏感字段。
	headers := source.Clone()
	removeConnectionHeaders(headers)
	for name := range requestHeadersToRemove {
		headers.Del(name)
	}
	return headers
}

func copyResponseHeaders(destination http.Header, source http.Header) {
	connectionHeaders := source.Clone()
	removeConnectionHeaders(connectionHeaders)
	for name, values := range connectionHeaders {
		if _, remove := responseHeadersToRemove[http.CanonicalHeaderKey(name)]; remove {
			continue
		}
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

func removeConnectionHeaders(headers http.Header) {
	// RFC 允许 Connection 列出任意仅对当前连接有效的 header，不能只维护固定名单。
	for _, value := range headers.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			headers.Del(strings.TrimSpace(name))
		}
	}
}

func prepareResponseHeaders(headers http.Header) {
	contentType := headers.Get("Content-Type")
	if contentType == "" {
		contentType = "application/json"
		headers.Set("Content-Type", contentType)
	}
	if !strings.Contains(strings.ToLower(contentType), "text/event-stream") {
		return
	}
	// 同时关闭应用层缓存提示和常见反向代理缓冲，保证事件尽快到达 Claude Code。
	if headers.Get("Cache-Control") == "" {
		headers.Set("Cache-Control", "no-cache")
	}
	headers.Set("X-Accel-Buffering", "no")
}

func writeProxyResponse(w http.ResponseWriter, response *http.Response) error {
	copyResponseHeaders(w.Header(), response.Header)
	prepareResponseHeaders(w.Header())
	w.WriteHeader(response.StatusCode)
	controller := http.NewResponseController(w)
	if err := flushProxyResponse(controller); err != nil {
		return err
	}
	// 固定 32 KiB 网络缓冲，与 32 MiB 请求上限无关；响应不会被完整读入内存。
	writer := flushingResponseWriter{writer: w, controller: controller}
	_, err := io.CopyBuffer(writer, response.Body, make([]byte, 32*1024))
	return err
}

func (w flushingResponseWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if err != nil {
		return written, err
	}
	if err := flushProxyResponse(w.controller); err != nil {
		return written, err
	}
	return written, nil
}

func flushProxyResponse(controller *http.ResponseController) error {
	err := controller.Flush()
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
