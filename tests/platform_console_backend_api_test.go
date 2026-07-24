package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlatformConsoleBackendMigratedRoutes(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("platform-console-backend-bucket"))
	defer app.close()

	orgUUID := loadDefaultOrganizationUUID(t, app)
	otherOrgUUID := seedConsoleDefaultWorkspace(t, app, "org_console_backend_other", "workspace_console_backend_other_default")
	orgPath := "/api/organizations/" + orgUUID
	consoleOrgPath := "/api/console/organizations/" + orgUUID
	cookies := app.platformLoginCookies(t, "console-backend@example.com")

	t.Run("success platform account and billing routes", func(t *testing.T) {
		bootstrapResp := app.platformRequest(t, http.MethodGet, "/api/bootstrap", nil, nil)
		defer bootstrapResp.Body.Close()
		if bootstrapResp.StatusCode != http.StatusOK {
			t.Fatalf("bootstrap status = %d, want 200: %s", bootstrapResp.StatusCode, readAll(t, bootstrapResp.Body))
		}
		var bootstrap map[string]any
		decodeJSON(t, bootstrapResp.Body, &bootstrap)
		if bootstrap["account"] != nil || bootstrap["statsig"] == nil || bootstrap["growthbook"] == nil {
			t.Fatalf("bootstrap = %#v, want anonymous bootstrap with statsig and growthbook", bootstrap)
		}

		appStartPath := "/api/bootstrap/" + orgUUID + "/app_start?statsig_hashing_algorithm=djb2&growthbook_format=sdk&include_system_prompts=false"
		appStartResp := app.platformRequest(t, http.MethodGet, appStartPath, nil, cookies)
		defer appStartResp.Body.Close()
		if appStartResp.StatusCode != http.StatusOK {
			t.Fatalf("bootstrap app_start status = %d, want 200: %s", appStartResp.StatusCode, readAll(t, appStartResp.Body))
		}
		var appStart map[string]any
		decodeJSON(t, appStartResp.Body, &appStart)
		account, ok := appStart["account"].(map[string]any)
		if !ok || account["uuid"] == "" || account["email_address"] == "" {
			t.Fatalf("bootstrap app_start account = %#v, want populated account", appStart["account"])
		}
		if account["full_name"] == "" || account["display_name"] == "" {
			t.Fatalf("bootstrap app_start account names = %#v/%#v, want onboarding-complete names", account["full_name"], account["display_name"])
		}
		if appStart["statsig"] != nil || appStart["growthbook"] != nil {
			t.Fatalf("bootstrap app_start = %#v, want org-scoped response without top-level statsig/growthbook", appStart)
		}
		orgGrowthbook, ok := appStart["org_growthbook"].(map[string]any)
		if !ok || orgGrowthbook["hashing_algorithm"] != "djb2" {
			t.Fatalf("bootstrap app_start org_growthbook = %#v, want djb2 hashing", appStart["org_growthbook"])
		}
		currentUserAccess, ok := appStart["current_user_access"].(map[string]any)
		if !ok || currentUserAccess["role"] != "owner" {
			t.Fatalf("bootstrap app_start current_user_access = %#v, want owner role", appStart["current_user_access"])
		}
		memberships, ok := account["memberships"].([]any)
		if !ok || len(memberships) == 0 {
			t.Fatalf("bootstrap app_start memberships = %#v, want at least one membership", account["memberships"])
		}
		membership, ok := memberships[0].(map[string]any)
		if !ok {
			t.Fatalf("bootstrap app_start membership = %#v, want object", memberships[0])
		}
		organization, ok := membership["organization"].(map[string]any)
		if !ok || organization["uuid"] != orgUUID {
			t.Fatalf("bootstrap app_start organization = %#v, want uuid %s", membership["organization"], orgUUID)
		}
		modelsConfig, ok := organization["claude_ai_bootstrap_models_config"].([]any)
		if !ok || len(modelsConfig) != 1 {
			t.Fatalf("bootstrap app_start models config = %#v, want one catalog model", organization["claude_ai_bootstrap_models_config"])
		}
		model, ok := modelsConfig[0].(map[string]any)
		if !ok || model["model"] != "test/model" || model["name"] != "Test model" {
			t.Fatalf("bootstrap app_start model = %#v, want test catalog model", modelsConfig[0])
		}
		features, ok := orgGrowthbook["features"].(map[string]any)
		if !ok {
			t.Fatalf("bootstrap app_start features = %#v, want object", orgGrowthbook["features"])
		}
		defaultModelFeature, ok := features["console_default_model"].(map[string]any)
		if !ok {
			t.Fatalf("console_default_model = %#v, want object", features["console_default_model"])
		}
		defaultModelValue, ok := defaultModelFeature["defaultValue"].(map[string]any)
		if !ok || defaultModelValue["model"] != "test/model" {
			t.Fatalf("console_default_model defaultValue = %#v, want catalog default", defaultModelFeature["defaultValue"])
		}

		missingOrgAppStartResp := app.platformRequest(t, http.MethodGet, "/api/bootstrap/7482d00f-2e42-478b-b2db-07c3d056a3b6/app_start", nil, cookies)
		defer missingOrgAppStartResp.Body.Close()
		if missingOrgAppStartResp.StatusCode != http.StatusOK {
			t.Fatalf("bootstrap missing org app_start status = %d, want 200: %s", missingOrgAppStartResp.StatusCode, readAll(t, missingOrgAppStartResp.Body))
		}
		var missingOrgAppStart map[string]any
		decodeJSON(t, missingOrgAppStartResp.Body, &missingOrgAppStart)
		missingOrgAccount, ok := missingOrgAppStart["account"].(map[string]any)
		if !ok || missingOrgAccount["full_name"] == "" || missingOrgAccount["display_name"] == "" {
			t.Fatalf("bootstrap missing org app_start account = %#v, want populated account", missingOrgAppStart["account"])
		}

		bannersResp := app.platformRequest(t, http.MethodGet, "/api/banners", nil, cookies)
		defer bannersResp.Body.Close()
		if bannersResp.StatusCode != http.StatusOK {
			t.Fatalf("banners status = %d, want 200: %s", bannersResp.StatusCode, readAll(t, bannersResp.Body))
		}
		var banners map[string]any
		decodeJSON(t, bannersResp.Body, &banners)
		if len(banners) != 0 {
			t.Fatalf("banners = %#v, want empty object", banners)
		}

		stripeResp := app.platformRequest(t, http.MethodGet, "/api/billing/stripe_region?country=US&organization_uuid="+orgUUID, nil, cookies)
		defer stripeResp.Body.Close()
		if stripeResp.StatusCode != http.StatusOK {
			t.Fatalf("stripe region status = %d, want 200: %s", stripeResp.StatusCode, readAll(t, stripeResp.Body))
		}
		var stripeRegion map[string]any
		decodeJSON(t, stripeResp.Body, &stripeRegion)
		if stripeRegion["stripe_region"] != "us" {
			t.Fatalf("stripe region = %#v, want us", stripeRegion)
		}
	})

	t.Run("success organization onboarding and billing routes", func(t *testing.T) {
		orgResp := app.platformRequest(t, http.MethodGet, orgPath, nil, cookies)
		defer orgResp.Body.Close()
		if orgResp.StatusCode != http.StatusOK {
			t.Fatalf("organization status = %d, want 200: %s", orgResp.StatusCode, readAll(t, orgResp.Body))
		}
		var organization map[string]any
		decodeJSON(t, orgResp.Body, &organization)
		if organization["uuid"] != orgUUID || organization["organization_type"] != "claude_enterprise" || organization["claude_ai_bootstrap_models_config"] == nil {
			t.Fatalf("organization = %#v, want source-compatible organization body", organization)
		}
		modelsConfig, ok := organization["claude_ai_bootstrap_models_config"].([]any)
		if !ok || len(modelsConfig) != 1 {
			t.Fatalf("organization models config = %#v, want test catalog model", organization["claude_ai_bootstrap_models_config"])
		}
		model, ok := modelsConfig[0].(map[string]any)
		if !ok || model["model"] != "test/model" {
			t.Fatalf("organization model = %#v, want test/model", modelsConfig[0])
		}
		settings, ok := organization["settings"].(map[string]any)
		if !ok {
			t.Fatalf("organization settings = %#v, want object", organization["settings"])
		}
		defaultWorkspaceSettings, ok := settings["default_workspace_settings"].(map[string]any)
		if !ok {
			t.Fatalf("default workspace settings = %#v, want object", settings["default_workspace_settings"])
		}
		if _, ok := defaultWorkspaceSettings["enable_api_keys"].(bool); !ok {
			t.Fatalf("default workspace settings = %#v, want boolean enable_api_keys", settings["default_workspace_settings"])
		}

		emptyNameResp := app.platformRequest(t, http.MethodPut, orgPath, strings.NewReader(`{"name":" "}`), cookies)
		defer emptyNameResp.Body.Close()
		if emptyNameResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("empty organization name status = %d, want 400: %s", emptyNameResp.StatusCode, readAll(t, emptyNameResp.Body))
		}

		updateOrgResp := app.platformRequest(t, http.MethodPut, orgPath, strings.NewReader(`{
			"name": " Open Managed Agent Labs ",
			"default_workspace_settings": {"enable_api_keys": false}
		}`), cookies)
		defer updateOrgResp.Body.Close()
		if updateOrgResp.StatusCode != http.StatusOK {
			t.Fatalf("update organization status = %d, want 200: %s", updateOrgResp.StatusCode, readAll(t, updateOrgResp.Body))
		}
		var updatedOrganization map[string]any
		decodeJSON(t, updateOrgResp.Body, &updatedOrganization)
		updatedSettings, _ := updatedOrganization["settings"].(map[string]any)
		updatedDefaultWorkspaceSettings, _ := updatedSettings["default_workspace_settings"].(map[string]any)
		if updatedOrganization["name"] != "Open Managed Agent Labs" || updatedDefaultWorkspaceSettings["enable_api_keys"] != false {
			t.Fatalf("updated organization = %#v, want persisted name and enable_api_keys false", updatedOrganization)
		}

		reloadOrgResp := app.platformRequest(t, http.MethodGet, orgPath, nil, cookies)
		defer reloadOrgResp.Body.Close()
		if reloadOrgResp.StatusCode != http.StatusOK {
			t.Fatalf("reload organization status = %d, want 200: %s", reloadOrgResp.StatusCode, readAll(t, reloadOrgResp.Body))
		}
		var reloadedOrganization map[string]any
		decodeJSON(t, reloadOrgResp.Body, &reloadedOrganization)
		reloadedSettings, _ := reloadedOrganization["settings"].(map[string]any)
		reloadedDefaultWorkspaceSettings, _ := reloadedSettings["default_workspace_settings"].(map[string]any)
		if reloadedOrganization["name"] != "Open Managed Agent Labs" || reloadedDefaultWorkspaceSettings["enable_api_keys"] != false {
			t.Fatalf("reloaded organization = %#v, want persisted organization update", reloadedOrganization)
		}

		clearProfileResp := app.platformRequest(t, http.MethodPut, orgPath+"/profile", strings.NewReader(`{
			"physical_address": null,
			"website": null,
			"industry": null,
			"bill_to": null,
			"remove_tax_id": true
		}`), cookies)
		defer clearProfileResp.Body.Close()
		if clearProfileResp.StatusCode != http.StatusOK {
			t.Fatalf("clear profile status = %d, want 200: %s", clearProfileResp.StatusCode, readAll(t, clearProfileResp.Body))
		}

		profileResp := app.platformRequest(t, http.MethodGet, orgPath+"/profile", nil, cookies)
		defer profileResp.Body.Close()
		if profileResp.StatusCode != http.StatusOK {
			t.Fatalf("profile status = %d, want 200: %s", profileResp.StatusCode, readAll(t, profileResp.Body))
		}
		var emptyProfile map[string]any
		decodeJSON(t, profileResp.Body, &emptyProfile)
		for _, key := range []string{"physical_address", "website", "industry", "tax_id", "bill_to"} {
			if value, ok := emptyProfile[key]; !ok || value != nil {
				t.Fatalf("empty profile %s = %#v, want null in %#v", key, value, emptyProfile)
			}
		}

		profilePayload := `{
			"physical_address": {
				"line1": " 1 Main St ",
				"line2": "",
				"city": " San Francisco ",
				"state": " CA ",
				"country": " US ",
				"postal_code": " 94105 "
			},
			"tax_id": {"type": "us_ein", "value": " 12-3456789 ", "country": " US "},
			"website": " https://example.com ",
			"industry": " AI ",
			"bill_to": " Finance "
		}`
		updateProfileResp := app.platformRequest(t, http.MethodPut, orgPath+"/profile", strings.NewReader(profilePayload), cookies)
		defer updateProfileResp.Body.Close()
		if updateProfileResp.StatusCode != http.StatusOK {
			t.Fatalf("update profile status = %d, want 200: %s", updateProfileResp.StatusCode, readAll(t, updateProfileResp.Body))
		}
		var savedProfile map[string]any
		decodeJSON(t, updateProfileResp.Body, &savedProfile)
		address, _ := savedProfile["physical_address"].(map[string]any)
		if address["line1"] != "1 Main St" || address["line2"] != nil || address["postal_code"] != "94105" {
			t.Fatalf("address = %#v, want normalized address", address)
		}
		taxID, _ := savedProfile["tax_id"].(map[string]any)
		if taxID["type"] != "us_ein" || taxID["value"] != "12-3456789" || taxID["country"] != "US" {
			t.Fatalf("tax_id = %#v, want normalized tax id", taxID)
		}
		if savedProfile["website"] != "https://example.com" || savedProfile["industry"] != "AI" || savedProfile["bill_to"] != "Finance" {
			t.Fatalf("profile strings = %#v, want normalized strings", savedProfile)
		}

		reloadProfileResp := app.platformRequest(t, http.MethodGet, orgPath+"/profile", nil, cookies)
		defer reloadProfileResp.Body.Close()
		if reloadProfileResp.StatusCode != http.StatusOK {
			t.Fatalf("reload profile status = %d, want 200: %s", reloadProfileResp.StatusCode, readAll(t, reloadProfileResp.Body))
		}
		var reloadedProfile map[string]any
		decodeJSON(t, reloadProfileResp.Body, &reloadedProfile)
		reloadedAddress, _ := reloadedProfile["physical_address"].(map[string]any)
		if reloadedAddress["line1"] != "1 Main St" || reloadedProfile["website"] != "https://example.com" {
			t.Fatalf("reloaded profile = %#v, want persisted profile", reloadedProfile)
		}

		removeTaxIDResp := app.platformRequest(t, http.MethodPut, orgPath+"/profile", strings.NewReader(`{"remove_tax_id": true}`), cookies)
		defer removeTaxIDResp.Body.Close()
		if removeTaxIDResp.StatusCode != http.StatusOK {
			t.Fatalf("remove tax id status = %d, want 200: %s", removeTaxIDResp.StatusCode, readAll(t, removeTaxIDResp.Body))
		}
		var removedProfile map[string]any
		decodeJSON(t, removeTaxIDResp.Body, &removedProfile)
		if removedProfile["tax_id"] != nil {
			t.Fatalf("tax_id = %#v, want nil after remove_tax_id", removedProfile["tax_id"])
		}

		ssoResp := app.platformRequest(t, http.MethodGet, orgPath+"/enterprise_auth/v2/sso_settings", nil, cookies)
		defer ssoResp.Body.Close()
		if ssoResp.StatusCode != http.StatusOK {
			t.Fatalf("sso settings status = %d, want 200: %s", ssoResp.StatusCode, readAll(t, ssoResp.Body))
		}
		var sso map[string]any
		decodeJSON(t, ssoResp.Body, &sso)
		if sso["organization_uuid"] != orgUUID || sso["sso_enabled"] != false || sso["provisioning_mode"] != "LOGIN_ONLY" {
			t.Fatalf("sso settings = %#v, want default source-compatible settings", sso)
		}
		for _, key := range []string{"group_role_mappings", "group_seat_tier_mappings", "organization_cached_idp_groups"} {
			values, ok := sso[key].([]any)
			if !ok || len(values) != 0 {
				t.Fatalf("sso %s = %#v, want empty array", key, sso[key])
			}
		}

		ssoPayload := `{
			"sso_enabled": true,
			"claudeai_sso_enforced": true,
			"console_sso_enforced": true,
			"provisioning_mode": "SCIM_PERMISSIVE",
			"directory_sync_enabled": true,
			"organization_creation_blocked": true,
			"seat_tier_assignment_enabled": true,
			"group_role_mappings": [{"group_name": "admins", "role": "admin"}],
			"group_seat_tier_mappings": [{"group_name": "admins", "seat_tier": "enterprise_standard"}],
			"organization_cached_idp_groups": [" admins ", "", "admins", "engineers"]
		}`
		updateSSOResp := app.platformRequest(t, http.MethodPut, orgPath+"/enterprise_auth/v2/sso_settings", strings.NewReader(ssoPayload), cookies)
		defer updateSSOResp.Body.Close()
		if updateSSOResp.StatusCode != http.StatusOK {
			t.Fatalf("update sso settings status = %d, want 200: %s", updateSSOResp.StatusCode, readAll(t, updateSSOResp.Body))
		}
		var updatedSSO map[string]any
		decodeJSON(t, updateSSOResp.Body, &updatedSSO)
		if updatedSSO["sso_enabled"] != true || updatedSSO["provisioning_mode"] != "SCIM_PERMISSIVE" || updatedSSO["directory_sync_enabled"] != true {
			t.Fatalf("updated sso settings = %#v, want saved settings", updatedSSO)
		}
		roleMappings, ok := updatedSSO["group_role_mappings"].([]any)
		if !ok || len(roleMappings) != 1 {
			t.Fatalf("group role mappings = %#v, want one mapping", updatedSSO["group_role_mappings"])
		}
		cachedGroups, ok := updatedSSO["organization_cached_idp_groups"].([]any)
		if !ok || len(cachedGroups) != 2 || cachedGroups[0] != "admins" || cachedGroups[1] != "engineers" {
			t.Fatalf("cached groups = %#v, want compacted groups", updatedSSO["organization_cached_idp_groups"])
		}

		onboardingResp := app.platformRequest(t, http.MethodGet, orgPath+"/console_onboarding/tasks", nil, cookies)
		defer onboardingResp.Body.Close()
		if onboardingResp.StatusCode != http.StatusOK {
			t.Fatalf("onboarding status = %d, want 200: %s", onboardingResp.StatusCode, readAll(t, onboardingResp.Body))
		}
		var onboarding map[string]any
		decodeJSON(t, onboardingResp.Body, &onboarding)
		if onboarding["panel_state"] != "open" || onboarding["panel_updated_at"] != nil {
			t.Fatalf("onboarding = %#v, want open panel with nil updated at", onboarding)
		}
		if tasks, ok := onboarding["tasks"].([]any); !ok || len(tasks) != 0 {
			t.Fatalf("onboarding tasks = %#v, want empty array", onboarding["tasks"])
		}

		setupResp := app.platformRequest(t, http.MethodGet, orgPath+"/setup_required", nil, cookies)
		defer setupResp.Body.Close()
		if setupResp.StatusCode != http.StatusOK {
			t.Fatalf("setup status = %d, want 200: %s", setupResp.StatusCode, readAll(t, setupResp.Body))
		}
		var setup map[string]any
		decodeJSON(t, setupResp.Body, &setup)
		if setup["setup_required"] != false {
			t.Fatalf("setup = %#v, want setup_required false", setup)
		}

		experiencesResp := app.platformRequest(t, http.MethodGet, orgPath+"/experiences/claude_console?locale=en-US", nil, cookies)
		defer experiencesResp.Body.Close()
		if experiencesResp.StatusCode != http.StatusOK {
			t.Fatalf("experiences status = %d, want 200: %s", experiencesResp.StatusCode, readAll(t, experiencesResp.Body))
		}
		var experiences map[string]any
		decodeJSON(t, experiencesResp.Body, &experiences)
		if values, ok := experiences["experiences"].([]any); !ok || len(values) != 0 {
			t.Fatalf("experiences = %#v, want empty array", experiences["experiences"])
		}
		rules, ok := experiences["rules"].(map[string]any)
		if !ok {
			t.Fatalf("experience rules = %#v, want object", experiences["rules"])
		}
		global, ok := rules["global"].(map[string]any)
		if !ok || global["cooldown"] != nil {
			t.Fatalf("experience global rules = %#v, want cooldown nil", rules["global"])
		}
		globalRateLimit, ok := global["rate_limit"].(map[string]any)
		if !ok || globalRateLimit["remaining"] != float64(0) || globalRateLimit["reset_at"] == "" {
			t.Fatalf("experience global rate_limit = %#v, want source-compatible rate limit", global["rate_limit"])
		}
		placements, ok := rules["placements"].(map[string]any)
		if !ok || placements["home-nudge"] == nil || placements["spotlight"] == nil || placements["chat-tooltip"] == nil || placements["cowork"] == nil || placements["global-banner"] == nil || placements["admin-capability-tooltip"] == nil {
			t.Fatalf("experience placements = %#v, want source-compatible placement keys", rules["placements"])
		}
		if tiers, ok := rules["tiers"].(map[string]any); !ok || len(tiers) != 0 {
			t.Fatalf("experience tiers = %#v, want empty object", rules["tiers"])
		}

		currentSpendResp := app.platformRequest(t, http.MethodGet, orgPath+"/current_spend", nil, cookies)
		defer currentSpendResp.Body.Close()
		if currentSpendResp.StatusCode != http.StatusOK {
			t.Fatalf("current spend status = %d, want 200: %s", currentSpendResp.StatusCode, readAll(t, currentSpendResp.Body))
		}
		var currentSpend map[string]any
		decodeJSON(t, currentSpendResp.Body, &currentSpend)
		if currentSpend["amount"] != float64(0) || currentSpend["credit_balance"] != float64(100) || currentSpend["monthly_limit"] != float64(50000) || currentSpend["resets_at"] == "" {
			t.Fatalf("current spend = %#v, want available credits with reset timestamp", currentSpend)
		}

		creditsResp := app.platformRequest(t, http.MethodGet, orgPath+"/prepaid/credits", nil, cookies)
		defer creditsResp.Body.Close()
		if creditsResp.StatusCode != http.StatusOK {
			t.Fatalf("prepaid credits status = %d, want 200: %s", creditsResp.StatusCode, readAll(t, creditsResp.Body))
		}
		var credits map[string]any
		decodeJSON(t, creditsResp.Body, &credits)
		if credits["amount"] != float64(100) || credits["currency"] != "usd" || credits["auto_reload_settings"] != nil {
			t.Fatalf("credits = %#v, want source-compatible available prepaid credits", credits)
		}
	})

	t.Run("success organization analytics routes", func(t *testing.T) {
		usagePath := orgPath + "/usage_activities?starting_on=2026-06-03&ending_before=2026-06-17&categories=true&granularity=daily"
		usageResp := app.platformRequest(t, http.MethodGet, usagePath, nil, cookies)
		defer usageResp.Body.Close()
		if usageResp.StatusCode != http.StatusOK {
			t.Fatalf("usage activities status = %d, want 200: %s", usageResp.StatusCode, readAll(t, usageResp.Body))
		}
		var usage map[string]any
		decodeJSON(t, usageResp.Body, &usage)
		if usage["granularity"] != "daily" {
			t.Fatalf("usage = %#v, want daily granularity", usage)
		}
		if usages, ok := usage["usages"].(map[string]any); !ok || len(usages) != 0 {
			t.Fatalf("usage usages = %#v, want empty object", usage["usages"])
		}

		for _, usageCostPath := range []string{
			orgPath + "/usage_cost?starting_on=2026-06-01&ending_before=2026-07-01&group_by=model",
			orgPath + "/workspaces/default/usage_cost?starting_on=2026-06-01&ending_before=2026-07-01&group_by=api_key_id",
		} {
			usageCostResp := app.platformRequest(t, http.MethodGet, usageCostPath, nil, cookies)
			defer usageCostResp.Body.Close()
			if usageCostResp.StatusCode != http.StatusOK {
				t.Fatalf("usage cost status = %d, want 200: %s", usageCostResp.StatusCode, readAll(t, usageCostResp.Body))
			}
			var usageCost map[string]any
			decodeJSON(t, usageCostResp.Body, &usageCost)
			for _, key := range []string{"costs", "web_search_costs", "code_execution_costs", "session_usage_costs", "claude_code_savings"} {
				values, ok := usageCost[key].(map[string]any)
				if !ok || len(values) != 0 {
					t.Fatalf("usage cost %s = %#v, want empty object", key, usageCost[key])
				}
			}
		}

		apiKeysUsagePath := orgPath + "/workspaces/default/api_keys/usage"
		apiKeysUsageResp := app.platformRequest(t, http.MethodGet, apiKeysUsagePath, nil, cookies)
		defer apiKeysUsageResp.Body.Close()
		if apiKeysUsageResp.StatusCode != http.StatusOK {
			t.Fatalf("api keys usage status = %d, want 200: %s", apiKeysUsageResp.StatusCode, readAll(t, apiKeysUsageResp.Body))
		}
		var apiKeysUsage map[string]any
		decodeJSON(t, apiKeysUsageResp.Body, &apiKeysUsage)
		if apiKeysUsage["cutoff_days"] != float64(30) {
			t.Fatalf("api keys usage = %#v, want cutoff_days 30", apiKeysUsage)
		}
		apiKeys, ok := apiKeysUsage["api_keys"].([]any)
		if !ok || len(apiKeys) != 0 {
			t.Fatalf("api keys usage api_keys = %#v, want empty array", apiKeysUsage["api_keys"])
		}

		cachePath := orgPath + "/cache_analytics?starting_on=2026-06-10&ending_before=2026-06-17&granularity=daily&group_by=model&series_limit=1&workspaces=default"
		cacheResp := app.platformRequest(t, http.MethodGet, cachePath, nil, cookies)
		defer cacheResp.Body.Close()
		if cacheResp.StatusCode != http.StatusOK {
			t.Fatalf("cache analytics status = %d, want 200: %s", cacheResp.StatusCode, readAll(t, cacheResp.Body))
		}
		var cache map[string]any
		decodeJSON(t, cacheResp.Body, &cache)
		if cache["summary"] == nil || cache["display_names"] == nil {
			t.Fatalf("cache analytics = %#v, want summary and display_names", cache)
		}
		for _, key := range []string{"sparkline", "group_stats", "series"} {
			values, ok := cache[key].([]any)
			if !ok || len(values) != 0 {
				t.Fatalf("cache analytics %s = %#v, want empty array", key, cache[key])
			}
		}

		sessionOverviewResp := app.platformRequest(t, http.MethodGet, orgPath+"/analytics/sessions/overview?agent_id=agent_test123", nil, cookies)
		defer sessionOverviewResp.Body.Close()
		if sessionOverviewResp.StatusCode != http.StatusOK {
			t.Fatalf("session analytics overview status = %d, want 200: %s", sessionOverviewResp.StatusCode, readAll(t, sessionOverviewResp.Body))
		}
		var sessionOverview map[string]any
		decodeJSON(t, sessionOverviewResp.Body, &sessionOverview)
		if sessionsCount, ok := sessionOverview["sessions_count"].(map[string]any); !ok || sessionsCount["value"] != float64(0) {
			t.Fatalf("session analytics sessions_count = %#v, want zero value bucket", sessionOverview["sessions_count"])
		}
		if inputTokens, ok := sessionOverview["input_tokens"].(map[string]any); !ok || inputTokens["p95"] == nil {
			t.Fatalf("session analytics input_tokens = %#v, want quantile bucket", sessionOverview["input_tokens"])
		}

		sessionTimeseriesResp := app.platformRequest(t, http.MethodGet, orgPath+"/analytics/sessions/timeseries?agent_id=agent_test123&group_by=agent_version", nil, cookies)
		defer sessionTimeseriesResp.Body.Close()
		if sessionTimeseriesResp.StatusCode != http.StatusOK {
			t.Fatalf("session analytics timeseries status = %d, want 200: %s", sessionTimeseriesResp.StatusCode, readAll(t, sessionTimeseriesResp.Body))
		}
		var sessionTimeseries map[string]any
		decodeJSON(t, sessionTimeseriesResp.Body, &sessionTimeseries)
		if sessionTimeseries["group_by"] != "agent_version" {
			t.Fatalf("session analytics timeseries = %#v, want requested group_by", sessionTimeseries)
		}
		if dataPoints, ok := sessionTimeseries["data_points"].([]any); !ok || len(dataPoints) != 0 {
			t.Fatalf("session analytics data_points = %#v, want empty array", sessionTimeseries["data_points"])
		}
	})

	t.Run("success organization rate limits route", func(t *testing.T) {
		resp := app.platformRequest(t, http.MethodGet, orgPath+"/rate_limits", nil, cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("rate limits status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		if body["rate_limit_tier"] != "auto_api_evaluation" || body["tier_model_rate_limiters"] == nil {
			t.Fatalf("rate limits = %#v, want source-compatible rate limit body", body)
		}
	})

	t.Run("success mcp vault auth start route", func(t *testing.T) {
		oauthServer := newFakePlatformMCPOAuthServer(t, http.StatusOK)
		defer oauthServer.Close()
		vault := createVault(t, app, `{"display_name":"console oauth start vault"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		redirectURI := "https://platform.claude.com/oauth/vault/success?organization_id=" + orgUUID
		resp := app.platformRequest(t, http.MethodPost, orgPath+"/mcp/vault-auth/start", strings.NewReader(fmt.Sprintf(`{
			"mcp_server_url":"%s",
			"vault_id":"%s",
			"workspace_id":"default",
			"redirect_url":"%s",
			"display_name":" Console MCP ",
			"source":"vault_detail"
		}`, oauthServer.MCPURL(), vault.ID, redirectURI)), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mcp vault auth start status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		flowID := stringValue(body["oauth_flow_id"])
		redirectURL := stringValue(body["redirect_url"])
		if flowID == "" || redirectURL == "" {
			t.Fatalf("mcp vault auth start = %#v, want flow id and redirect url", body)
		}
		parsed, err := url.Parse(redirectURL)
		if err != nil {
			t.Fatalf("parse redirect url %q: %v", redirectURL, err)
		}
		issuerURL, err := url.Parse(oauthServer.URL)
		if err != nil {
			t.Fatalf("parse oauth server URL: %v", err)
		}
		if parsed.Scheme != issuerURL.Scheme || parsed.Host != issuerURL.Host || parsed.Path != "/authorize" {
			t.Fatalf("redirect url = %q, want MCP server authorize URL", redirectURL)
		}
		query := parsed.Query()
		if query.Get("state") != flowID || query.Get("redirect_uri") != redirectURI || query.Get("client_id") != "registered-client" || query.Get("resource") != oauthServer.MCPURL() {
			t.Fatalf("redirect query = %#v, want oauth params bound to request", query)
		}
		if query.Get("scope") != "files.read" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
			t.Fatalf("redirect query = %#v, want scope and PKCE params", query)
		}
		if oauthServer.Registrations() != 1 {
			t.Fatalf("registrations = %d, want 1", oauthServer.Registrations())
		}

		callbackResp := app.platformRequest(t, http.MethodGet, "/oauth/vault/success?state="+url.QueryEscape(flowID)+"&code=oauth-code", nil, cookies)
		defer callbackResp.Body.Close()
		if callbackResp.StatusCode != http.StatusOK {
			t.Fatalf("mcp vault auth callback status = %d, want 200: %s", callbackResp.StatusCode, readAll(t, callbackResp.Body))
		}
		callbackHTML := string(readAll(t, callbackResp.Body))
		if !strings.Contains(callbackHTML, `"type":"vault_oauth_complete"`) || !strings.Contains(callbackHTML, `"credential_id":"vcrd_`) || strings.Contains(callbackHTML, "access-token") {
			t.Fatalf("callback html = %q, want completion broadcast without token leak", callbackHTML)
		}
		if oauthServer.TokenExchanges() != 1 {
			t.Fatalf("token exchanges = %d, want 1", oauthServer.TokenExchanges())
		}
		credentialPage := listVaultCredentials(t, app, vault.ID, "")
		if len(credentialPage.Data) != 1 {
			t.Fatalf("credentials = %#v, want one OAuth credential", credentialPage.Data)
		}
		credential := credentialPage.Data[0]
		if credential.DisplayName != "Console MCP" {
			t.Fatalf("credential display name = %q, want trimmed request display name", credential.DisplayName)
		}
		assertRawContains(t, credential.Auth, `"type":"mcp_oauth"`)
		assertRawContains(t, credential.Auth, `"mcp_server_url":"`+oauthServer.MCPURL()+`"`)
		assertRawContains(t, credential.Auth, `"token_endpoint":"`+oauthServer.URL+`/token"`)
		assertRawContains(t, credential.Auth, `"client_id":"registered-client"`)
		assertRawNotContains(t, credential.Auth, "access-token")
		assertRawNotContains(t, credential.Auth, "refresh-token")

		var secretPayload string
		if err := app.db.Pool.QueryRow(context.Background(), `
			select secret_payload::text
			from vault_credentials
			where external_id = $1
		`, credential.ID).Scan(&secretPayload); err != nil {
			t.Fatalf("load credential secret payload: %v", err)
		}
		if !strings.Contains(secretPayload, "access-token") || !strings.Contains(secretPayload, "refresh-token") {
			t.Fatalf("secret payload = %q, want stored access and refresh tokens", secretPayload)
		}
	})

	t.Run("failure mcp vault auth callback token exchange", func(t *testing.T) {
		oauthServer := newFakePlatformMCPOAuthServer(t, http.StatusBadRequest)
		defer oauthServer.Close()
		vault := createVault(t, app, `{"display_name":"console oauth token failure vault"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		redirectURI := "https://platform.claude.com/oauth/vault/success?organization_id=" + orgUUID
		resp := app.platformRequest(t, http.MethodPost, orgPath+"/mcp/vault-auth/start", strings.NewReader(fmt.Sprintf(`{
			"mcp_server_url":"%s",
			"vault_id":"%s",
			"workspace_id":"default",
			"redirect_url":"%s"
		}`, oauthServer.MCPURL(), vault.ID, redirectURI)), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mcp vault auth start status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		flowID := stringValue(body["oauth_flow_id"])
		callbackResp := app.platformRequest(t, http.MethodGet, "/oauth/vault/success?state="+url.QueryEscape(flowID)+"&code=bad-code", nil, cookies)
		defer callbackResp.Body.Close()
		if callbackResp.StatusCode != http.StatusOK {
			t.Fatalf("mcp vault auth callback status = %d, want 200: %s", callbackResp.StatusCode, readAll(t, callbackResp.Body))
		}
		callbackHTML := string(readAll(t, callbackResp.Body))
		if !strings.Contains(callbackHTML, `"error_code":"token_exchange_failed"`) || strings.Contains(callbackHTML, `"credential_id"`) {
			t.Fatalf("callback html = %q, want token exchange failure broadcast", callbackHTML)
		}
		credentialPage := listVaultCredentials(t, app, vault.ID, "")
		if len(credentialPage.Data) != 0 {
			t.Fatalf("credentials = %#v, want no credential after token exchange failure", credentialPage.Data)
		}
	})

	t.Run("failure mcp vault auth start discovery", func(t *testing.T) {
		badServer := httptest.NewServer(http.NotFoundHandler())
		defer badServer.Close()
		vault := createVault(t, app, `{"display_name":"console oauth discovery failure vault"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		resp := app.platformRequest(t, http.MethodPost, orgPath+"/mcp/vault-auth/start", strings.NewReader(fmt.Sprintf(`{
			"mcp_server_url":"%s/mcp",
			"vault_id":"%s",
			"workspace_id":"default",
			"redirect_url":"https://platform.claude.com/oauth/vault/success?organization_id=%s"
		}`, badServer.URL, vault.ID, orgUUID)), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("mcp vault auth discovery status = %d, want 400: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		if body["error_code"] != "oauth_discovery_failed" || stringValue(body["oauth_flow_id"]) == "" {
			t.Fatalf("mcp vault auth discovery body = %#v, want discovery failure with flow id", body)
		}
	})

	t.Run("failure mcp vault auth start duplicate server", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"console oauth duplicate vault"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		serverURL := "https://mcp.console-duplicate.example/mcp"
		createVaultCredential(t, app, vault.ID, staticBearerBody("duplicate mcp", serverURL, "bearer-secret"))
		resp := app.platformRequest(t, http.MethodPost, orgPath+"/mcp/vault-auth/start", strings.NewReader(fmt.Sprintf(`{
			"mcp_server_url":"%s",
			"vault_id":"%s",
			"workspace_id":"workspace_default",
			"redirect_url":"https://platform.claude.com/oauth/vault/success?organization_id=%s"
		}`, serverURL, vault.ID, orgUUID)), cookies)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("duplicate mcp vault auth status = %d, want 409: %s", resp.StatusCode, readAll(t, resp.Body))
		}
		var body map[string]any
		decodeJSON(t, resp.Body, &body)
		if body["error_code"] != "already_exists" || body["oauth_flow_id"] != nil {
			t.Fatalf("duplicate mcp vault auth body = %#v, want already_exists without flow id", body)
		}
	})

	t.Run("failure organization oauth environment tokens route requires session", func(t *testing.T) {
		path := "/api/oauth/organizations/" + orgUUID + "/environments/env_test_tokens/tokens"
		resp := app.platformRequest(t, http.MethodGet, path, nil, nil)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("tokens status = %d, want 401: %s", resp.StatusCode, readAll(t, resp.Body))
		}
	})

	t.Run("success organization oauth environment token routes", func(t *testing.T) {
		path := "/api/oauth/organizations/" + orgUUID + "/environments/env_test_tokens/tokens"
		listResp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer listResp.Body.Close()
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("list tokens status = %d, want 200: %s", listResp.StatusCode, readAll(t, listResp.Body))
		}
		var emptyList map[string]any
		decodeJSON(t, listResp.Body, &emptyList)
		if data, ok := emptyList["data"].([]any); !ok || len(data) != 0 {
			t.Fatalf("empty token list data = %#v, want empty array", emptyList["data"])
		}
		pagination, ok := emptyList["pagination"].(map[string]any)
		if !ok || pagination["limit"] != float64(100) || pagination["has_more"] != false {
			t.Fatalf("empty token list pagination = %#v, want source-compatible pagination", emptyList["pagination"])
		}

		createResp := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"name":" Quickstart key "}`), cookies)
		defer createResp.Body.Close()
		if createResp.StatusCode != http.StatusOK {
			t.Fatalf("create token status = %d, want 200: %s", createResp.StatusCode, readAll(t, createResp.Body))
		}
		var created map[string]any
		decodeJSON(t, createResp.Body, &created)
		if token := stringValue(created["access_token"]); !strings.HasPrefix(token, "sk-ant-oat01-") {
			t.Fatalf("created access token = %#v, want oat token", created["access_token"])
		}
		if created["expires_in"] != float64(31536000) {
			t.Fatalf("created expires_in = %#v, want one year", created["expires_in"])
		}
		details, ok := created["authorization_details"].([]any)
		if !ok || len(details) != 1 {
			t.Fatalf("authorization_details = %#v, want one detail", created["authorization_details"])
		}
		detail, ok := details[0].(map[string]any)
		if !ok || detail["type"] != "ccr_env" || detail["environment_id"] != "env_test_tokens" {
			t.Fatalf("authorization detail = %#v, want ccr_env for environment", details[0])
		}

		afterResp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer afterResp.Body.Close()
		if afterResp.StatusCode != http.StatusOK {
			t.Fatalf("list after create status = %d, want 200: %s", afterResp.StatusCode, readAll(t, afterResp.Body))
		}
		var afterList map[string]any
		decodeJSON(t, afterResp.Body, &afterList)
		data, ok := afterList["data"].([]any)
		if !ok || len(data) != 1 {
			t.Fatalf("token list data = %#v, want one token", afterList["data"])
		}
		token, ok := data[0].(map[string]any)
		if !ok || token["id"] == "" || token["name"] != "Quickstart key" || token["created_at"] == "" || token["expires_at"] == "" {
			t.Fatalf("token = %#v, want source-compatible token metadata", data[0])
		}
	})

	t.Run("success console workspace api key lifecycle", func(t *testing.T) {
		path := consoleOrgPath + "/workspaces/default/api_keys"
		createResp := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"name":" HAR key "}`), cookies)
		defer createResp.Body.Close()
		if createResp.StatusCode != http.StatusOK {
			t.Fatalf("create api key status = %d, want 200: %s", createResp.StatusCode, readAll(t, createResp.Body))
		}
		var created map[string]any
		decodeJSON(t, createResp.Body, &created)
		keyID := stringValue(created["id"])
		rawKey := stringValue(created["raw_key"])
		if !strings.HasPrefix(keyID, "apikey_") {
			t.Fatalf("created api key id = %q, want apikey_ prefix", keyID)
		}
		if !strings.HasPrefix(rawKey, "sk-ant-api03-") {
			t.Fatalf("created raw_key = %q, want sk-ant-api03- prefix", rawKey)
		}
		if created["type"] != "api_key" || created["status"] != "active" || created["workspace_id"] != nil {
			t.Fatalf("created api key = %#v, want HAR-compatible active default-workspace key", created)
		}
		createdBy, ok := created["created_by"].(map[string]any)
		if !ok || createdBy["id"] == "" || createdBy["type"] != "user" {
			t.Fatalf("created_by = %#v, want user object", created["created_by"])
		}

		modelsResp := app.do(t, http.MethodGet, "/v1/models?limit=1", nil, rawKey, false, "")
		defer modelsResp.Body.Close()
		if modelsResp.StatusCode != http.StatusOK {
			t.Fatalf("models with created key status = %d, want 200: %s", modelsResp.StatusCode, readAll(t, modelsResp.Body))
		}

		listResp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer listResp.Body.Close()
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("list api keys status = %d, want 200: %s", listResp.StatusCode, readAll(t, listResp.Body))
		}
		var listed []map[string]any
		decodeJSON(t, listResp.Body, &listed)
		if !containsConsoleAPIKeyStatus(listed, keyID, "active") {
			t.Fatalf("listed api keys = %#v, want active %s", listed, keyID)
		}

		inactiveResp := app.platformRequest(t, http.MethodPost, path+"/"+keyID, strings.NewReader(`{"status":"inactive"}`), cookies)
		defer inactiveResp.Body.Close()
		if inactiveResp.StatusCode != http.StatusOK {
			t.Fatalf("inactive api key status = %d, want 200: %s", inactiveResp.StatusCode, readAll(t, inactiveResp.Body))
		}
		var inactive map[string]any
		decodeJSON(t, inactiveResp.Body, &inactive)
		if inactive["status"] != "inactive" {
			t.Fatalf("inactive api key = %#v, want inactive", inactive)
		}
		assertError(t, app.do(t, http.MethodGet, "/v1/models?limit=1", nil, rawKey, false, ""), http.StatusUnauthorized, "authentication_error")

		activeResp := app.platformRequest(t, http.MethodPost, path+"/"+keyID, strings.NewReader(`{"status":"active"}`), cookies)
		defer activeResp.Body.Close()
		if activeResp.StatusCode != http.StatusOK {
			t.Fatalf("active api key status = %d, want 200: %s", activeResp.StatusCode, readAll(t, activeResp.Body))
		}
		var active map[string]any
		decodeJSON(t, activeResp.Body, &active)
		if active["status"] != "active" || active["archived_at"] != nil {
			t.Fatalf("active api key = %#v, want active and unarchived", active)
		}
		reactivatedModelsResp := app.do(t, http.MethodGet, "/v1/models?limit=1", nil, rawKey, false, "")
		defer reactivatedModelsResp.Body.Close()
		if reactivatedModelsResp.StatusCode != http.StatusOK {
			t.Fatalf("models with reactivated key status = %d, want 200: %s", reactivatedModelsResp.StatusCode, readAll(t, reactivatedModelsResp.Body))
		}

		archivedResp := app.platformRequest(t, http.MethodPost, path+"/"+keyID, strings.NewReader(`{"status":"archived"}`), cookies)
		defer archivedResp.Body.Close()
		if archivedResp.StatusCode != http.StatusOK {
			t.Fatalf("archive api key status = %d, want 200: %s", archivedResp.StatusCode, readAll(t, archivedResp.Body))
		}
		var archived map[string]any
		decodeJSON(t, archivedResp.Body, &archived)
		if archived["status"] != "archived" || archived["archived_at"] == nil {
			t.Fatalf("archived api key = %#v, want archived timestamp", archived)
		}
		assertError(t, app.do(t, http.MethodGet, "/v1/models?limit=1", nil, rawKey, false, ""), http.StatusUnauthorized, "authentication_error")

		afterArchiveResp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer afterArchiveResp.Body.Close()
		if afterArchiveResp.StatusCode != http.StatusOK {
			t.Fatalf("list after archive status = %d, want 200: %s", afterArchiveResp.StatusCode, readAll(t, afterArchiveResp.Body))
		}
		var afterArchive []map[string]any
		decodeJSON(t, afterArchiveResp.Body, &afterArchive)
		if !containsConsoleAPIKeyStatus(afterArchive, keyID, "archived") {
			t.Fatalf("listed api keys after archive = %#v, want archived %s", afterArchive, keyID)
		}
	})

	t.Run("success console workspace create route", func(t *testing.T) {
		path := consoleOrgPath + "/workspaces"
		workspaceName := fmt.Sprintf("Docs %d", time.Now().UnixNano())
		createResp := app.platformRequest(t, http.MethodPost, path, strings.NewReader(`{"name":"`+workspaceName+`","display_color":"#1A8961","data_residency":{"workspace_geo":"us"}}`), cookies)
		defer createResp.Body.Close()
		if createResp.StatusCode != http.StatusOK {
			t.Fatalf("create workspace status = %d, want 200: %s", createResp.StatusCode, readAll(t, createResp.Body))
		}
		var created map[string]any
		decodeJSON(t, createResp.Body, &created)
		workspaceID := stringValue(created["id"])
		if !strings.HasPrefix(workspaceID, "wrkspc_") {
			t.Fatalf("created workspace id = %q, want wrkspc_ prefix", workspaceID)
		}
		if created["type"] != "workspace" || created["name"] != workspaceName || created["display_color"] != "#1A8961" || created["color"] != "#1A8961" {
			t.Fatalf("created workspace = %#v, want source-compatible workspace shape", created)
		}
		dataResidency, ok := created["data_residency"].(map[string]any)
		if !ok || dataResidency["workspace_geo"] != "us" || dataResidency["allowed_inference_geos"] != "unrestricted" || dataResidency["default_inference_geo"] != "global" {
			t.Fatalf("created data_residency = %#v, want normalized us residency", created["data_residency"])
		}

		listResp := app.platformRequest(t, http.MethodGet, path, nil, cookies)
		defer listResp.Body.Close()
		if listResp.StatusCode != http.StatusOK {
			t.Fatalf("list workspaces status = %d, want 200: %s", listResp.StatusCode, readAll(t, listResp.Body))
		}
		var listed []map[string]any
		decodeJSON(t, listResp.Body, &listed)
		if !containsConsoleWorkspace(listed, workspaceID, workspaceName) {
			t.Fatalf("listed workspaces = %#v, want created workspace %s", listed, workspaceID)
		}
	})

	t.Run("success console organization routes", func(t *testing.T) {
		workspacesResp := app.platformRequest(t, http.MethodGet, consoleOrgPath+"/workspaces", nil, cookies)
		defer workspacesResp.Body.Close()
		if workspacesResp.StatusCode != http.StatusOK {
			t.Fatalf("workspaces status = %d, want 200: %s", workspacesResp.StatusCode, readAll(t, workspacesResp.Body))
		}
		var workspaces []map[string]any
		decodeJSON(t, workspacesResp.Body, &workspaces)
		for _, workspace := range workspaces {
			if workspace["id"] == "workspace_console_backend_other_default" {
				t.Fatalf("workspaces = %#v, want only current organization workspaces", workspaces)
			}
			if strings.EqualFold(stringValue(workspace["name"]), "default") {
				t.Fatalf("workspaces = %#v, want built-in default workspace hidden from console list", workspaces)
			}
			if workspace["type"] != "workspace" || workspace["id"] == "" {
				t.Fatalf("workspace = %#v, want source-compatible workspace shape", workspace)
			}
		}

		otherOrgWorkspacesResp := app.platformRequest(t, http.MethodGet, "/api/console/organizations/"+otherOrgUUID+"/workspaces", nil, cookies)
		defer otherOrgWorkspacesResp.Body.Close()
		if otherOrgWorkspacesResp.StatusCode != http.StatusNotFound {
			t.Fatalf("other org workspaces status = %d, want 404: %s", otherOrgWorkspacesResp.StatusCode, readAll(t, otherOrgWorkspacesResp.Body))
		}

		adminRequestsResp := app.platformRequest(t, http.MethodGet, consoleOrgPath+"/admin_requests/join_org", nil, cookies)
		defer adminRequestsResp.Body.Close()
		if adminRequestsResp.StatusCode != http.StatusOK {
			t.Fatalf("admin requests status = %d, want 200: %s", adminRequestsResp.StatusCode, readAll(t, adminRequestsResp.Body))
		}
		var adminRequests []map[string]any
		decodeJSON(t, adminRequestsResp.Body, &adminRequests)
		if len(adminRequests) != 0 {
			t.Fatalf("admin requests = %#v, want empty array", adminRequests)
		}

		apiKeysResp := app.platformRequest(t, http.MethodGet, consoleOrgPath+"/api_keys", nil, cookies)
		defer apiKeysResp.Body.Close()
		if apiKeysResp.StatusCode != http.StatusOK {
			t.Fatalf("api keys status = %d, want 200: %s", apiKeysResp.StatusCode, readAll(t, apiKeysResp.Body))
		}
		var apiKeys []map[string]any
		decodeJSON(t, apiKeysResp.Body, &apiKeys)
		for _, apiKey := range apiKeys {
			if apiKey["id"] == "" || apiKey["type"] != "api_key" {
				t.Fatalf("api key = %#v, want source-compatible api key shape", apiKey)
			}
		}
	})
}

