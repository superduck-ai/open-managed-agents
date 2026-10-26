package platformapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type llmPresetModelsRequest struct {
	Revision string                  `json:"revision"`
	Models   []config.LLMPresetModel `json:"models"`
}

func registerConsoleLLMProviderPresets(r chi.Router, database *db.DB, path string) {
	r.Get("/llm_provider_presets", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := llmProviderOrganization(w, r, database); !ok {
			return
		}
		catalog, err := config.ReadLLMProviderPresetCatalog(path)
		if err != nil {
			internalError(w, "failed to read supplier defaults")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
	r.Put("/llm_provider_presets/{presetId}/models", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := llmProviderOrganization(w, r, database); !ok {
			return
		}
		body, err := readRequiredJSON[llmPresetModelsRequest](r, true)
		if err != nil || body.Models == nil {
			writeLLMProviderError(w, http.StatusBadRequest, llmProviderCodeConfigurationInvalid, "revision and models array are required")
			return
		}
		catalog, err := config.SaveLLMProviderPresetModels(path, chi.URLParam(r, "presetId"), body.Revision, body.Models)
		if err != nil {
			switch {
			case errors.Is(err, config.ErrPresetConflict):
				writeLLMProviderError(w, http.StatusConflict, "llm_provider_presets_conflict", err.Error())
			case errors.Is(err, config.ErrPresetReadOnly):
				writeLLMProviderError(w, http.StatusForbidden, "llm_provider_presets_read_only", err.Error())
			case errors.Is(err, config.ErrPresetNotFound):
				writeLLMProviderError(w, http.StatusNotFound, llmProviderCodeConfigurationInvalid, err.Error())
			default:
				writeLLMProviderError(w, http.StatusBadRequest, "llm_provider_presets_write_failed", "failed to save supplier defaults; check model values and file permissions")
			}
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
}
