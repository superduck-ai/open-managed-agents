package httpapi

import (
	"net/http"
	"time"
)

func handleOrganizationExperienceType(w http.ResponseWriter, r *http.Request) {
	if _, ok := visibleOrgUUID(w, r); !ok {
		return
	}
	now := time.Now().UTC()
	in24h := now.Add(24 * time.Hour).Format(time.RFC3339)
	in48h := now.Add(48 * time.Hour).Format(time.RFC3339)
	writeJSON(w, http.StatusOK, map[string]any{
		"experiences": []any{},
		"rules": map[string]any{
			"global": map[string]any{
				"rate_limit": map[string]any{"remaining": 0, "reset_at": in24h},
				"cooldown":   nil,
			},
			"placements": map[string]any{
				"home-nudge": map[string]any{
					"rate_limit": map[string]any{"remaining": 1, "reset_at": in24h},
					"cooldown":   nil,
				},
				"spotlight": map[string]any{
					"rate_limit": map[string]any{"remaining": 0, "reset_at": in48h},
					"cooldown":   nil,
				},
				"chat-tooltip": map[string]any{
					"rate_limit": map[string]any{"remaining": 1, "reset_at": in48h},
					"cooldown":   nil,
				},
				"cowork":        map[string]any{"rate_limit": nil, "cooldown": nil},
				"global-banner": map[string]any{"rate_limit": nil, "cooldown": nil},
				"admin-capability-tooltip": map[string]any{
					"rate_limit": map[string]any{"remaining": 1, "reset_at": in24h},
					"cooldown":   nil,
				},
			},
			"tiers": map[string]any{},
		},
	})
}
