package models

import (
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	router  chi.Router
	catalog modelcatalog.Reader
}

type listResponse struct {
	Data    []map[string]any `json:"data"`
	HasMore bool             `json:"has_more"`
	FirstID string           `json:"first_id"`
	LastID  string           `json:"last_id"`
}

func NewHandler(catalog modelcatalog.Reader) *Handler {
	h := &Handler{catalog: catalog}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Get("/", h.list)
	h.router = router
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func notFound(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Not found"))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	if h.catalog == nil {
		writeCatalogUnavailable(w, r)
		return
	}
	snapshot, err := h.catalog.Snapshot(r.Context())
	if err != nil {
		writeCatalogUnavailable(w, r)
		return
	}

	data := make([]map[string]any, 0, len(snapshot.Models))
	for _, model := range snapshot.Models {
		data = append(data, modelResponse(model))
	}
	firstID := ""
	lastID := ""
	if len(snapshot.Models) > 0 {
		firstID = snapshot.Models[0].ID
		lastID = snapshot.Models[len(snapshot.Models)-1].ID
	}
	httpapi.WriteJSON(w, http.StatusOK, listResponse{
		Data:    data,
		HasMore: false,
		FirstID: firstID,
		LastID:  lastID,
	})
}

func writeCatalogUnavailable(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, "api_error", "Model catalog is unavailable"))
}

func modelResponse(model modelcatalog.Model) map[string]any {
	response := map[string]any{
		"type":         "model",
		"id":           model.ID,
		"display_name": model.DisplayName,
	}
	if model.Description != "" {
		response["description"] = model.Description
	}
	if model.CreatedAt != "" {
		response["created_at"] = model.CreatedAt
	}
	if model.MaxInputTokens != nil {
		response["max_input_tokens"] = *model.MaxInputTokens
	}
	if model.MaxTokens != nil {
		response["max_tokens"] = *model.MaxTokens
	}
	if capabilities := model.Capabilities.RawJSON(); len(capabilities) > 0 {
		response["capabilities"] = capabilities
	}
	return response
}
