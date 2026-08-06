package models

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/modelcatalog"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	router  chi.Router
	catalog modelcatalog.Reader
	logger  *slog.Logger
}

type listResponse struct {
	Data    []modelResponse `json:"data"`
	HasMore bool            `json:"has_more"`
	FirstID string          `json:"first_id"`
	LastID  string          `json:"last_id"`
}

type modelResponse struct {
	ID             string                    `json:"id"`
	Capabilities   modelcatalog.Capabilities `json:"capabilities,omitempty"`
	CreatedAt      string                    `json:"created_at,omitempty"`
	DisplayName    string                    `json:"display_name"`
	MaxInputTokens *int                      `json:"max_input_tokens,omitempty"`
	MaxTokens      *int                      `json:"max_tokens,omitempty"`
	Type           string                    `json:"type"`
}

type listParams struct {
	AfterID  string
	BeforeID string
	Limit    int
}

func NewHandler(catalog modelcatalog.Reader, logger *slog.Logger) *Handler {
	h := &Handler{catalog: catalog, logger: logging.LoggerOrDefault(logger)}
	router := chi.NewRouter()
	router.NotFound(notFound)
	router.MethodNotAllowed(notFound)
	router.Get("/", h.list)
	router.Get("/*", h.retrieve)
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
	params, err := parseListParams(r)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}
	snapshot, ok := h.loadSnapshot(w, r)
	if !ok {
		return
	}
	models, hasMore, err := paginateModels(snapshot.Models, params)
	if err != nil {
		httpapi.WriteError(w, r, httpapi.NewError(http.StatusBadRequest, "invalid_request_error", err.Error()))
		return
	}

	data := make([]modelResponse, 0, len(models))
	for _, model := range models {
		data = append(data, modelResponseFromCatalog(model))
	}
	firstID := ""
	lastID := ""
	if len(models) > 0 {
		firstID = models[0].ID
		lastID = models[len(models)-1].ID
	}
	httpapi.WriteJSON(w, http.StatusOK, listResponse{
		Data:    data,
		HasMore: hasMore,
		FirstID: firstID,
		LastID:  lastID,
	})
}

func (h *Handler) retrieve(w http.ResponseWriter, r *http.Request) {
	snapshot, ok := h.loadSnapshot(w, r)
	if !ok {
		return
	}
	modelID := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	for _, model := range snapshot.Models {
		if model.ID == modelID {
			httpapi.WriteJSON(w, http.StatusOK, modelResponseFromCatalog(model))
			return
		}
	}
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusNotFound, "not_found_error", "Model not found"))
}

func (h *Handler) loadSnapshot(w http.ResponseWriter, r *http.Request) (modelcatalog.Snapshot, bool) {
	if h.catalog == nil {
		h.logger.WarnContext(r.Context(), "model catalog is not configured")
		writeCatalogUnavailable(w, r)
		return modelcatalog.Snapshot{}, false
	}
	snapshot, err := h.catalog.Snapshot(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "load model catalog snapshot", "error", err)
		writeCatalogUnavailable(w, r)
		return modelcatalog.Snapshot{}, false
	}
	return snapshot, true
}

func writeCatalogUnavailable(w http.ResponseWriter, r *http.Request) {
	httpapi.WriteError(w, r, httpapi.NewError(http.StatusServiceUnavailable, "api_error", "Model catalog is unavailable"))
}

func modelResponseFromCatalog(model modelcatalog.Model) modelResponse {
	return modelResponse{
		ID:             model.ID,
		Capabilities:   model.Capabilities,
		CreatedAt:      model.CreatedAt,
		DisplayName:    model.DisplayName,
		MaxInputTokens: model.MaxInputTokens,
		MaxTokens:      model.MaxTokens,
		Type:           "model",
	}
}

func parseListParams(r *http.Request) (listParams, error) {
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000 {
			return listParams{}, errors.New("limit must be between 1 and 1000")
		}
		limit = parsed
	}
	params := listParams{
		AfterID:  r.URL.Query().Get("after_id"),
		BeforeID: r.URL.Query().Get("before_id"),
		Limit:    limit,
	}
	if params.AfterID != "" && params.BeforeID != "" {
		return listParams{}, errors.New("after_id and before_id cannot be used together")
	}
	return params, nil
}

func paginateModels(models []modelcatalog.Model, params listParams) ([]modelcatalog.Model, bool, error) {
	start := 0
	end := len(models)
	if params.AfterID != "" {
		index := modelIndex(models, params.AfterID)
		if index < 0 {
			return nil, false, errors.New("after_id does not match an available model")
		}
		start = index + 1
	} else if params.BeforeID != "" {
		index := modelIndex(models, params.BeforeID)
		if index < 0 {
			return nil, false, errors.New("before_id does not match an available model")
		}
		end = index
		start = max(0, end-params.Limit)
		return models[start:end], start > 0, nil
	}
	end = min(end, start+params.Limit)
	return models[start:end], end < len(models), nil
}

func modelIndex(models []modelcatalog.Model, id string) int {
	for index, model := range models {
		if model.ID == id {
			return index
		}
	}
	return -1
}