func containsConsoleAPIKeyStatus(keys []map[string]any, keyID string, status string) bool {
	for _, key := range keys {
		if key["id"] == keyID && key["status"] == status {
			return true
		}
	}
	return false
}

type fakePlatformMCPOAuthServer struct {
	*httptest.Server
	tokenStatus    int
	registrations  atomic.Int32
	tokenExchanges atomic.Int32
}

func newFakePlatformMCPOAuthServer(t *testing.T, tokenStatus int) *fakePlatformMCPOAuthServer {
	t.Helper()
	fake := &fakePlatformMCPOAuthServer{tokenStatus: tokenStatus}
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+fake.URL+`/.well-known/oauth-protected-resource/mcp", scope="files.read"`)
		http.Error(w, "authorization required", http.StatusUnauthorized)
	})
	mux.HandleFunc("/.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{
			"resource":              fake.MCPURL(),
			"authorization_servers": []string{fake.URL},
			"scopes_supported":      []string{"files.read"},
		})
	})
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(w, map[string]any{
			"issuer":                                fake.URL,
			"authorization_endpoint":                fake.URL + "/authorize",
			"token_endpoint":                        fake.URL + "/token",
			"registration_endpoint":                 fake.URL + "/register",
			"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic"},
			"code_challenge_methods_supported":      []string{"S256"},
			"scopes_supported":                      []string{"files.read"},
		})
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		redirectURIs, _ := payload["redirect_uris"].([]any)
		if len(redirectURIs) != 1 || stringValue(redirectURIs[0]) == "" {
			http.Error(w, "missing redirect uri", http.StatusBadRequest)
			return
		}
		fake.registrations.Add(1)
		writeTestJSON(w, map[string]any{
			"client_id":                  "registered-client",
			"token_endpoint_auth_method": "none",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fake.tokenExchanges.Add(1)
		if fake.tokenStatus != http.StatusOK {
			w.WriteHeader(fake.tokenStatus)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "authorization_code" ||
			r.Form.Get("code") == "" ||
			r.Form.Get("client_id") != "registered-client" ||
			r.Form.Get("code_verifier") == "" ||
			r.Form.Get("resource") != fake.MCPURL() {
			http.Error(w, "invalid token request", http.StatusBadRequest)
			return
		}
		writeTestJSON(w, map[string]any{
			"access_token":  "access-token",
			"refresh_token": "refresh-token",
			"expires_in":    3600,
			"scope":         "files.read",
			"token_type":    "Bearer",
		})
	})
	fake.Server = httptest.NewServer(mux)
	return fake
}

