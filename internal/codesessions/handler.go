package codesessions

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

// Handler 是 code-session 的 HTTP transport 边界。
// 它持有协议相关的鉴权、代理连接和日志状态；业务状态与业务编排统一委托给 Service。
type Handler struct {
	cfg                    config.Config
	db                     *db.DB
	service                *Service
	logger                 *slog.Logger
	errorAdapter           *httpapi.ErrorAdapter
	sandboxTimeoutExtender SandboxTimeoutExtender
	upstreamProxy          upstreamProxyRuntime
	mcpProxyTransport      http.RoundTripper
	wrapMCPVaultTransport  mcpProxyTransportWrapper
	egressSubstitutor      *vaults.EgressSubstitutor
	// 策略加载函数在生产环境读取数据库，测试可替换为 fixture。
	loadUpstreamPolicyContext func(ctx context.Context, identity upstreamProxyIdentity) (upstreamProxyPolicyContext, error)
	loadMCPPolicyContext      func(ctx context.Context, identity upstreamProxyIdentity) (mcpProxyPolicyContext, error)
	otlpLogMu                 sync.Mutex
}

// SandboxTimeoutExtender renews the provider-side lifetime of a managed-agent
// sandbox after its current worker proves liveness.
type SandboxTimeoutExtender interface {
	SetTimeout(ctx context.Context, sandboxID string, timeout time.Duration) error
}

// NewHandler 创建长生命周期的 HTTP handler。Handler 直接复用 Service 的数据库依赖，
// 避免 HTTP 路由和跨资源业务服务意外连接到不同的数据源。
func NewHandler(cfg config.Config, service *Service, sandboxTimeoutExtender SandboxTimeoutExtender, logger *slog.Logger) *Handler {
	if service == nil {
		panic("codesessions: service is required")
	}
	logger = logging.LoggerOrDefault(logger)
	handler := &Handler{
		cfg:                    cfg,
		db:                     service.db,
		service:                service,
		logger:                 logger,
		errorAdapter:           httpapi.NewErrorAdapter(logger),
		sandboxTimeoutExtender: sandboxTimeoutExtender,
		upstreamProxy:          newUpstreamProxyRuntime(),
		mcpProxyTransport:      newMCPProxyTransport(cfg.CodeSession.UpstreamProxyDisableSSRFProtection),
	}
	handler.loadUpstreamPolicyContext = handler.loadUpstreamProxyPolicyContext
	handler.loadMCPPolicyContext = handler.loadMCPProxyPolicyContext
	// 只有 MITM 开启时才在构造阶段读取稳定私钥并签发一年期根证书，使配置错误在启动期失败。
	// MITM 关闭时私钥路径完全休眠，由 CA 下载接口按需生成进程级临时 CA。
	if cfg.CodeSession.UpstreamProxyMITMEnabled {
		if _, err := handler.loadUpstreamProxyCA(); err != nil {
			panic("codesessions: load upstream proxy CA: " + err.Error())
		}
	}
	return handler
}

// WithVaultSecrets wires vault credential injection (static_bearer / mcp_oauth)
// into the MCP HTTP proxy, including one 401 refresh retry for mcp_oauth, and
// Egress Secret Substitution for environment_variable credentials on MITM.
func (h *Handler) WithVaultSecrets(secretSvc *secrets.Service) *Handler {
	if h == nil || h.db == nil || secretSvc == nil {
		return h
	}
	injector := vaults.NewInjector(h.db, secretSvc, h.logger)
	h.wrapMCPVaultTransport = func(ctx context.Context, claims SessionCredentialClaims, target *url.URL, base http.RoundTripper) http.RoundTripper {
		return injector.WrapTransport(
			ctx,
			claims.SessionID,
			claims.OrganizationUUID,
			claims.WorkspaceUUID,
			target,
			base,
		)
	}
	h.egressSubstitutor = vaults.NewEgressSubstitutor(h.db, secretSvc, h.logger)
	return h
}
