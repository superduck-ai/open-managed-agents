package models

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	database     *db.DB
	router       chi.Router
	errorAdapter *httpapi.ErrorAdapter
}

type listResponse struct {
	Data []modelResponse `json:"data"`
}

func NewHandler(database *db.DB) *Handler {
	h := &Handler{database: database, errorAdapter: httpapi.NewErrorAdapter(nil)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Get("/", wrap(h.list))
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *Handler) notFound(http.ResponseWriter, *http.Request) error {
	return modelRouteNotFound()
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return modelAuthenticationRequired()
	}
	modelIDs, err := llmproviders.ListModelIDs(
		r.Context(),
		h.database,
		principal.OrganizationUUID,
		principal.WorkspaceUUID,
	)
	if err != nil {
		return modelUnavailable(err)
	}
	httpapi.WriteJSON(w, http.StatusOK, listResponse{
		Data: modelResponses(modelIDs),
	})
	return nil
}
