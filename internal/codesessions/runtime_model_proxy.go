package codesessions

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

// maxRuntimeModelRequestBodySize 限制已声明 Content-Length 的模型请求。
// 16 MiB 足够容纳 Claude messages 请求中的常规文本、工具定义和编码后的小型附件。
const maxRuntimeModelRequestBodySize = 16 << 20

func (h *Handler) handleRuntimeMessagesProxy(w http.ResponseWriter, r *http.Request) {
	// 在建立上游连接和消费请求体前先鉴权，避免未授权调用占用转发资源。
	if _, ok := h.authenticateRuntimeSession(w, r); !ok {
		return
	}
	targetURL, err := runtimeMessagesEndpoint(h.cfg.AnthropicUpstreamBaseURL)
	if err != nil || strings.TrimSpace(h.cfg.AnthropicUpstreamAPIKey) == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Anthropic upstream is not configured"))
		return
	}
	// 只检查客户端明确声明的长度；请求体不预读、不落盘，直接由上游 transport 流式消费。
	// Content-Length 未知（例如 chunked）时按流透传，不在本层执行额外大小探测。
	if r.ContentLength > maxRuntimeModelRequestBodySize {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusRequestEntityTooLarge, "invalid_request_error", "Request body is too large"))
		return
	}
	defer r.Body.Close()

	upstreamRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, r.Body)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Could not prepare Anthropic upstream request"))
		return
	}
	// NewRequest 无法从通用 io.ReadCloser 推断长度，显式保留原始值；-1 表示未知并使用流式编码。
	upstreamRequest.ContentLength = r.ContentLength
	// 先复制 Anthropic 端到端 headers（版本、beta、content-type 等），再删除逐跳字段和
	// sandbox 凭证，最后注入服务端 API key。这样 code-session token 不会泄漏给模型上游。
	upstreamRequest.Header = r.Header.Clone()
	stripRuntimeProxyHopHeaders(upstreamRequest.Header)
	upstreamRequest.Header.Del("Authorization")
	upstreamRequest.Header.Del("X-Api-Key")
	upstreamRequest.Header.Set("X-Api-Key", strings.TrimSpace(h.cfg.AnthropicUpstreamAPIKey))

	upstreamResponse, err := h.upstreamHTTPClient.Do(upstreamRequest)
	if err != nil {
		if !errors.Is(err, r.Context().Err()) {
			log.Printf("proxy code session model request: %v", err)
		}
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadGateway, "api_error", "Could not reach Anthropic upstream"))
		return
	}
	defer upstreamResponse.Body.Close()

	// 响应一旦提交就不能再改写状态码，因此流错误只记录，不尝试追加第二个 HTTP 错误响应。
	if err := writeRuntimeProxyResponse(w, upstreamResponse); err != nil && r.Context().Err() == nil {
		log.Printf("stream Anthropic upstream response: %v", err)
	}
}

func (h *Handler) authenticateRuntimeSession(w http.ResponseWriter, r *http.Request) (db.CodeSession, bool) {
	// auth.ExtractAPIKey 同时兼容 Claude 使用的 X-Api-Key 与 Bearer 形式。
	// token 必须能够查到现存 code_sessions.external_id，不能退化为“非空即通过”。
	token := auth.ExtractAPIKey(r)
	if token == "" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing code session token"))
		return db.CodeSession{}, false
	}
	record, err := h.db.GetCodeSession(r.Context(), token)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			httpapi.WriteError(w, r, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid code session token"))
			return db.CodeSession{}, false
		}
		log.Printf("authenticate code session runtime: %v", err)
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed"))
		return db.CodeSession{}, false
	}
	return record, true
}

func runtimeMessagesEndpoint(baseURL string) (string, error) {
	// 只允许无 userinfo 的 HTTP(S) 基址，防止配置中嵌入用户名/密码并意外传播。
	// 无论基址是否带业务前缀，都将 /v1/messages 追加到其末尾。
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return "", err
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return "", errors.New("invalid Anthropic upstream base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func stripRuntimeProxyHopHeaders(header http.Header) {
	// RFC 允许 Connection 头动态声明额外逐跳字段，必须先删除这些动态字段，
	// 再处理标准逐跳头；否则代理可能把只对当前连接有效的信息转给下一跳。
	for _, connectionValue := range header.Values("Connection") {
		for _, name := range strings.Split(connectionValue, ",") {
			header.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"Te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
	} {
		header.Del(name)
	}
}

// copyRuntimeProxyHeaders 只把 source 中的端到端响应头复制到 destination，不修改上游原始响应。
// 响应头体积相对 SSE body 很小，先 Clone 再过滤可以避免后续日志、指标或调试代码观察到被删改的 Header。
func copyRuntimeProxyHeaders(destination http.Header, source http.Header) {
	headers := source.Clone()
	stripRuntimeProxyHopHeaders(headers)
	for name, values := range headers {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}

// writeRuntimeProxyResponse 统一管理响应的提交边界：先准备 headers，再一次性提交
// 上游 status，最后流式复制 body。提交后的读写错误由调用方记录，不能再改写响应。
func writeRuntimeProxyResponse(w http.ResponseWriter, response *http.Response) error {
	copyRuntimeProxyHeaders(w.Header(), response.Header)
	w.WriteHeader(response.StatusCode)

	controller := http.NewResponseController(w)
	if err := flushRuntimeProxyResponse(controller); err != nil {
		return err
	}
	writer := runtimeProxyFlushWriter{writer: w, controller: controller}
	_, err := io.CopyBuffer(writer, response.Body, make([]byte, 32<<10))
	return err
}

// runtimeProxyFlushWriter 在每次成功写入后立即 Flush。模型响应以 SSE 为主，
// 因此这里优先保证事件延迟，而不是依赖 net/http 的响应缓冲区攒批发送。
type runtimeProxyFlushWriter struct {
	writer     io.Writer
	controller *http.ResponseController
}

func (w runtimeProxyFlushWriter) Write(data []byte) (int, error) {
	written, err := w.writer.Write(data)
	if err != nil {
		return written, err
	}
	if err := flushRuntimeProxyResponse(w.controller); err != nil {
		return written, err
	}
	return written, nil
}

// ResponseController 可以穿透支持 Unwrap 的中间件 writer。若底层确实不支持 Flush，
// 仍继续普通流式复制；其他 Flush 错误通常表示连接异常，应返回给调用方停止转发。
func flushRuntimeProxyResponse(controller *http.ResponseController) error {
	err := controller.Flush()
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
