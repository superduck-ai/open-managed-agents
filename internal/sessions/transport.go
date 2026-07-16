package sessions

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5"
)

// NewHandler 要求显式注入与 environment runner 和 code-session HTTP Handler 共用的 Service，
// 并把自身注册为公开事件 sink；这样 worker 输出会进入同一 session stream，而不会落到另一份 Service 状态。
func NewHandler(cfg config.Config, database *db.DB, codeSessionService *codesessions.Service) *Handler {
	if codeSessionService == nil {
		panic("sessions: code-session service is required")
	}
	h := &Handler{cfg: cfg, db: database, codeSessions: codeSessionService, streams: newStreamHub()}
	codeSessionService.SetPublicEventSink(h)
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Post("/", h.create)
	router.Get("/", h.list)
	router.Route("/{session_id}", func(r chi.Router) {
		r.Get("/", h.retrieveRoute)
		r.Post("/", h.updateRoute)
		r.Delete("/", h.deleteRoute)
		r.Post("/archive", h.archiveRoute)
		r.Route("/events", func(r chi.Router) {
			r.Get("/", h.listEventsRoute)
			r.Post("/", h.sendEventsRoute)
			r.Get("/stream", h.streamEventsRoute)
		})
		r.Route("/resources", func(r chi.Router) {
			r.Get("/", h.listResourcesRoute)
			r.Post("/", h.addResourceRoute)
			r.Get("/{resource_id}", h.retrieveResourceRoute)
			r.Post("/{resource_id}", h.updateResourceRoute)
			r.Delete("/{resource_id}", h.deleteResourceRoute)
		})
		r.Route("/threads", func(r chi.Router) {
			r.Get("/", h.listThreadsRoute)
			r.Get("/{thread_id}", h.retrieveThreadRoute)
			r.Post("/{thread_id}/archive", h.archiveThreadRoute)
			r.Get("/{thread_id}/events", h.listThreadEventsRoute)
			r.Get("/{thread_id}/stream", h.streamThreadEventsRoute)
		})
	})
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", "Sessions API requires beta=true"))
		return
	}
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}
