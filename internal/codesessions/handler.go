package codesessions

import (
	"sync"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// Handler 是 code-session 的 HTTP transport 边界。
// 它持有协议相关的鉴权、代理连接和日志状态；业务状态与业务编排统一委托给 Service。
type Handler struct {
	cfg           config.Config
	db            *db.DB
	service       *Service
	upstreamProxy upstreamProxyRuntime
	otlpLogMu     sync.Mutex
}

// NewHandler 创建长生命周期的 HTTP handler。Handler 直接复用 Service 的数据库依赖，
// 避免 HTTP 路由和跨资源业务服务意外连接到不同的数据源。
func NewHandler(cfg config.Config, service *Service) *Handler {
	if service == nil {
		panic("codesessions: service is required")
	}
	handler := &Handler{
		cfg:           cfg,
		db:            service.db,
		service:       service,
		upstreamProxy: newUpstreamProxyRuntime(),
	}
	// 只有 MITM 开启时才在构造阶段读取稳定私钥并签发一年期根证书，使配置错误在启动期失败。
	// MITM 关闭时私钥路径完全休眠，由 CA 下载接口按需生成进程级临时 CA。
	if cfg.CodeSessionUpstreamProxyMITMEnabled {
		if _, err := handler.loadUpstreamProxyCA(); err != nil {
			panic("codesessions: load upstream proxy CA: " + err.Error())
		}
	}
	return handler
}
