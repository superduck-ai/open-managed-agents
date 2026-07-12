package mcpcatalogs

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/db"

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

// CatalogTool 复用持久化 schema，确保探测结果、数据库和 Console API 使用同一份字段定义。
type CatalogTool = db.MCPToolCatalogItem

type ProbeResult struct {
	Tools []CatalogTool
}

type ProbeError struct {
	Code    string
	Message string
}

func (e *ProbeError) Error() string { return e.Message }

type Prober struct{}

// Probe 通过匿名 Streamable HTTP 会话发现工具，并对分页数量、字段长度和响应体读取量设置上限。
// 返回结果只包含可安全展示的工具元数据，不携带请求凭据、初始化详情或工具输入 schema。
func (p Prober) Probe(ctx context.Context, endpoint string) (ProbeResult, error) {
	normalized, err := NormalizeEndpoint(endpoint)
	if err != nil {
		return ProbeResult{}, probeError("invalid_endpoint", "The MCP endpoint is invalid.")
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
		// 手动刷新只执行一次明确的用户请求；关闭 SDK 内部重试，让超时和失败立即返回页面。
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
				return ProbeResult{}, probeError("invalid_response", "The MCP server returned an invalid tool list.")
			}
			name := strings.TrimSpace(tool.Name)
			if name == "" || runeCount(name) > maxToolNameRunes {
				return ProbeResult{}, probeError("invalid_response", "The MCP server returned an invalid tool name.")
			}
			// 工具权限和前端展示都以 name 作为身份键；重复名称会产生不确定映射，
			// 因此拒绝整份快照，而不是静默采用 first-wins 或 last-wins。
			if _, duplicate := seenNames[name]; duplicate {
				return ProbeResult{}, probeError("invalid_response", "The MCP server returned duplicate tool names.")
			}
			if len(tools) >= maxCatalogTools {
				return ProbeResult{}, probeError("response_too_large", "The MCP server returned too many tools.")
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
			return ProbeResult{Tools: tools}, nil
		}
	}
	return ProbeResult{}, probeError("response_too_large", "The MCP server returned too many tool pages.")
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
		return probeError("auth_required", "Authentication is required to discover this MCP server's tools.")
	}
	if statusCode == http.StatusTooManyRequests || statusCode >= 500 {
		return probeError("upstream_unavailable", "The MCP server is temporarily unavailable.")
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return probeError("timeout", "The MCP server did not respond in time.")
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return probeError("unreachable", "The MCP server could not be reached.")
	}
	return probeError("invalid_response", "The MCP server returned an invalid MCP response.")
}

func probeError(code, message string) *ProbeError {
	return &ProbeError{Code: code, Message: message}
}

func runeCount(value string) int { return len([]rune(value)) }

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}