func (f *fakePlatformMCPOAuthServer) MCPURL() string {
	return f.URL + "/mcp"
}

func (f *fakePlatformMCPOAuthServer) Registrations() int {
	return int(f.registrations.Load())
}

func (f *fakePlatformMCPOAuthServer) TokenExchanges() int {
	return int(f.tokenExchanges.Load())
}

func writeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func containsConsoleWorkspace(workspaces []map[string]any, workspaceID string, name string) bool {
	for _, workspace := range workspaces {
		if workspace["id"] == workspaceID && workspace["name"] == name {
			return true
		}
	}
	return false
}

func loadDefaultOrganizationUUID(t *testing.T, app *testApp) string {
	t.Helper()
	var orgUUID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select o.uuid::text
		from organizations o
		where o.external_id = 'org_default'
	`).Scan(&orgUUID); err != nil {
		t.Fatalf("load default organization uuid: %v", err)
	}
	return orgUUID
}

func seedConsoleDefaultWorkspace(t *testing.T, app *testApp, orgExternalID string, workspaceExternalID string) string {
	t.Helper()
	var organizationID int64
	var orgUUID string
	if err := app.db.Pool.QueryRow(context.Background(), `
		insert into organizations (external_id, name)
		values ($1, $1)
		on conflict (external_id) do update set name = excluded.name
		returning id, uuid::text
	`, orgExternalID).Scan(&organizationID, &orgUUID); err != nil {
		t.Fatalf("seed console org: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		insert into workspaces (external_id, organization_id, name)
		values ($1, $2, 'default')
		on conflict (external_id) do update set
			organization_id = excluded.organization_id,
			name = excluded.name,
			archived_at = null,
			updated_at = now()
	`, workspaceExternalID, organizationID); err != nil {
		t.Fatalf("seed console workspace: %v", err)
	}
	return orgUUID
}
