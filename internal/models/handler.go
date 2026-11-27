package models

import (
	"cmp"
	"context"
	"net/http"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"

	"github.com/go-chi/chi/v5"
)

type catalogLoader func(ctx context.Context, organizationUUID string) (llmproviders.Catalog, error)

type Handler struct {
	loadCatalog     catalogLoader
	runtimeResolver *agentruntime.Resolver
	router          chi.Router
	errorAdapter    *httpapi.ErrorAdapter
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

func (h *Handler) WithRuntimeResolver(resolver *agentruntime.Resolver) *Handler {
	h.runtimeResolver = resolver
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
	if isExternalRuntime(principal) {
		return h.listExternal(w, r, principal)
	}
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
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		return modelAuthenticationRequired()
	}
	if isExternalRuntime(principal) {
		return h.retrieveExternal(w, r, principal, modelID)
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

func isExternalRuntime(principal auth.Principal) bool {
	return principal.RuntimeProvider != "" && principal.RuntimeProvider != agentruntime.ProviderOMANative.String()
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

func (h *Handler) loadExternalModels(r *http.Request, principal auth.Principal) ([]modelResponse, error) {
	if h.runtimeResolver == nil {
		return nil, modelRuntimeUnavailable(nil)
	}
	rt, profile, err := h.runtimeResolver.ResolveWorkspace(r.Context(), principal.OrganizationUUID, principal.WorkspaceUUID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rt.Close() }()
	modelRT, ok := rt.(agentruntime.ModelRuntime)
	if !ok {
		return nil, modelRuntimeUnavailable(nil)
	}
	scope := agentruntime.Scope{
		OrganizationUUID:   principal.OrganizationUUID,
		WorkspaceUUID:      principal.WorkspaceUUID,
		RuntimeProfileUUID: profile.UUID,
		ProviderKind:       agentruntime.ProviderKind(profile.ProviderKind),
		UserUUID:           principal.UserUUID,
	}
	items, err := modelRT.ListModels(r.Context(), scope)
	if err != nil {
		return nil, err
	}
	data := make([]modelResponse, 0, len(items))
	for _, item := range items {
		entry := modelResponse{
			Type:        "model",
			ID:          item.ID,
			DisplayName: cmp.Or(item.DisplayName, item.ID),
			CreatedAt:   unknownModelCreatedAt,
		}
		if item.DefaultContextWindow > 0 {
			maxInput := int(item.DefaultContextWindow)
			entry.MaxInputTokens = &maxInput
		}
		data = append(data, entry)
	}
	return data, nil
}

func (h *Handler) listExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal) error {
	data, err := h.loadExternalModels(r, principal)
	if err != nil {
		return err
	}
	response := listResponse{Data: data}
	if len(data) > 0 {
		first := data[0].ID
		last := data[len(data)-1].ID
		response.FirstID = &first
		response.LastID = &last
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
	return nil
}

func (h *Handler) retrieveExternal(w http.ResponseWriter, r *http.Request, principal auth.Principal, modelID string) error {
	data, err := h.loadExternalModels(r, principal)
	if err != nil {
		return err
	}
	for _, item := range data {
		if item.ID == modelID {
			httpapi.WriteJSON(w, http.StatusOK, item)
			return nil
		}
	}
	return modelLookupFailed(modelID, nil)
}
