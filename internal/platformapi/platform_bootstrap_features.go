package platformapi

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
)

func buildBootstrapCompatibilityResponse(account *Account, orgScoped bool, growthbookHashingAlgorithm string, models platformModelCatalog) BootstrapCompatibilityResponse {
	statsigUser := map[string]any{}
	growthbookUser := map[string]any{"anonymousId": "None", "stableId": "None"}
	if account != nil {
		statsigUser = map[string]any{"userID": account.UUID, "email": account.EmailAddress}
		growthbookUser = map[string]any{"anonymousId": account.UUID, "stableId": account.UUID, "id": account.UUID}
	}
	if growthbookHashingAlgorithm == "" {
		growthbookHashingAlgorithm = "sha256"
	}

	statsig := BootstrapStatsig{User: statsigUser, Values: map[string]any{}, ValuesHash: ""}
	growthbook := BootstrapGrowthbook{
		Features:         buildGrowthbookFeatures(models),
		HashingAlgorithm: growthbookHashingAlgorithm,
		User:             growthbookUser,
	}
	locale := any(nil)
	if account != nil {
		locale = "en-US"
	}
	response := BootstrapCompatibilityResponse{
		Account:             account,
		Statsig:             &statsig,
		Growthbook:          &growthbook,
		OrgStatsig:          statsig,
		OrgGrowthbook:       growthbook,
		CurrentUserAccess:   buildCurrentUserAccess(),
		IntercomAccountHash: nil,
		Locale:              locale,
		SystemPrompts:       map[string]any{},
		GatedMessages: BootstrapGatedMessages{
			Messages: map[string]any{},
			Gates:    []any{},
			Locale:   "en-US",
		},
		GatedImports:        map[string]any{},
		ServerLocalizations: map[string]string{},
	}
	if orgScoped && account != nil {
		response.Locale = "en-US"
		response.Statsig = nil
		response.Growthbook = nil
	}
	return response
}

func buildGrowthbookFeatures(models platformModelCatalog) map[string]any {
	features := map[string]any{}
	enabledFlags := []string{
		"yukon_silver",
		"yukon_silver_dramatic_shrimp",
		"yukon_silver_dramatic_shrimp_portage",
		"yukon_silver_dramatic_shrimp_clocks",
		"yukon_silver_clocks",
		"yukon_silver_manta_enabled",
		"yukon_silver_admin_settings",
		"cowork_tester_overrides_admin",
		"cowork_default_landing_enabled",
		"cowork_show_tool_permissioning_always_allow",
		"cowork_send_to_tea_boar",
		"cowork_confuse_tea_boar",
		"cowork_auto_permission_mode",
		"cowork_bypass_permissions_mode",
		"cowork_argonaut_org_policies_main",
		"cowork_dispatch_panel",
		"cowork_dispatch_unified_settings_rail",
		"dispatch_child_task_card_main",
		"squares_enabled",
		"squares_enabled_desktop",
		"conway_shell_enabled",
		"mobile_code_notifications_enabled",
		"ccr_code_requires_action_category_enabled",
		"ccr_client_presence_enabled",
		"tengu_blingo_enabled",
		"bad_moon_rising",
		"claude_code_waffles",
		"ccr_cobalt_lantern",
		"cc_carrier_pigeon",
		"claudeai_cc_epitaxy_folder_browser",
		"claude_ai_tamarind",
		"chilling_sloth_clocks",
		"cowork_free_trial",
		"claudeai_mcp_apps_visualize",
		"connectors-directory-bff-migration",
		"internal_tier_selector",
		"internal_test_account_tools_enabled",
		"can_reset_rate_limits",
		"mobile_artifacts_gallery",
		"mobile_android_remote_enabled",
		"mobile_android_send_haptics",
		"mobile_cowork_tab_enabled",
		"mobile_cowork_task_list_enabled",
		"mobile_cowork_task_cards_enabled",
		"mobile_cowork_activity_pill_enabled",
		"mobile_dispatch_sticky_launch",
		"mobile_dispatch_event_cache_enabled",
		"mobile_session_latest_first_pagination",
		"squares_enabled_mobile",
		"yukon_silver_manta_mobile",
		"tibro_enabled",
		"tibro_widget_enabled",
		"claude_ai_tasks_nav",
		"worn_elbow_patch",
		"worn_elbow_share",
		"worn_elbow_clockwork",
		"worn_elbow_courier",
		"worn_elbow_seam",
		"worn_elbow_puma",
		"claudeai_claude_code_penguin_mode_admin",
		"console_enable_claude_code_remote_settings",
		"console_managed_agents",
		"console_starfish",
		"console_sundial",
		"console_reverie",
		"console_papaya",
		"console_saffron",
		"console_tamarind",
		"console_marigold",
		"console_fennel",
		"console_vetiver",
		"console_sesame",
		"console_shiso",
		"console_playground",
		"console_baklava",
		"console_baklava_override",
		"console_baklava_gateway",
		"console_fenugreek",
		"tall_gopher",
		"userauth_oidc_federation_org_enrollment",
	}
	for _, name := range enabledFlags {
		setGrowthbookFeature(features, name, map[string]any{"defaultValue": true})
	}

	setGrowthbookFeature(features, "console_dashboard_discovery_config", bootstrapDashboardDiscoveryFeature(models))
	setGrowthbookFeature(features, "apps_redacted_strings_starfish", bootstrapStarfishRedactedStringsFeature())
	setGrowthbookFeature(features, "apps_redacted_strings_shiso", bootstrapShisoRedactedStringsFeature())

	consoleDefaultModelConfig := map[string]any{
		"defaultValue": map[string]any{
			"model":          models.defaultModelValue(),
			"overrideSticky": true,
			"nuxId":          nil,
		},
	}
	setGrowthbookFeature(features, "console_default_model", consoleDefaultModelConfig)

	modelConfig := map[string]any{
		"defaultValue": map[string]any{
			"allowed_models":           models.modelIDs(),
			"model":                    models.defaultModelValue(),
			"legacy_models":            []string{},
			"supports_1m_context":      []string{},
			"synthetic_allowed_models": map[string]any{},
		},
	}
	setGrowthbookFeature(features, "cowork_model", modelConfig)
	setGrowthbookFeature(features, "ccr_model", modelConfig)
	setGrowthbookFeature(features, "holdup", map[string]any{
		"defaultValue": map[string]any{"modelFallbacks": map[string]any{}},
	})
	setGrowthbookFeature(features, "mobile_cowork_worker_types", map[string]any{
		"defaultValue": map[string]any{"worker_types": []string{"cowork", "claude_code_assistant"}},
	})
	setGrowthbookFeature(features, "claude_ai_cowork_dispatch_homepage_v3_main", map[string]any{
		"defaultValue": map[string]any{"variant": "short_video_alpha_anywhere"},
	})
	setGrowthbookFeature(features, "claude_ai_projects_limits", map[string]any{
		"defaultValue": map[string]any{"max_free_projects": 100},
	})
	features["1578936685"] = bootstrapDefaultWebToolsFeature()
	features["3999619734"] = map[string]any{
		"defaultValue": true,
	}
	features["681353549"] = map[string]any{
		"defaultValue": true,
	}

	features["1525594127"] = map[string]any{
		"defaultValue": true,
	}
	return features
}

