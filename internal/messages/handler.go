package messages

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

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

// Handler 校验顶层 model 后按原样转发请求体。
type Handler struct {
	database *db.DB
	secrets  *secrets.Service
	client   *http.Client
	logger   *slog.Logger
}

// flushingResponseWriter 在每次复制一块响应后主动 flush，避免 SSE 被 net/http 缓冲。
type flushingResponseWriter struct {
	writer     io.Writer
	controller *http.ResponseController
}

// NewHandler 创建复用连接池的 Messages 代理 handler。
func NewHandler(database *db.DB, secretService *secrets.Service, logger *slog.Logger) *Handler {
	logger = logging.LoggerOrDefault(logger)
	return &Handler{database: database, secrets: secretService, client: llmproviders.NewHTTPClient(0), logger: logger}
}

// Create 处理 canonical POST /v1/messages，并以有界内存完成请求校验和响应流式转发。
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	// 鉴权已由 API middleware 完成；这里只确认 Principal 存在，避免 handler 被错误地裸挂载。
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteError(w, r, authenticationRequiredError())
		return
	}
	if r.ContentLength > llmproviders.MaxMessagesRequestBodyBytes {
		writeRequestTooLarge(w, r)
		return
	}
	// 必须在转发前扫完顶层 JSON，否则后置的重复 model 可绕过白名单。
	r.Body = http.MaxBytesReader(w, r.Body, llmproviders.MaxMessagesRequestBodyBytes)
	modelID, body, err := readRequestModel(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeRequestTooLarge(w, r)
			return
		}
		httpapi.WriteError(w, r, invalidRequestError(err))
		return
	}
	upstream, err := llmproviders.Resolve(
		r.Context(),
		h.database,
		h.secrets,
		principal.OrganizationUUID,
		principal.WorkspaceUUID,
		modelID,
	)
	if err != nil {
		h.writeProviderError(w, r, err)
		return
	}
	target, err := llmproviders.Endpoint(upstream.BaseURL, "/v1/messages", r.URL.RawQuery)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "build messages upstream endpoint", "error", err)
		httpapi.WriteError(w, r, upstreamUnavailableError())
		return
	}
	upstreamRequest, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		h.logger.ErrorContext(r.Context(), "build messages upstream request", "error", err)
		httpapi.WriteError(w, r, upstreamUnavailableError())
		return
	}
	upstreamRequest.ContentLength = int64(len(body))
	// 先清除客户端鉴权和租户 header，再注入只存在于 OMA 服务端的真实上游 key。
	upstreamRequest.Header = sanitizedRequestHeaders(r.Header)
	llmproviders.ApplyAPIKey(upstreamRequest.Header, upstream.APIKey)
	upstreamResponse, err := h.client.Do(upstreamRequest)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "proxy messages upstream request", "error", err)
		httpapi.WriteError(w, r, upstreamUnavailableError())
		return
	}
	defer upstreamResponse.Body.Close()
	if err := writeProxyResponse(w, upstreamResponse); err != nil && r.Context().Err() == nil {
		h.logger.ErrorContext(r.Context(), "stream Messages upstream response", "error", err)
	}
}

func (h *Handler) writeProviderError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, llmproviders.ErrModelNotConfigured):
		httpapi.WriteError(w, r, modelNotConfiguredError())
	case errors.Is(err, llmproviders.ErrNotConfigured):
		httpapi.WriteError(w, r, providerNotConfiguredError())
	default:
		h.logger.ErrorContext(r.Context(), "resolve Messages LLM provider", "error", err)
		httpapi.WriteError(w, r, providerUnavailableError())
	}
}

func readRequestModel(body io.Reader) (string, []byte, error) {
	buffered, err := io.ReadAll(body)
	if err != nil {
		return "", nil, err
	}
	modelID, err := llmproviders.MessageRequestModel(buffered)
	if errors.Is(err, llmproviders.ErrRequestBodyNotObject) {
		return "", nil, errors.New("Request body must be a JSON object")
	}
	if err != nil {
		return "", nil, err
	}
	return modelID, buffered, nil
}

func writeRequestTooLarge(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, requestTooLargeError())
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
