package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
)

var (
	privacyConsentsMu sync.RWMutex
	privacyConsents   = map[string]map[string]any{}
)

type PrivacyConsentsResponse struct {
	Consents []map[string]any `json:"consents"`
}

func RegisterPlatformPrivacyConsentRoutes(r chi.Router) {
	r.Get("/privacy-consents", handleListPrivacyConsents)
	r.Put("/privacy-consents", handlePutPrivacyConsents)
}

func handleListPrivacyConsents(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	privacyConsentsMu.RLock()
	keys := make([]string, 0, len(privacyConsents))
	for key := range privacyConsents {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	consents := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		consents = append(consents, cloneMap(privacyConsents[key]))
	}
	privacyConsentsMu.RUnlock()
	writeJSON(w, http.StatusOK, PrivacyConsentsResponse{Consents: consents})
}

func handlePutPrivacyConsents(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid privacy consent payload"})
		return
	}
	consentType, _ := body["consent_type"].(string)
	if strings.TrimSpace(consentType) != "" {
		privacyConsentsMu.Lock()
		privacyConsents[consentType] = cloneMap(body)
		privacyConsentsMu.Unlock()
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
