package models

import (
	"maps"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/modelmapping"

	"github.com/go-chi/chi/v5"
	"github.com/samber/lo"
)

type Handler struct {
	router        chi.Router
	modelMappings map[string]string
}

type listResponse struct {
	Data    []map[string]any `json:"data"`
	HasMore bool             `json:"has_more"`
	FirstID string           `json:"first_id"`
	LastID  string           `json:"last_id"`
}

func NewHandler(upstream config.AnthropicUpstreamConfig) *Handler {
	h := &Handler{modelMappings: upstream.ModelMappings}
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

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) {
	models := resolvePlatformModels(buildPlatformModels(), h.modelMappings)
	firstID := ""
	lastID := ""
	if len(models) > 0 {
		firstID, _ = models[0]["id"].(string)
		lastID, _ = models[len(models)-1]["id"].(string)
	}
	httpapi.WriteJSON(w, http.StatusOK, listResponse{
		Data:    models,
		HasMore: false,
		FirstID: firstID,
		LastID:  lastID,
	})
}

func resolvePlatformModels(models []map[string]any, mappings map[string]string) []map[string]any {
	resolved := lo.Map(models, func(model map[string]any, _ int) map[string]any {
		out := maps.Clone(model)
		modelID, _ := out["id"].(string)
		sourceID := strings.TrimSpace(modelID)
		effectiveID := modelmapping.Resolve(modelID, mappings)
		out["id"] = effectiveID
		if effectiveID != sourceID {
			out["display_name"] = effectiveID
		}
		return out
	})
	return lo.UniqBy(resolved, func(model map[string]any) string {
		modelID, _ := model["id"].(string)
		return modelID
	})
}

func buildPlatformModels() []map[string]any {
	return []map[string]any{
		platformModel("claude-fable-5", "Claude Fable 5", "2026-06-07T00:00:00Z", 1000000, 128000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   true,
			AdaptiveThinking: true,
			ThinkingEnabled:  false,
			Effort:           true,
			XHighEffort:      true,
			MaxEffort:        true,
		}),
		platformModel("claude-opus-4-8", "Claude Opus 4.8", "2026-05-28T00:00:00Z", 1000000, 128000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   true,
			AdaptiveThinking: true,
			ThinkingEnabled:  false,
			Effort:           true,
			XHighEffort:      true,
			MaxEffort:        true,
		}),
		platformModel("claude-opus-4-7", "Claude Opus 4.7", "2026-04-14T00:00:00Z", 1000000, 128000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   true,
			AdaptiveThinking: true,
			ThinkingEnabled:  false,
			Effort:           true,
			XHighEffort:      true,
			MaxEffort:        true,
		}),
		platformModel("claude-sonnet-4-6", "Claude Sonnet 4.6", "2026-02-17T00:00:00Z", 1000000, 128000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   true,
			AdaptiveThinking: true,
			ThinkingEnabled:  true,
			Effort:           true,
			XHighEffort:      false,
			MaxEffort:        true,
		}),
		platformModel("claude-opus-4-6", "Claude Opus 4.6", "2026-02-04T00:00:00Z", 1000000, 128000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   true,
			AdaptiveThinking: true,
			ThinkingEnabled:  true,
			Effort:           true,
			XHighEffort:      false,
			MaxEffort:        true,
		}),
		platformModel("claude-opus-4-5-20251101", "Claude Opus 4.5", "2025-11-24T00:00:00Z", 200000, 64000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   false,
			AdaptiveThinking: false,
			ThinkingEnabled:  true,
			Effort:           true,
			XHighEffort:      false,
			MaxEffort:        false,
		}),
		platformModel("claude-haiku-4-5-20251001", "Claude Haiku 4.5", "2025-10-15T00:00:00Z", 200000, 64000, platformModelCapabilities{
			CodeExecution:    false,
			CompactContext:   false,
			AdaptiveThinking: false,
			ThinkingEnabled:  true,
			Effort:           false,
			XHighEffort:      false,
			MaxEffort:        false,
		}),
		platformModel("claude-sonnet-4-5-20250929", "Claude Sonnet 4.5", "2025-09-29T00:00:00Z", 1000000, 64000, platformModelCapabilities{
			CodeExecution:    true,
			CompactContext:   false,
			AdaptiveThinking: false,
			ThinkingEnabled:  true,
			Effort:           false,
			XHighEffort:      false,
			MaxEffort:        false,
		}),
	}
}

type platformModelCapabilities struct {
	CodeExecution    bool
	CompactContext   bool
	AdaptiveThinking bool
	ThinkingEnabled  bool
	Effort           bool
	XHighEffort      bool
	MaxEffort        bool
}

func platformModel(id string, displayName string, createdAt string, maxInputTokens int, maxTokens int, capabilities platformModelCapabilities) map[string]any {
	return map[string]any{
		"type":             "model",
		"id":               id,
		"display_name":     displayName,
		"created_at":       createdAt,
		"max_input_tokens": maxInputTokens,
		"max_tokens":       maxTokens,
		"capabilities": map[string]any{
			"batch":              map[string]any{"supported": true},
			"citations":          map[string]any{"supported": true},
			"code_execution":     map[string]any{"supported": capabilities.CodeExecution},
			"context_management": platformContextManagementCapabilities(capabilities.CompactContext),
			"effort":             platformEffortCapabilities(capabilities),
			"image_input":        map[string]any{"supported": true},
			"pdf_input":          map[string]any{"supported": true},
			"structured_outputs": map[string]any{"supported": true},
			"thinking": map[string]any{
				"supported": true,
				"types": map[string]any{
					"enabled":  map[string]any{"supported": capabilities.ThinkingEnabled},
					"adaptive": map[string]any{"supported": capabilities.AdaptiveThinking},
				},
			},
		},
	}
}

func platformContextManagementCapabilities(compactSupported bool) map[string]any {
	return map[string]any{
		"supported":                true,
		"clear_tool_uses_20250919": map[string]any{"supported": true},
		"clear_thinking_20251015":  map[string]any{"supported": true},
		"compact_20260112":         map[string]any{"supported": compactSupported},
	}
}

func platformEffortCapabilities(capabilities platformModelCapabilities) map[string]any {
	return map[string]any{
		"supported": capabilities.Effort,
		"low":       map[string]any{"supported": capabilities.Effort},
		"medium":    map[string]any{"supported": capabilities.Effort},
		"high":      map[string]any{"supported": capabilities.Effort},
		"xhigh":     map[string]any{"supported": capabilities.XHighEffort},
		"max":       map[string]any{"supported": capabilities.MaxEffort},
	}
}
