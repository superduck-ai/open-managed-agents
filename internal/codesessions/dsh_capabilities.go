package codesessions

import (
	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/dsh"
)

func (h *Handler) registerDSHCapabilities(router chi.Router) {
	const base = "/sessions/{code_session_id}"
	guarded := router.With(dsh.RequireVersion)
	guarded.Post(base+"/sandbox/mcp", h.handleSandboxMCP)
	guarded.Post(base+"/sandbox/tool-outputs", h.handleSandboxToolOutput)
	guarded.HandleFunc(base+"/mcp", h.handleMCPProxy)
	guarded.HandleFunc(base+"/mcp/*", h.handleNamedMCPProxy)
	guarded.Post(base+"/web/search", h.handleWebSearch)
	guarded.Post(base+"/web/fetch", h.handleWebFetch)
	guarded.Get(base+"/threads/{thread_id}", h.handleGetCodeSessionSubagentThread)
	guarded.Post(base+"/otlp/v1/logs", h.handleCodeSessionWorkerOTLP)
	guarded.Post(base+"/otlp/v1/traces", h.handleCodeSessionWorkerTraces)
	if h.imageAttachments != nil {
		guarded.Route(base+"/attachments", h.imageAttachments.RegisterRoutes)
	}
}
