package httpapi

func buildAccount(user UserRecord, orgs []UserOrganizationRecord, preferredOrgUUID string) (Account, string, error) {
	if len(orgs) == 0 {
		return Account{}, "", ErrNotFound
	}

	memberships := make([]Membership, 0, len(orgs))
	selectedOrgUUID := orgs[0].UUID
	for _, org := range orgs {
		if org.UUID == preferredOrgUUID || org.ExternalID == preferredOrgUUID {
			selectedOrgUUID = org.UUID
		}
		createdAt := isoTime(org.AddedAt)
		organization := buildOrganization(org.OrganizationRecord)
		memberships = append(memberships, Membership{
			Organization:            organization,
			Role:                    firstNonEmpty(org.Role, "admin"),
			SeatTier:                "enterprise_standard",
			CreatedAt:               createdAt,
			UpdatedAt:               createdAt,
			NotificationPreferences: buildNotificationPreferences(),
		})
	}

	createdAt := isoTime(user.CreatedAt)
	accountSettings := buildDefaultAccountSettings()
	for key, value := range user.Settings {
		accountSettings[key] = value
	}

	account := Account{
		TaggedID:                  taggedUserID(user.UUID),
		UUID:                      user.UUID,
		EmailAddress:              user.Email,
		FullName:                  user.FullName,
		DisplayName:               user.DisplayName,
		IsVerified:                user.IsVerified,
		AgeIsVerified:             user.AgeIsVerified,
		IsAnonymous:               false,
		CreatedAt:                 createdAt,
		UpdatedAt:                 createdAt,
		Settings:                  accountSettings,
		Memberships:               memberships,
		WorkspaceMemberships:      []any{},
		Invites:                   []any{},
		CompletedVerificationAt:   createdAt,
		AcceptedClickwrapVersions: map[string]any{},
		VerifiedPhoneNumberLast4:  "6754",
	}
	return account, selectedOrgUUID, nil
}

func buildOrganization(org OrganizationRecord) map[string]any {
	createdAt := isoTime(org.CreatedAt)
	updatedAt := createdAt
	if !org.UpdatedAt.IsZero() {
		updatedAt = isoTime(org.UpdatedAt)
	}
	parentOrgUUID := "00000000-0000-4000-8000-000000000001"
	if org.ParentOrganizationUUID != nil && *org.ParentOrganizationUUID != "" {
		parentOrgUUID = *org.ParentOrganizationUUID
	}

	return map[string]any{
		"uuid":              org.UUID,
		"name":              org.Name,
		"organization_type": "claude_enterprise",
		"org_type":          "claude_enterprise",
		"created_at":        createdAt,
		"updated_at":        updatedAt,
		"capabilities": []string{
			"api",
			"raven",
			"chat",
			"claude_pro",
			"claude_max",
			"claude_enterprise",
			"claude_code",
			"claude_code_assistant",
			"claude_code_security",
		},
		"settings":                          buildOrganizationSettingsFromStored(org.Settings),
		"billing_type":                      "stripe_subscription",
		"raven_type":                        "enterprise",
		"visibility_status":                 nil,
		"rate_limit_tier":                   "default_claude_ai",
		"free_credits_status":               "available",
		"data_retention":                    "default",
		"api_disabled_reason":               nil,
		"api_disabled_until":                nil,
		"billable_usage_paused_until":       nil,
		"rate_limit_upsell":                 nil,
		"seat_tier":                         "enterprise_standard",
		"parent_organization_uuid":          parentOrgUUID,
		"active_flags":                      []any{},
		"has_icon":                          false,
		"external_mapping":                  nil,
		"raven_configuration":               nil,
		"merchant_of_record":                "anthropic",
		"claude_ai_bootstrap_models_config": buildBootstrapModelsConfig(),
	}
}

type BootstrapModelOption struct {
	Model           string                        `json:"model"`
	Name            string                        `json:"name"`
	Description     string                        `json:"description,omitempty"`
	Overflow        bool                          `json:"overflow,omitempty"`
	Inactive        bool                          `json:"inactive,omitempty"`
	ThinkingModes   []BootstrapThinkingModeOption `json:"thinking_modes"`
	Capabilities    *BootstrapModelCapabilities   `json:"capabilities,omitempty"`
	NoticeText      string                        `json:"notice_text,omitempty"`
	KnowledgeCutoff string                        `json:"knowledgeCutoff,omitempty"`
	PaprikaModes    []string                      `json:"paprika_modes"`
	HardLimit       int                           `json:"hard_limit,omitempty"`
}

type BootstrapThinkingModeOption struct {
	Description      string `json:"description"`
	ID               string `json:"id"`
	IsDefault        bool   `json:"is_default"`
	Mode             string `json:"mode"`
	PaprikaModeValue string `json:"paprika_mode_value"`
	SelectionTitle   string `json:"selection_title"`
	Title            string `json:"title"`
}

