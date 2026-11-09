package platformapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func registerConsoleLLMProviderPresets(r chi.Router, path string) {
	r.Get("/llm_provider_presets", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := visibleOrgUUID(w, r); !ok {
			return
		}
		catalog, err := config.ReadLLMProviderPresetCatalog(path)
		if err != nil {
			internalError(w, "failed to read supplier defaults")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	})
}