func bootstrapDashboardDiscoveryFeature(catalog platformModelCatalog) map[string]any {
	models := make([]map[string]any, 0, len(catalog.models))
	compare := make([]map[string]any, 0, len(catalog.models))
	for _, model := range catalog.models {
		models = append(models, map[string]any{
			"id":    model.ID,
			"match": model.ID,
			"chips": []string{},
		})
		specs := map[string]any{}
		if model.Capabilities.AdaptiveThinking != nil {
			specs["adaptive_thinking"] = *model.Capabilities.AdaptiveThinking
		}
		if model.MaxInputTokens != nil {
			specs["context_window_tokens"] = *model.MaxInputTokens
		}
		if model.MaxTokens != nil {
			specs["max_output_tokens"] = *model.MaxTokens
		}
		compare = append(compare, dashboardCompareModel(model.ID, model.ID, map[string]any{}, specs))
	}
	return map[string]any{
		"defaultValue": map[string]any{
			"enabled": true,
			"models":  models,
			"resources": []map[string]any{
				{"id": "advisor", "badge": "beta"},
				{"id": "batch"},
				{"id": "caching"},
			},
			"compare": compare,
		},
	}
}

func dashboardCompareModel(descriptionKey string, match string, price map[string]any, specs map[string]any) map[string]any {
	return map[string]any{"description_key": descriptionKey, "match": match, "price": price, "specs": specs}
}

func bootstrapStarfishRedactedStringsFeature() map[string]any {
	return map[string]any{"defaultValue": map[string]any{
		"saffron_cove_ember":  "Deployments",
		"slate_current_prism": "deployment_runs",
		"umber_reef_spire":    "deployments",
		"willow_shoal_harbor": "managed-agents-2026-04-01",
	}}
}