type BootstrapModelCapabilities struct {
	MMPDF       bool `json:"mm_pdf"`
	MMImages    bool `json:"mm_images"`
	WebSearch   bool `json:"web_search"`
	GSuiteTools bool `json:"gsuite_tools"`
	Compass     bool `json:"compass"`
}

func buildBootstrapModelsConfig() []BootstrapModelOption {
	return []BootstrapModelOption{
		{Model: "claude-fable-5", Name: "Claude Fable 5", Description: "For your toughest challenges", PaprikaModes: []string{"extended"}, ThinkingModes: adaptiveThinkingModes(), HardLimit: 449000},
		{Model: "claude-opus-4-8", Name: "Claude Opus 4.8", Description: "For complex tasks", NoticeText: "Opus consumes usage limits faster than other models", PaprikaModes: []string{"extended"}, ThinkingModes: adaptiveThinkingModes(), HardLimit: 449000},
		{Model: "claude-opus-4-5-20251101", Name: "Claude Opus 4.5", Inactive: true, NoticeText: "Opus consumes usage limits faster than other models", PaprikaModes: []string{"extended"}, ThinkingModes: extendedThinkingModes(), HardLimit: 190000},
		{Model: "claude-opus-4-1-20250805-claude-ai", Name: "Opus 4.1", Inactive: true, NoticeText: "Opus consumes usage limits faster than other models", PaprikaModes: []string{"extended"}, ThinkingModes: extendedThinkingModes(), HardLimit: 190000},
		{Model: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6", Description: "Most efficient for everyday tasks", PaprikaModes: []string{"extended"}, ThinkingModes: adaptiveThinkingModes(), HardLimit: 449000},
		{Model: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Inactive: true, PaprikaModes: []string{"extended"}, ThinkingModes: extendedThinkingModes(), HardLimit: 190000},
		{Model: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Description: "Fastest for quick answers", PaprikaModes: []string{"extended"}, ThinkingModes: extendedThinkingModes(), HardLimit: 190000},
		{Model: "claude-opus-4-7", Name: "Claude Opus 4.7", Overflow: true, NoticeText: "Opus consumes usage limits faster than other models", PaprikaModes: []string{"extended"}, ThinkingModes: adaptiveThinkingModes(), HardLimit: 449000},
		{Model: "claude-opus-4-6", Name: "Claude Opus 4.6", Overflow: true, NoticeText: "Opus consumes usage limits faster than other models", PaprikaModes: []string{"extended"}, ThinkingModes: extendedThinkingModes(), HardLimit: 449000},
		{Model: "claude-3-opus-20240229", Name: "Claude Opus 3", Overflow: true, NoticeText: "Opus consumes usage limits faster than other models", PaprikaModes: []string{}, ThinkingModes: []BootstrapThinkingModeOption{}, KnowledgeCutoff: "August 2023", Capabilities: &BootstrapModelCapabilities{}, HardLimit: 190000},
	}
}

func adaptiveThinkingModes() []BootstrapThinkingModeOption {
	return []BootstrapThinkingModeOption{{
		Description:      "Can think for more complex tasks",
		ID:               "auto",
		IsDefault:        false,
		Mode:             "extended",
		PaprikaModeValue: "extended",
		SelectionTitle:   "Thinking",
		Title:            "Thinking",
	}}
}

func extendedThinkingModes() []BootstrapThinkingModeOption {
	return []BootstrapThinkingModeOption{{
		Description:      "Think longer for complex tasks",
		ID:               "extended",
		IsDefault:        false,
		Mode:             "extended",
		PaprikaModeValue: "extended",
		SelectionTitle:   "Extended",
		Title:            "Extended thinking",
	}}
}

func buildOrganizationSettings() map[string]any {
	return map[string]any{
		"account_session_duration_seconds":          nil,
		"allowed_invite_domains":                    nil,
		"batches_download_ui_enabled_workspace_ids": []any{},
		"batches_download_ui_visibility":            "all",
		"browser_extension_settings":                nil,
		"claude_ai_ccr_sharing_enabled":             true,
		"claude_ai_chat_sharing_enabled":            true,
		"claude_ai_completion_feedback_enabled":     true,
		"claude_ai_integration_sharing_enabled":     true,
		"claude_ai_omelette_enabled":                nil,
		"claude_ai_operon_enabled":                  nil,
		"claude_ai_skill_creation_enabled":          nil,
		"claude_ai_skill_sharing_enabled":           nil,
		"claude_ai_skill_sharing_org_enabled":       nil,
		"claude_code_default_worker_environment_id": "env_default",
		"claude_code_default_worker_pool_id":        nil,
		"claude_code_github_analytics_enabled":      true,
		"claude_code_hide_managed_environments":     false,
		"claude_code_metrics_logging_enabled":       true,
		"claude_code_penguin_mode_enabled":          true,
		"claude_code_quick_web_setup_enabled":       true,
		"claude_code_remote_control_enabled":        true,
		"claude_code_routines_enabled":              true,
		"claude_code_trusted_devices_required":      false,
		"claude_console_privacy":                    "default_private",
		"default_workspace_settings":                map[string]any{"enable_api_keys": true},
		"disabled_admin_request_types":              nil,
		"frontier_services_data_use_enabled":        nil,
		"inline_visualizations_enabled":             nil,
		"is_desktop_extension_allowlist_enabled":    false,
		"lti_course_projects_enabled":               nil,
		"managed_agents_enabled":                    true,
		"oc_overage_credit_claimed":                 nil,
		"sampling_restriction":                      nil,
		"system_preferences_prompt":                 nil,
		"vcs_connections":                           nil,
		"work_across_apps_enabled":                  nil,
		"workbench_completion_feedback_enabled":     nil,
	}
}

func buildOrganizationSettingsFromStored(stored map[string]any) map[string]any {
	settings := buildOrganizationSettings()
	mergeOrganizationSettings(settings, stored)
	defaultWorkspaceSettings, ok := settings["default_workspace_settings"].(map[string]any)
	if !ok || defaultWorkspaceSettings == nil {
		defaultWorkspaceSettings = map[string]any{}
		settings["default_workspace_settings"] = defaultWorkspaceSettings
	}
	if _, ok := defaultWorkspaceSettings["enable_api_keys"]; !ok {
		defaultWorkspaceSettings["enable_api_keys"] = true
	}
	return settings
}

func mergeOrganizationSettings(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if valueMap, ok := value.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				mergeOrganizationSettings(dstMap, valueMap)
				continue
			}
			dst[key] = cloneAnyMap(valueMap)
			continue
		}
		dst[key] = value
	}
}

