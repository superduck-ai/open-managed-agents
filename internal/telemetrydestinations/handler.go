package telemetrydestinations

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const maxRequestBody = 128 << 10

type Handler struct {
	service *Service
	router  chi.Router
}

func NewHandler(service *Service, logger *slog.Logger) *Handler {
	adapter := httpapi.NewErrorAdapter(logger)
	h := &Handler{service: service}
	router := chi.NewRouter()
	router.Use(requireBeta(adapter))
	router.Get("/", adapter.Wrap(h.list))
	router.Post("/", adapter.Wrap(h.create))
	router.Get("/{destination_id}", adapter.Wrap(h.get))
	router.Post("/{destination_id}", adapter.Wrap(h.update))
	router.Delete("/{destination_id}", adapter.Wrap(h.delete))
	router.Post("/{destination_id}/test", adapter.Wrap(h.test))
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func requireBeta(adapter *httpapi.ErrorAdapter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, value := range r.Header.Values("anthropic-beta") {
				for _, part := range strings.Split(value, ",") {
					if strings.TrimSpace(part) == BetaHeader {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			adapter.Wrap(func(http.ResponseWriter, *http.Request) error { return betaRequired() }).ServeHTTP(w, r)
		})
	}
}

func organization(r *http.Request, write bool) (string, error) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal.OrganizationUUID == "" || principal.WorkspaceAccess.OrganizationRole == "" {
		return "", organizationNotFound()
	}
	if write && !principal.WorkspaceAccess.ManageOrganization() {
		return "", permissionDenied()
	}
	return principal.OrganizationUUID, nil
}

func decodeRequest(w http.ResponseWriter, r *http.Request) (destinationRequest, error) {
	body, err := httpapi.DecodeObjectBodyAs[destinationRequest](w, r, maxRequestBody)
	if err != nil {
		return destinationRequest{}, invalidBody(err)
	}
	return *body, nil
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	org, err := organization(r, false)
	if err != nil {
		return err
	}
	query := r.URL.Query()
	limit := 20
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 1000 {
			return invalidRequest("limit must be between 1 and 1000")
		}
	}
	workspaceIDs := append(query["workspace_ids[]"], query["workspace_ids"]...)
	result, err := h.service.List(r.Context(), org, ListQuery{Limit: limit, Page: strings.TrimSpace(query.Get("page")), WorkspaceIDs: workspaceIDs})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	org, err := organization(r, true)
	if err != nil {
		return err
	}
	request, err := decodeRequest(w, r)
	if err != nil {
		return err
	}
	result, err := h.service.Create(r.Context(), org, request)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) error {
	org, err := organization(r, false)
	if err != nil {
		return err
	}
	result, err := h.service.Get(r.Context(), org, chi.URLParam(r, "destination_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	org, err := organization(r, true)
	if err != nil {
		return err
	}
	request, err := decodeRequest(w, r)
	if err != nil {
		return err
	}
	result, err := h.service.Update(r.Context(), org, chi.URLParam(r, "destination_id"), request)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	org, err := organization(r, true)
	if err != nil {
		return err
	}
	result, err := h.service.Delete(r.Context(), org, chi.URLParam(r, "destination_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) test(w http.ResponseWriter, r *http.Request) error {
	org, err := organization(r, true)
	if err != nil {
		return err
	}
	result, err := h.service.Test(r.Context(), org, chi.URLParam(r, "destination_id"))
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}