func bootstrapShisoRedactedStringsFeature() map[string]any {
	return map[string]any{"defaultValue": map[string]any{
		"birch_hollow_prism":   "At least one host is required for limited networking.",
		"cedar_vale_ribbon":    "api.example.com, *.example.com",
		"dun_creek_spire":      "Limited",
		"fawn_basin_relay":     "Unrestricted",
		"flint_moor_lantern":   "Networking",
		"hazel_tundra_beacon":  "Environment variable",
		"ivory_delta_bramble":  "Allowed hosts",
		"ochre_fjord_thicket":  "Variable name",
		"russet_tundra_signal": "Environment variable",
		"sepia_heath_quill":    "MY_API_KEY",
		"sienna_brook_lattice": "For CLIs, SDKs, or direct API calls. The agent never sees the value.",
		"slate_meadow_cipher":  "The secret can be sent to any host the agent calls. Limiting hosts is strongly recommended.",
		"taupe_ridge_ember":    "Separate hosts with commas or newlines.",
		"umber_glade_whistle":  "Use uppercase letters, numbers, and underscores (e.g., MY_API_KEY).",
	}}
}

func bootstrapDefaultWebToolsFeature() map[string]any {
	tools := []map[string]string{{"name": "repl", "type": "repl_v0"}, {"name": "web_search", "type": "web_search_v0"}}
	return map[string]any{"defaultValue": map[string]any{"completion": tools, "conversation": tools}}
}

func bootstrapGrowthbookHashingAlgorithm(r *http.Request) string {
	if r == nil {
		return "sha256"
	}
	query := r.URL.Query()
	for _, value := range []string{query.Get("growthbook_hashing_algorithm"), query.Get("statsig_hashing_algorithm")} {
		if strings.EqualFold(value, "djb2") {
			return "djb2"
		}
	}
	if strings.EqualFold(query.Get("growthbook_format"), "sdk") {
		return "djb2"
	}
	return "sha256"
}

func setGrowthbookFeature(features map[string]any, name string, feature any) {
	features[name] = feature
	features[growthbookSHA256FeatureKey(name)] = feature
	features[growthbookDJB2FeatureKey(name)] = feature
}

func growthbookSHA256FeatureKey(name string) string {
	sum := sha256.Sum256([]byte(name))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func growthbookDJB2FeatureKey(name string) string {
	var hash uint32
	for _, r := range name {
		hash = hash*31 + uint32(r)
	}
	return strconv.FormatUint(uint64(hash), 10)
}

func buildCurrentUserAccess() CurrentUserAccess {
	permissions := ownerAccountPermissions()
	accountPermissions := make([]CurrentAccountPermission, 0, len(permissions))
	for _, permission := range permissions {
		accountPermissions = append(accountPermissions, CurrentAccountPermission{Permission: permission, Status: "available"})
	}
	return CurrentUserAccess{
		Permissions:        permissions,
		Role:               "owner",
		Features:           currentUserFeatures(),
		AccountPermissions: accountPermissions,
	}
}

func ownerAccountPermissions() []string {
	return []string{
		"members:view", "members:manage", "api:view", "api:manage", "integrations:manage",
		"billing:view", "billing:manage", "cost:view", "usage:view", "invoices:view",
		"organization:manage", "organization:manage_settings", "export:data", "export:members",
		"owners:manage", "workspaces:view", "workspaces:manage", "enterprise_auth:view",
		"enterprise_auth:manage", "limits:view", "membership_admins:manage", "export:audit_logs",
		"security_keys:manage", "compliance:manage", "privacy:view", "privacy:manage",
		"scoped_api_keys:manage", "scim1p:manage", "analytics:view", "workbench:view",
		"library:manage", "workspace:api:resource_manage",
	}
}

func currentUserFeatures() []CurrentUserFeature {
	available := []string{
		"chat", "web_search", "geolocation", "saffron", "wiggle", "skills",
		"mcp_artifacts", "inline_visualizations", "haystack", "interactive_content",
		"thumbs", "claude_code", "claude_code_assistant", "claude_code_fast_mode",
		"claude_code_remote_control", "claude_code_web", "claude_code_desktop",
		"claude_code_desktop_bypass_permissions", "claude_code_desktop_auto_permissions",
		"claude_code_security", "cowork", "work_across_apps", "dittos",
		"tool_approval_default_always_allow", "skill_creation", "claude_code_quick_web_setup",
		"omelette", "claude_browser_extension", "claude_api_in_artifacts", "org_data_export",
		"project_knowledge_search", "drive_cataloging", "claude_code_routines",
		"conversation_search", "operon",
	}
	features := make([]CurrentUserFeature, 0, len(available)+3)
	for _, feature := range available {
		features = append(features, CurrentUserFeature{Feature: feature, Status: "available"})
	}
	for _, feature := range []string{"dxt_allowlist", "claude_code_review", "claude_code_trusted_devices_required"} {
		features = append(features, CurrentUserFeature{Feature: feature, Status: "blocked_by_org_admin"})
	}
	return features
}