func cloneAnyMap(value map[string]any) map[string]any {
	out := make(map[string]any, len(value))
	for key, item := range value {
		if itemMap, ok := item.(map[string]any); ok {
			out[key] = cloneAnyMap(itemMap)
			continue
		}
		out[key] = item
	}
	return out
}

func buildDefaultAccountSettings() map[string]any {
	return map[string]any{
		"browser_extension_settings":       nil,
		"ccr_auto_archive_on_pr_close":     nil,
		"ccr_auto_create_pr_as_draft":      nil,
		"ccr_auto_create_pr_on_push":       nil,
		"ccr_autofix_on_pr_create":         nil,
		"ccr_persistent_memory":            nil,
		"ccr_plugins_mount":                nil,
		"ccr_session_state_buckets":        nil,
		"ccr_sharing_auto_share_on_pr":     nil,
		"ccr_sharing_enforce_repo_check":   nil,
		"ccr_sharing_show_display_name":    nil,
		"cowork_onboarding_completed_at":   nil,
		"cowork_sms_enabled":               nil,
		"default_model":                    nil,
		"dismissed_artifact_feedback_form": nil,
		"dismissed_artifacts_announcement": nil,
		"dismissed_claude_code_spotlight":  nil,
		"dismissed_claudeai_banners":       []any{},
		"dismissed_saffron_themes":         true,
		"dittos_mobile_onboarding_seen_at": nil,
		"enable_chat_suggestions":          nil,
		"enabled_artifacts_attachments":    false,
		"enabled_bananagrams":              nil,
		"enabled_cli_ops":                  nil,
		"enabled_compass":                  nil,
		"enabled_connector_suggestions":    nil,
		"enabled_foccacia":                 nil,
		"enabled_full_thinking":            nil,
		"enabled_gdrive":                   nil,
		"enabled_gdrive_indexing":          nil,
		"enabled_geolocation":              nil,
		"enabled_mcp_tools":                nil,
		"enabled_megaminds":                nil,
		"enabled_melange":                  nil,
		"enabled_mm_pdfs":                  nil,
		"enabled_saffron":                  nil,
		"enabled_saffron_search":           true,
		"enabled_sourdough":                nil,
		"enabled_turmeric":                 nil,
		"enabled_web_search":               true,
		"enabled_wiggle_egress":            nil,
		"grove_enabled":                    true,
		"has_finished_claudeai_onboarding": true,
		"tool_search_mode":                 "auto",
		"paprika_mode":                     "extended",
	}
}

func buildNotificationPreferences() map[string]any {
	featurePreference := map[string]any{}
	for _, name := range []string{
		"compass",
		"bogosort",
		"code_requires_action",
		"code_security_scan",
		"completion",
		"tool_notification",
		"project_sharing",
		"orbit_insight",
		"orbit_widget_refresh",
		"dispatch",
		"assist",
		"conway",
		"marketing",
	} {
		featurePreference[name] = map[string]any{"enable_email": nil, "enable_push": nil}
	}
	return map[string]any{"feature_preference": featurePreference}
}
