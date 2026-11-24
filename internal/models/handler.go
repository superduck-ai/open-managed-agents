package models

import (
	"context"
	"net/http"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"

	"github.com/go-chi/chi/v5"
)

type catalogLoader func(ctx context.Context, organizationUUID string) (llmproviders.Catalog, error)

type Handler struct {
	loadCatalog  catalogLoader
	router       chi.Router
	errorAdapter *httpapi.ErrorAdapter
}

type listResponse struct {
	Data    []modelResponse `json:"data"`
	HasMore bool            `json:"has_more"`
	FirstID *string         `json:"first_id"`
	LastID  *string         `json:"last_id"`
}

func NewHandler(database *db.DB) *Handler {
	return newHandler(func(ctx context.Context, organizationUUID string) (llmproviders.Catalog, error) {
		return llmproviders.LoadCatalog(ctx, database, organizationUUID)
	})
}

func newHandler(loadCatalog catalogLoader) *Handler {
	h := &Handler{loadCatalog: loadCatalog, errorAdapter: httpapi.NewErrorAdapter(nil)}
	wrap := h.errorAdapter.Wrap
	router := chi.NewRouter()
	router.NotFound(wrap(h.notFound))
	router.MethodNotAllowed(wrap(h.notFound))
	router.Get("/", wrap(h.list))
	router.Get("/*", wrap(h.retrieve))
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
	catalog, err := h.principalCatalog(r)
	if err != nil {
		return err
	}
	modelIDs := catalog.ModelIDs()
	response := listResponse{Data: modelResponses(catalog)}
	if len(modelIDs) > 0 {
		response.FirstID = &modelIDs[0]
		response.LastID = &modelIDs[len(modelIDs)-1]
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request) error {
	modelID, ok := retrieveModelID(r)
	if !ok {
		return modelRouteNotFound()
	}
	catalog, err := h.principalCatalog(r)
	if err != nil {
		return err
	}
	selected, err := catalog.Lookup(modelID)
	if err != nil {
		return modelLookupFailed(modelID, err)
	}
	httpapi.WriteJSON(w, http.StatusOK, newModelResponse(selected))
	return nil
}

func retrieveModelID(r *http.Request) (string, bool) {
	modelID := chi.URLParam(r, "*")
	if r.URL.RawPath != "" {
		unescaped, err := url.PathUnescape(modelID)
		if err != nil {
			return "", false
		}
		modelID = unescaped
	}
	return modelID, true
}

func (h *Handler) principalCatalog(r *http.Request) (llmproviders.Catalog, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return llmproviders.Catalog{}, modelAuthenticationRequired()
	}
	catalog, err := h.loadCatalog(r.Context(), principal.OrganizationUUID)
	if err != nil {
		return llmproviders.Catalog{}, modelUnavailable(err)
	}
	return catalog, nil
}
