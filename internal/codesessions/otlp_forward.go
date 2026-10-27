package codesessions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

// maxOpenObserveResponseBytes 限制回读的上游响应体大小，防止异常上游拖垮内存。
const maxOpenObserveResponseBytes = 1 << 20

// otlpSink 是 OTLP 写入路径的后端扩展点：输入是 ingress 已注入可信租户属性的
// canonical payload，实现负责投递到具体后端并把上游失败翻译成 otlpSinkError。
// 目前唯一实现是 openObserveOTLPForwarder；接入新后端（如 otel-collector）时
// 增加实现并在 newOTLPSink 按 observability.backend 装配即可，ingress 不感知后端。
type otlpSink interface {
	forward(ctx context.Context, signal string, protocol otlpProtocol, body []byte) (otlpSinkResponse, error)
}

// openObserveOTLPForwarder 把 OTLP 请求转发到 OpenObserve。其中 OpenObserve
// 特有的只有三处：`/api/{org}/v1/{signal}` 路径、logs/traces 的 stream-name
// header、Basic Auth；其余（超时、禁重定向、响应大小上限、状态码归一化、
// Retry-After 透传）对任何 OTLP/HTTP 后端都通用。
type openObserveOTLPForwarder struct {
	enabled      bool
	baseURL      string
	organization string
	logsStream   string
	tracesStream string
	username     string
	password     string
	client       *http.Client
}

// otlpSinkResponse 保留上游成功响应体（如 partialSuccess），由 ingress 原样回给 worker。
type otlpSinkResponse struct {
	body []byte
}

// otlpSinkError 的 statusCode 已经过 normalizeOTLPSinkStatus 归一化，可直接
// 作为对 worker 的响应状态；retryAfter 透传上游的退避提示。
type otlpSinkError struct {
	statusCode int
	retryAfter string
	cause      error
}

func (e *otlpSinkError) Error() string {
	if e == nil || e.cause == nil {
		return "OTLP sink error"
	}
	return e.cause.Error()
}

func (e *otlpSinkError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// newOTLPSink 按 observability.backend 选择转发实现。配置校验保证 enabled 时
// backend 只会是受支持的值；新增后端时在此按 selector 扩展装配。
func newOTLPSink(cfg config.ObservabilityConfig) otlpSink {
	return newOpenObserveOTLPForwarder(cfg)
}

func newOpenObserveOTLPForwarder(cfg config.ObservabilityConfig) *openObserveOTLPForwarder {
	return &openObserveOTLPForwarder{
		enabled:      cfg.Enabled,
		baseURL:      strings.TrimRight(strings.TrimSpace(cfg.OpenObserve.BaseURL), "/"),
		organization: strings.TrimSpace(cfg.OpenObserve.Organization),
		logsStream:   strings.TrimSpace(cfg.OpenObserve.LogsStream),
		tracesStream: strings.TrimSpace(cfg.OpenObserve.TracesStream),
		username:     strings.TrimSpace(cfg.OpenObserve.Ingestion.Username),
		password:     cfg.OpenObserve.Ingestion.Password,
		client: &http.Client{
			Timeout: cfg.OTLP.ForwardTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (f *openObserveOTLPForwarder) forward(ctx context.Context, signal string, protocol otlpProtocol, body []byte) (otlpSinkResponse, error) {
	if f == nil || !f.enabled {
		return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusServiceUnavailable, cause: errors.New("agent observability is not enabled")}
	}
	endpoint, err := f.endpoint(signal)
	if err != nil {
		return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusServiceUnavailable, cause: err}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusServiceUnavailable, cause: fmt.Errorf("create OpenObserve OTLP request: %w", err)}
	}
	contentType := otlpContentType(protocol)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", contentType)
	if stream := f.streamForSignal(signal); stream != "" {
		request.Header.Set("stream-name", stream)
	}
	request.SetBasicAuth(f.username, f.password)

	response, err := f.client.Do(request)
	if err != nil {
		return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusServiceUnavailable, cause: fmt.Errorf("forward OTLP to OpenObserve: %w", err)}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxOpenObserveResponseBytes+1))
	if err != nil {
		return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusBadGateway, cause: fmt.Errorf("read OpenObserve OTLP response: %w", err)}
	}
	if len(responseBody) > maxOpenObserveResponseBytes {
		return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusBadGateway, cause: errors.New("OpenObserve OTLP response is too large")}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		cause := fmt.Errorf("OpenObserve rejected OTLP with status %d", response.StatusCode)
		return otlpSinkResponse{}, &otlpSinkError{
			statusCode: normalizeOTLPSinkStatus(response.StatusCode),
			retryAfter: strings.TrimSpace(response.Header.Get("Retry-After")),
			cause:      cause,
		}
	}
	if len(responseBody) > 0 {
		responseProtocol, parseErr := parseOTLPProtocol(response.Header.Get("Content-Type"))
		if parseErr != nil || responseProtocol != protocol {
			return otlpSinkResponse{}, &otlpSinkError{statusCode: http.StatusBadGateway, cause: errors.New("OpenObserve returned a mismatched OTLP response encoding")}
		}
	}
	return otlpSinkResponse{body: responseBody}, nil
}

func (f *openObserveOTLPForwarder) endpoint(signal string) (string, error) {
	parsed, err := url.Parse(f.baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("invalid OpenObserve base URL")
	}
	if signal != "metrics" && signal != "logs" && signal != "traces" {
		return "", fmt.Errorf("unsupported OTLP signal %q", signal)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/" + url.PathEscape(f.organization) + "/v1/" + signal
	return parsed.String(), nil
}

func (f *openObserveOTLPForwarder) streamForSignal(signal string) string {
	switch signal {
	case "logs":
		return f.logsStream
	case "traces":
		return f.tracesStream
	default:
		return ""
	}
}

// normalizeOTLPSinkStatus 把上游状态码翻译成对 worker 安全的状态码。
// 上游 401/403 说明是 OMA 侧凭据配置问题，映射为 503，避免让 worker 误以为
// 自己的 session token 失效；白名单内的状态原样透传；其余 4xx 归一为 400，
// 未知错误一律 503。
func normalizeOTLPSinkStatus(statusCode int) int {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusServiceUnavailable
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return statusCode
	default:
		if statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError {
			return http.StatusBadRequest
		}
		return http.StatusServiceUnavailable
	}
}
