package httpapi

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

var provisioningModes = map[string]struct{}{
	"LOGIN_ONLY":      {},
	"JIT_PERMISSIVE":  {},
	"JIT_ADVANCED":    {},
	"SCIM_PERMISSIVE": {},
	"SCIM_ADVANCED":   {},
}

type SSOSettings struct {
	OrgUUID                     string
	SSOEnabled                  bool
	ClaudeAISSOEnforced         bool
	ConsoleSSOEnforced          bool
	ProvisioningMode            string
	DirectorySyncEnabled        bool
	SAMLGroupLastSeen           *string
	OrganizationCreationBlocked bool
	SeatTierAssignmentEnabled   bool
	GroupRoleMappings           []map[string]any
	GroupSeatTierMappings       []map[string]any
	OrganizationCachedIDPGroups []string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

type SSOPatch struct {
	SSOEnabled                  *bool
	ClaudeAISSOEnforced         *bool
	ConsoleSSOEnforced          *bool
	ProvisioningMode            *string
	DirectorySyncEnabled        *bool
	SAMLGroupLastSeen           **string
	OrganizationCreationBlocked *bool
	SeatTierAssignmentEnabled   *bool
	GroupRoleMappings           []map[string]any
	GroupRoleMappingsSet        bool
	GroupSeatTierMappings       []map[string]any
	GroupSeatTierMappingsSet    bool
	OrganizationCachedIDPGroups []string
	OrganizationCachedSet       bool
}

var ssoSettingsState = struct {
	sync.RWMutex
	settings map[string]SSOSettings
}{
	settings: map[string]SSOSettings{},
}

func RegisterOrganizationSSORoutes(r chi.Router) {
	r.Get("/enterprise_auth/v2/sso_settings", handleGetSSOSettings)
	r.Put("/enterprise_auth/v2/sso_settings", handlePutSSOSettings)
}

func handleGetSSOSettings(w http.ResponseWriter, r *http.Request) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return
	}
	settings := getSSOSettings(orgUUID)
	writeJSON(w, http.StatusOK, formatSSOSettings(orgUUID, settings))
}

