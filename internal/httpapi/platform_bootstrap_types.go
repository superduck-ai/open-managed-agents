package httpapi

type Account struct {
	TaggedID                  string         `json:"tagged_id"`
	UUID                      string         `json:"uuid"`
	EmailAddress              string         `json:"email_address"`
	FullName                  *string        `json:"full_name"`
	DisplayName               *string        `json:"display_name"`
	IsVerified                bool           `json:"is_verified"`
	AgeIsVerified             bool           `json:"age_is_verified"`
	IsAnonymous               bool           `json:"is_anonymous"`
	CreatedAt                 string         `json:"created_at"`
	UpdatedAt                 string         `json:"updated_at"`
	Settings                  map[string]any `json:"settings"`
	Memberships               []Membership   `json:"memberships"`
	WorkspaceMemberships      []any          `json:"workspace_memberships"`
	Invites                   []any          `json:"invites"`
	CompletedVerificationAt   string         `json:"completed_verification_at"`
	AcceptedClickwrapVersions map[string]any `json:"accepted_clickwrap_versions"`
	VerifiedPhoneNumberLast4  string         `json:"verified_phone_number_last4"`
}

type Membership struct {
	Organization            map[string]any `json:"organization"`
	Role                    string         `json:"role"`
	SeatTier                string         `json:"seat_tier"`
	CreatedAt               string         `json:"created_at"`
	UpdatedAt               string         `json:"updated_at"`
	NotificationPreferences map[string]any `json:"notification_preferences"`
}

type BootstrapCompatibilityResponse struct {
	Account                  *Account               `json:"account"`
	Statsig                  *BootstrapStatsig      `json:"statsig,omitempty"`
	Growthbook               *BootstrapGrowthbook   `json:"growthbook,omitempty"`
	OrgStatsig               BootstrapStatsig       `json:"org_statsig"`
	OrgGrowthbook            BootstrapGrowthbook    `json:"org_growthbook"`
	CurrentUserAccess        CurrentUserAccess      `json:"current_user_access"`
	IntercomAccountHash      any                    `json:"intercom_account_hash"`
	Locale                   any                    `json:"locale"`
	SystemPrompts            map[string]any         `json:"system_prompts"`
	GatedMessages            BootstrapGatedMessages `json:"gated_messages"`
	GatedImports             map[string]any         `json:"gated_imports"`
	ServerLocalizations      map[string]string      `json:"server_localizations,omitempty"`
	ClaudeAITranslationsPath string                 `json:"claude_ai_translations_path,omitempty"`
	Metadata                 map[string]any         `json:"metadata,omitempty"`
}

type BootstrapStatsig struct {
	User       map[string]any `json:"user"`
	Values     map[string]any `json:"values"`
	ValuesHash string         `json:"values_hash"`
}

type BootstrapGrowthbook struct {
	Features         map[string]any `json:"features"`
	HashingAlgorithm string         `json:"hashing_algorithm"`
	User             map[string]any `json:"user"`
}

type BootstrapGatedMessages struct {
	Messages map[string]any `json:"messages"`
	Gates    []any          `json:"gates"`
	Locale   string         `json:"locale"`
}

type CurrentUserAccess struct {
	Permissions        []string                   `json:"permissions"`
	Role               string                     `json:"role"`
	Features           []CurrentUserFeature       `json:"features"`
	AccountPermissions []CurrentAccountPermission `json:"account_permissions"`
}

type CurrentUserFeature struct {
	Feature string `json:"feature"`
	Status  string `json:"status"`
}

type CurrentAccountPermission struct {
	Permission string `json:"permission"`
	Status     string `json:"status"`
}
