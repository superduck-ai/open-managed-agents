package codesessions

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

// RegisterV1Routes 在已经挂载到 /v1 的 chi router 中注册 code-session 资源。
func (h *Handler) RegisterV1Routes(router chi.Router) {
	h.registerRuntimeRoutes(router)
	h.registerCodeSessionRoutes(router)
	// 旧版 HTTP session_ingress 继续只校验 session token；CCR v2 的 worker 归属与 epoch
	// 约束只在 /worker/* 路由执行，避免改变兼容接口语义。
	h.registerSessionIngressRoutes(router)
}

// RegisterV2Routes 在已经挂载到 /v2 的 chi router 中注册 CCRv2 兼容资源。
func (h *Handler) RegisterV2Routes(router chi.Router) {
	h.registerSessionIngressRoutes(router)
	router.Get("/sessions/{code_session_id}", h.handleSessionContext)
}

func (h *Handler) registerRuntimeRoutes(router chi.Router) {
	router.Post("/messages", h.handleRuntimeMessagesProxy)
	router.Route("/code/upstreamproxy", func(router chi.Router) {
		router.Get("/ca-cert", h.handleUpstreamProxyCACertificate)
		router.Get("/ws", h.handleUpstreamProxyWebSocket)
	})
}

func (h *Handler) registerCodeSessionRoutes(router chi.Router) {
	const sessionPath = "/code/sessions/{code_session_id}"

	router.Get(sessionPath, h.handleCodeSession)
	router.Post(sessionPath, h.handleCodeSession)
	router.Put(sessionPath, h.handleCodeSession)
	router.Post(sessionPath+"/bridge", h.handleCodeSessionBridge)
	router.Get(sessionPath+"/worker", h.handleGetCodeSessionWorker)
	router.Put(sessionPath+"/worker", h.handlePutCodeSessionWorker)
	router.HandleFunc(sessionPath+"/worker/internal-events", h.handleCodeSessionWorkerInternalEvents)
	router.Get(sessionPath+"/worker/events/stream", h.handleCodeSessionWorkerEventsStream)
	router.Post(sessionPath+"/worker/register", h.handleCodeSessionWorkerRegister)
	router.Post(sessionPath+"/worker/events", h.handleCodeSessionWorkerEvents)
	router.Post(sessionPath+"/worker/events/delivery", h.handleCodeSessionWorkerDelivery)
	router.Post(sessionPath+"/worker/diagnostics", h.handleCodeSessionWorkerDiagnostics)
	router.Post(sessionPath+"/worker/heartbeat", h.handleCodeSessionWorkerHeartbeat)
	router.Post(sessionPath+"/worker/otlp/metrics", h.handleCodeSessionWorkerOTLP)
	router.Post(sessionPath+"/worker/otlp/logs", h.handleCodeSessionWorkerOTLP)
}

func (h *Handler) registerSessionIngressRoutes(router chi.Router) {
	const sessionPath = "/session_ingress/session/{code_session_id}"

	// 旧 WebSocket ingress 已永久退役；显式 tombstone 保证它不落入通用 /v1 鉴权 fallback。
	router.Get("/session_ingress/ws/{code_session_id}", handleRetiredSessionIngressWebSocket)
	router.Get(sessionPath, h.handleSessionIngressPersistence)
	router.Post(sessionPath, h.handleSessionIngressPersistence)
	router.Put(sessionPath, h.handleSessionIngressPersistence)
	router.Post(sessionPath+"/events", h.handleSessionIngressEvents)
	router.Post(sessionPath+"/diag_logs", h.handleSessionIngressDiagLogs)
}

func handleRetiredSessionIngressWebSocket(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}
