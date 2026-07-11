package mcpcatalogs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	maxCatalogTools       = 512
	maxCatalogPages       = 20
	maxProbeResponseBytes = 1 << 20
	maxToolNameRunes      = 256
	maxToolTitleRunes     = 512
	maxDescriptionRunes   = 4096
)

type CatalogTool struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type ProbeResult struct {
	Tools           []CatalogTool
	ProtocolVersion string
	ServerInfo      json.RawMessage
}

type ProbeError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ProbeError) Error() string { return e.Message }

type Prober struct{}

// Probe 通过匿名 Streamable HTTP 会话发现工具，并对分页数量、字段长度和响应体读取量设置上限。
// 返回结果只包含可安全展示的工具元数据及 MCP 初始化信息，不携带请求凭据或工具输入 schema。
func (p Prober) Probe(ctx context.Context, endpoint string) (ProbeResult, error) {
	normalized, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return ProbeResult{}, probeError("invalid_endpoint", "The MCP endpoint is invalid.", false)
	}
	status := &statusRecorder{}
	// 当前产品决策不做目标地址级 SSRF 过滤，直接使用系统 DNS 和标准拨号；
	// MCP 的可达网络范围由部署环境的网络策略决定。
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       15 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: status.wrap(transport),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 允许同源路径重定向以兼容常见部署，但不接受跨源跳转；
			// 这能保证 catalog 记录的 endpoint 与实际提供工具的服务来源一致。
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if !sameOrigin(via[0].URL, req.URL) {
				return errors.New("cross-origin redirect blocked")
			}
			return nil
		},
	}
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "open-managed-agents-catalog", Version: "1"}, nil)
	session, err := mcpClient.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   normalized,
		HTTPClient: client,
		// 重试统一交给持久化 job 状态机处理，避免 SDK 内部重试绕过 attempts、退避与可观测状态。
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return ProbeResult{}, classifyProbeError(err, status.code())
	}
	defer func() { _ = session.Close() }()

	tools := make([]CatalogTool, 0)
	seenNames := map[string]struct{}{}
	cursor := ""
	for page := 0; page < maxCatalogPages; page++ {
		result, listErr := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if listErr != nil {
			return ProbeResult{}, classifyProbeError(listErr, status.code())
		}
		for _, tool := range result.Tools {
			if tool == nil {
				return ProbeResult{}, probeError("invalid_response", "The MCP server returned an invalid tool list.", false)
			}
			name := strings.TrimSpace(tool.Name)
			if name == "" || runeCount(name) > maxToolNameRunes {
				return ProbeResult{}, probeError("invalid_response", "The MCP server returned an invalid tool name.", false)
			}
			// 工具权限和前端展示都以 name 作为身份键；重复名称会产生不确定映射，
			// 因此拒绝整份快照，而不是静默采用 first-wins 或 last-wins。
			if _, duplicate := seenNames[name]; duplicate {
				return ProbeResult{}, probeError("invalid_response", "The MCP server returned duplicate tool names.", false)
			}
			if len(tools) >= maxCatalogTools {
				return ProbeResult{}, probeError("response_too_large", "The MCP server returned too many tools.", false)
			}
			seenNames[name] = struct{}{}
			title := truncateRunes(strings.TrimSpace(tool.Title), maxToolTitleRunes)
			if title == "" && tool.Annotations != nil {
				title = truncateRunes(strings.TrimSpace(tool.Annotations.Title), maxToolTitleRunes)
			}
			tools = append(tools, CatalogTool{
				Name:        name,
				Title:       title,
				Description: truncateRunes(strings.TrimSpace(tool.Description), maxDescriptionRunes),
			})
		}
		cursor = strings.TrimSpace(result.NextCursor)
		if cursor == "" {
			initialize := session.InitializeResult()
			var serverInfo json.RawMessage
			protocolVersion := ""
			if initialize != nil {
				protocolVersion = initialize.ProtocolVersion
				if initialize.ServerInfo != nil {
					serverInfo, _ = json.Marshal(initialize.ServerInfo)
				}
			}
			return ProbeResult{Tools: tools, ProtocolVersion: protocolVersion, ServerInfo: serverInfo}, nil
		}
	}
	return ProbeResult{}, probeError("response_too_large", "The MCP server returned too many tool pages.", false)
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

// statusRecorder 补充记录 MCP SDK 错误中可能丢失的 HTTP 状态，用于区分鉴权、限流和上游故障；
// transport wrapper 同时限制单个不受信响应体的读取量，互斥锁保护潜在的并发请求。
type statusRecorder struct {
	mu         sync.Mutex
	statusCode int
}

func (r *statusRecorder) wrap(next http.RoundTripper) http.RoundTripper {
	return roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		response, err := next.RoundTrip(request)
		if response != nil {
			r.mu.Lock()
			r.statusCode = response.StatusCode
			r.mu.Unlock()
			response.Body = &limitedReadCloser{Reader: io.LimitReader(response.Body, maxProbeResponseBytes+1), Closer: response.Body}
		}
		return response, err
	})
}

func (r *statusRecorder) code() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.statusCode
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

func classifyProbeError(err error, statusCode int) error {
	var existing *ProbeError
	if errors.As(err, &existing) {
		return existing
	}
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		return probeError("auth_required", "Authentication is required to discover this MCP server's tools.", false)
	}
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return probeError("upstream_unavailable", "The MCP server is temporarily unavailable.", true)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return probeError("timeout", "The MCP server did not respond in time.", true)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return probeError("unreachable", "The MCP server could not be reached.", true)
	}
	return probeError("invalid_response", "The MCP server returned an invalid MCP response.", false)
}

func probeError(code, message string, retryable bool) *ProbeError {
	return &ProbeError{Code: code, Message: message, Retryable: retryable}
}

func runeCount(value string) int { return len([]rune(value)) }

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

func probeErrorDetails(err error) (string, string, bool) {
	var probeErr *ProbeError
	if errors.As(err, &probeErr) {
		return probeErr.Code, probeErr.Message, probeErr.Retryable
	}
	return "internal_error", "MCP discovery failed.", true
}
