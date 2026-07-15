package codesessions

import (
	"net/http"
	"sync"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

// BridgeAuthenticator 在 HTTP 边界校验 bridge 请求，并返回已解析的调用方身份。
type BridgeAuthenticator func(r *http.Request, codeSessionID string) (auth.Principal, *httpapi.Error)

// Handler 是 code-session 的 HTTP transport 边界。
// 它持有协议相关的鉴权、代理连接和日志状态；业务状态与业务编排统一委托给 Service。
type Handler struct {
	cfg                 config.Config
	db                  *db.DB
	service             *Service
	bridgeAuthenticator BridgeAuthenticator
	upstreamProxy       upstreamProxyRuntime
	upstreamHTTPClient  *http.Client // 转发 /v1/messages，测试中可替换为受控上游。
	otlpLogMu           sync.Mutex
}

// NewHandler 创建长生命周期的 HTTP handler。Handler 直接复用 Service 的数据库依赖，
// 避免 HTTP 路由和跨资源业务服务意外连接到不同的数据源。
func NewHandler(cfg config.Config, service *Service) *Handler {
	if service == nil {
		panic("codesessions: service is required")
	}
	handler := &Handler{
		cfg:                cfg,
		db:                 service.db,
		service:            service,
		upstreamProxy:      newUpstreamProxyRuntime(),
		upstreamHTTPClient: &http.Client{Transport: newRuntimeModelProxyTransport()},
	}
	// MITM 已开启或配置了稳定私钥时，在构造阶段立即读取私钥并在内存中签发一年期根证书，
	// 使缺失或畸形私钥在启动期失败，而不是延迟到第一个 CONNECT 隧道。
	// 只有 MITM 关闭且未配置私钥时，才保留临时 CA 的惰性生成兼容行为。
	if cfg.CodeSessionUpstreamProxyMITMEnabled || cfg.CodeSessionUpstreamProxyCAKeyFile != "" {
		if _, err := handler.loadUpstreamProxyCA(); err != nil {
			panic("codesessions: load upstream proxy CA: " + err.Error())
		}
	}
	return handler
}

// newRuntimeModelProxyTransport 复用默认 Transport 配置，并将每个上游主机可保留的
// 空闲 keep-alive 连接数提高到 32，减少并发或突发模型请求之间重复建立 TCP/TLS 连接的开销。
// http.Client 不设置整体 Timeout，使 SSE 流的生命周期由请求 context 和上游连接关闭控制，
// 避免正常的长时间流式响应被固定超时截断。
func newRuntimeModelProxyTransport() http.RoundTripper {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
		cloned := transport.Clone()
		cloned.MaxIdleConnsPerHost = 32
		return cloned
	}
	return &http.Transport{MaxIdleConnsPerHost: 32}
}