func handlePutSSOSettings(w http.ResponseWriter, r *http.Request) {
	orgUUID, ok := visibleOrgUUID(w, r)
	if !ok {
		return
	}
	body, _ := readJSONObject(r)
	patch, err := parseSSOPatch(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	saved := upsertSSOSettings(orgUUID, patch)
	writeJSON(w, http.StatusOK, formatSSOSettings(orgUUID, &saved))
}

func getSSOSettings(orgUUID string) *SSOSettings {
	ssoSettingsState.RLock()
	settings, ok := ssoSettingsState.settings[orgUUID]
	ssoSettingsState.RUnlock()
	if !ok {
		return nil
	}
	return &settings
}

func upsertSSOSettings(orgUUID string, patch SSOPatch) SSOSettings {
	ssoSettingsState.Lock()
	next := SSOSettings{
		OrgUUID:                     orgUUID,
		ProvisioningMode:            "LOGIN_ONLY",
		GroupRoleMappings:           []map[string]any{},
		GroupSeatTierMappings:       []map[string]any{},
		OrganizationCachedIDPGroups: []string{},
	}
	if existing, ok := ssoSettingsState.settings[orgUUID]; ok {
		next = existing
	}
	if patch.SSOEnabled != nil {
		next.SSOEnabled = *patch.SSOEnabled
	}
	if patch.ClaudeAISSOEnforced != nil {
		next.ClaudeAISSOEnforced = *patch.ClaudeAISSOEnforced
	}
	if patch.ConsoleSSOEnforced != nil {
		next.ConsoleSSOEnforced = *patch.ConsoleSSOEnforced
	}
	if patch.ProvisioningMode != nil {
		next.ProvisioningMode = *patch.ProvisioningMode
	}
	if patch.DirectorySyncEnabled != nil {
		next.DirectorySyncEnabled = *patch.DirectorySyncEnabled
	}
	if patch.OrganizationCreationBlocked != nil {
		next.OrganizationCreationBlocked = *patch.OrganizationCreationBlocked
	}
	if patch.SeatTierAssignmentEnabled != nil {
		next.SeatTierAssignmentEnabled = *patch.SeatTierAssignmentEnabled
	}
	if patch.GroupRoleMappingsSet {
		next.GroupRoleMappings = patch.GroupRoleMappings
	}
	if patch.GroupSeatTierMappingsSet {
		next.GroupSeatTierMappings = patch.GroupSeatTierMappings
	}
	if patch.OrganizationCachedSet {
		next.OrganizationCachedIDPGroups = patch.OrganizationCachedIDPGroups
	}
	ssoSettingsState.settings[orgUUID] = next
	ssoSettingsState.Unlock()
	return next
}

func formatSSOSettings(orgUUID string, settings *SSOSettings) map[string]any {
	sso := SSOSettings{
		OrgUUID:                     orgUUID,
		ProvisioningMode:            "LOGIN_ONLY",
		GroupRoleMappings:           []map[string]any{},
		GroupSeatTierMappings:       []map[string]any{},
		OrganizationCachedIDPGroups: []string{},
	}
	if settings != nil {
		sso = *settings
	}
	return map[string]any{
		"organization_uuid":              orgUUID,
		"sso_enabled":                    sso.SSOEnabled,
		"claudeai_sso_enforced":          sso.ClaudeAISSOEnforced,
		"console_sso_enforced":           sso.ConsoleSSOEnforced,
		"provisioning_mode":              sso.ProvisioningMode,
		"directory_sync_enabled":         sso.DirectorySyncEnabled,
		"saml_group_last_seen":           sso.SAMLGroupLastSeen,
		"organization_creation_blocked":  sso.OrganizationCreationBlocked,
		"seat_tier_assignment_enabled":   sso.SeatTierAssignmentEnabled,
		"group_role_mappings":            sso.GroupRoleMappings,
		"group_seat_tier_mappings":       sso.GroupSeatTierMappings,
		"organization_cached_idp_groups": sso.OrganizationCachedIDPGroups,
	}
}

func parseSSOPatch(body map[string]any) (SSOPatch, error) {
	var patch SSOPatch
	if value, ok := body["sso_enabled"].(bool); ok {
		patch.SSOEnabled = &value
	}
	if value, ok := body["claudeai_sso_enforced"].(bool); ok {
		patch.ClaudeAISSOEnforced = &value
	}
	if value, ok := body["console_sso_enforced"].(bool); ok {
		patch.ConsoleSSOEnforced = &value
	}
	if value, ok := body["provisioning_mode"].(string); ok {
		if _, valid := provisioningModes[value]; !valid {
			return patch, errString("invalid provisioning_mode")
		}
		patch.ProvisioningMode = &value
	}
	if value, ok := body["directory_sync_enabled"].(bool); ok {
		patch.DirectorySyncEnabled = &value
	}
	if value, ok := body["organization_creation_blocked"].(bool); ok {
		patch.OrganizationCreationBlocked = &value
	}
	if value, ok := body["seat_tier_assignment_enabled"].(bool); ok {
		patch.SeatTierAssignmentEnabled = &value
	}
	if values, ok := body["group_role_mappings"].([]any); ok {
		patch.GroupRoleMappings = objectList(values)
		patch.GroupRoleMappingsSet = true
	}
	if values, ok := body["group_seat_tier_mappings"].([]any); ok {
		patch.GroupSeatTierMappings = objectList(values)
		patch.GroupSeatTierMappingsSet = true
	}
	if _, exists := body["organization_cached_idp_groups"]; exists {
		patch.OrganizationCachedIDPGroups = normalizeStringList(body["organization_cached_idp_groups"])
		patch.OrganizationCachedSet = true
	}
	return patch, nil
}

func normalizeStringList(value any) []string {
	values, ok := jsonStringSlice(value)
	if !ok {
		return []string{}
	}
	return compactStrings(values)
}

func objectList(values []any) []map[string]any {
	out := []map[string]any{}
	for _, value := range values {
		if obj, ok := value.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

func jsonStringSlice(value any) ([]string, bool) {
	items, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}

func compactStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

type errString string

func (e errString) Error() string {
	return string(e)
}
