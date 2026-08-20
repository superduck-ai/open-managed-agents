package sessions

import (
	"log/slog"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/sessionfanout"

	"github.com/go-chi/chi/v5"
)

// NewHandler 要求显式注入与 environment runner 和 code-session HTTP Handler 共用的 Service，
// 并把自身注册为公开事件 sink；这样 worker 输出会进入同一 session stream，而不会落到另一份 Service 状态。
func NewHandler(cfg config.Config, database *db.DB, codeSessionService *codesessions.Service, webhookEvents webhookEnqueuer, eventBus sessionfanout.EventBus, logger *slog.Logger) *Handler {
	if codeSessionService == nil {
		panic("sessions: code-session service is required")
	}
	logger = logging.LoggerOrDefault(logger)
	if eventBus == nil {
		eventBus = sessionfanout.NewLocal()
	}
	h := &Handler{
		cfg:          cfg,
		db:           database,
		codeSessions: codeSessionService,
		webhooks:     webhookEvents,
		logger:       logger,
		errorAdapter: httpapi.NewErrorAdapter(logger),
		streams:      newStreamHub(),
		eventBus:     eventBus,
		previews:     newWorkerPreviewConverter(),
	}
	eventBus.Register(h.receiveFanout, h.resetFanout)
	codeSessionService.SetPublicEventSink(h)
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Post("/", wrap(h.create))
	router.Get("/", wrap(h.list))
	router.Route("/{session_id}", func(r chi.Router) {
		r.Get("/", wrap(h.retrieveRoute))
		r.Post("/", wrap(h.updateRoute))
		r.Delete("/", wrap(h.deleteRoute))
		r.Post("/archive", wrap(h.archiveRoute))
		r.Route("/events", func(r chi.Router) {
			r.Get("/", wrap(h.listEventsRoute))
			r.Post("/", wrap(h.sendEventsRoute))
			r.Get("/stream", h.streamEventsRoute)
		})
		r.Route("/resources", func(r chi.Router) {
			r.Get("/", wrap(h.listResourcesRoute))
			r.Post("/", wrap(h.addResourceRoute))
			r.Get("/{resource_id}", wrap(h.retrieveResourceRoute))
			r.Post("/{resource_id}", wrap(h.updateResourceRoute))
			r.Delete("/{resource_id}", wrap(h.deleteResourceRoute))
		})
		r.Route("/threads", func(r chi.Router) {
			r.Get("/", wrap(h.listThreadsRoute))
			r.Get("/{thread_id}", wrap(h.retrieveThreadRoute))
			r.Post("/{thread_id}/archive", wrap(h.archiveThreadRoute))
			r.Get("/{thread_id}/events", wrap(h.listThreadEventsRoute))
			r.Get("/{thread_id}/stream", h.streamThreadEventsRoute)
		})
	})
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("beta") != "true" {
		h.errorAdapter.Write(w, r, sessionsBetaRequired())
		return
	}
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return sessionRouteNotFound()
}
