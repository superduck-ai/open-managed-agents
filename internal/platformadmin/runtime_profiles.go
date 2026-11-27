package platformadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/agentruntime"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
)

type RuntimeProfileCreateInput struct {
	Name                string          `json:"name"`
	ProviderKind        string          `json:"provider_kind"`
	EndpointURL         string          `json:"endpoint_url"`
	IsolationMode       string          `json:"isolation_mode"`
	Config              json.RawMessage `json:"config"`
	CapabilityOverrides json.RawMessage `json:"capability_overrides"`
	Credentials         json.RawMessage `json:"credentials"`
	Status              string          `json:"status"`
}

type RuntimeProfileUpdateInput struct {
	ExpectedVersion     int64            `json:"expected_version"`
	Name                *string          `json:"name"`
	EndpointURL         *string          `json:"endpoint_url"`
	IsolationMode       *string          `json:"isolation_mode"`
	Config              *json.RawMessage `json:"config"`
	CapabilityOverrides *json.RawMessage `json:"capability_overrides"`
	Credentials         *json.RawMessage `json:"credentials"`
	Status              *string          `json:"status"`
}

type RuntimeProfileView struct {
	UUID                  string                     `json:"uuid"`
	ID                    string                     `json:"id"`
	Scope                 string                     `json:"scope"`
	Name                  string                     `json:"name"`
	ProviderKind          string                     `json:"provider_kind"`
	EndpointURL           string                     `json:"endpoint_url"`
	IsolationMode         string                     `json:"isolation_mode"`
	Config                json.RawMessage            `json:"config"`
	CapabilityOverrides   json.RawMessage            `json:"capability_overrides"`
	EffectiveCapabilities agentruntime.CapabilitySet `json:"effective_capabilities"`
	HasCredentials        bool                       `json:"has_credentials"`
	CredentialHint        string                     `json:"credential_hint"`
	CredentialRevision    int64                      `json:"credential_revision"`
	Builtin               bool                       `json:"builtin"`
	Status                string                     `json:"status"`
	LastHealthStatus      string                     `json:"last_health_status"`
	LastHealthCheckedAt   *time.Time                 `json:"last_health_checked_at"`
	LastHealthError       string                     `json:"last_health_error"`
	Version               int64                      `json:"version"`
	CreatedAt             time.Time                  `json:"created_at"`
	UpdatedAt             time.Time                  `json:"updated_at"`
}

type RuntimeProfileTestResult struct {
	Status    string    `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
}

type OrganizationRuntimePolicyView struct {
	OrganizationUUID           string               `json:"organization_uuid"`
	DefaultRuntimeProfileUUID  string               `json:"default_runtime_profile_uuid"`
	AllowedRuntimeProfileUUIDs []string             `json:"allowed_runtime_profile_uuids"`
	Profiles                   []RuntimeProfileView `json:"profiles"`
}

type UpdateOrganizationRuntimePolicyInput struct {
	DefaultRuntimeProfileUUID  string   `json:"default_runtime_profile_uuid"`
	AllowedRuntimeProfileUUIDs []string `json:"allowed_runtime_profile_uuids"`
}

type runtimeProfileStore interface {
	ListPlatformRuntimeProfiles(context.Context) ([]db.RuntimeProfile, error)
	GetRuntimeProfileByExternalID(context.Context, string) (db.RuntimeProfile, error)
	GetRuntimeProfileByUUID(context.Context, string) (db.RuntimeProfile, error)
	CreateRuntimeProfile(context.Context, db.RuntimeProfile) (db.RuntimeProfile, error)
	UpdateRuntimeProfile(context.Context, db.RuntimeProfile) (db.RuntimeProfile, error)
	DeleteRuntimeProfile(context.Context, string, int64) error
	GetOrganizationRuntimePolicy(context.Context, string) (db.OrganizationRuntimePolicy, error)
	UpdateOrganizationRuntimePolicy(context.Context, string, string, []string) (db.OrganizationRuntimePolicy, error)
}

type runtimeProfileSealer interface {
	SealRuntimeProfile(context.Context, secrets.RuntimeProfileBinding, []byte) (secrets.Envelope, error)
}

type runtimeProfileHealthChecker interface {
	HealthCheckProfile(context.Context, string) (agentruntime.HealthCheckResult, error)
}

type RuntimeProfileService struct {
	store         runtimeProfileStore
	registry      *agentruntime.Registry
	secretSvc     runtimeProfileSealer
	healthChecker runtimeProfileHealthChecker
	logger        *slog.Logger
	environment   string
}

func NewRuntimeProfileService(
	store runtimeProfileStore,
	registry *agentruntime.Registry,
	secretSvc runtimeProfileSealer,
	healthChecker runtimeProfileHealthChecker,
	logger *slog.Logger,
) *RuntimeProfileService {
	return &RuntimeProfileService{
		store:         store,
		registry:      registry,
		secretSvc:     secretSvc,
		healthChecker: healthChecker,
		logger:        logging.LoggerOrDefault(logger),
	}
}

func (s *RuntimeProfileService) WithEnvironment(environment string) *RuntimeProfileService {
	if s == nil {
		return nil
	}
	scoped := *s
	scoped.environment = environment
	return &scoped
}

func (s *RuntimeProfileService) List(ctx context.Context) ([]RuntimeProfileView, error) {
	profiles, err := s.store.ListPlatformRuntimeProfiles(ctx)
	if err != nil {
		return nil, ErrRuntimeProfileUnavailable
	}
	return runtimeProfileViews(profiles)
}

func (s *RuntimeProfileService) Create(ctx context.Context, actor Actor, input RuntimeProfileCreateInput) (view RuntimeProfileView, failure error) {
	auditProfile := db.RuntimeProfile{}
	defer func() {
		if failure != nil {
			s.audit(ctx, actor, auditProfile, "create", nil, failure)
		}
	}()
	name := strings.TrimSpace(input.Name)
	providerKind := agentruntime.ProviderKind(strings.TrimSpace(input.ProviderKind))
	endpointURL := strings.TrimSpace(input.EndpointURL)
	if err := s.validateRuntimeProfileCreate(name, providerKind, endpointURL); err != nil {
		return RuntimeProfileView{}, err
	}
	isolationMode, err := normalizeRuntimeIsolationMode(input.IsolationMode, "shared_upstream")
	if err != nil {
		return RuntimeProfileView{}, err
	}
	status, err := normalizeRuntimeProfileStatus(input.Status, "active")
	if err != nil {
		return RuntimeProfileView{}, err
	}
	configJSON, err := normalizeJSONObject(input.Config, "config")
	if err != nil {
		return RuntimeProfileView{}, err
	}
	overridesJSON, err := normalizeCapabilityOverridesJSON(providerKind, input.CapabilityOverrides)
	if err != nil {
		return RuntimeProfileView{}, err
	}
	externalID, err := ids.New("rprof_")
	if err != nil {
		return RuntimeProfileView{}, err
	}
	now := time.Now().UTC()
	profile := db.RuntimeProfile{
		UUID:                uuid.NewV4().String(),
		ExternalID:          externalID,
		Scope:               "platform",
		Name:                name,
		ProviderKind:        providerKind.String(),
		EndpointURL:         endpointURL,
		IsolationMode:       isolationMode,
		Config:              configJSON,
		CapabilityOverrides: overridesJSON,
		CredentialRevision:  1,
		Status:              status,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	auditProfile = profile
	if hasCredentialPayload(input.Credentials) {
		if err := s.sealCredentials(ctx, &profile, input.Credentials); err != nil {
			return RuntimeProfileView{}, err
		}
	}
	created, err := s.store.CreateRuntimeProfile(ctx, profile)
	if err != nil {
		return RuntimeProfileView{}, mapRuntimeProfileError(err)
	}
	s.audit(ctx, actor, created, "create", []string{"name", "provider_kind", "endpoint_url", "isolation_mode", "config", "capability_overrides", "status"}, nil)
	return runtimeProfileView(created)
}

func (s *RuntimeProfileService) Update(ctx context.Context, actor Actor, externalID string, input RuntimeProfileUpdateInput) (view RuntimeProfileView, failure error) {
	current, err := s.store.GetRuntimeProfileByExternalID(ctx, externalID)
	defer func() {
		if failure != nil {
			s.audit(ctx, actor, current, "update", input.changedFields(), failure)
		}
	}()
	if err != nil {
		return RuntimeProfileView{}, mapRuntimeProfileError(err)
	}
	if current.Scope != "platform" {
		return RuntimeProfileView{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return RuntimeProfileView{}, invalid("必须提供 expected_version")
	}
	if input.ExpectedVersion != current.Version {
		return RuntimeProfileView{}, ErrRuntimeProfileVersion
	}
	if err := s.applyRuntimeProfilePatch(ctx, &current, input); err != nil {
		return RuntimeProfileView{}, err
	}
	current.UpdatedAt = time.Now().UTC()
	updated, err := s.store.UpdateRuntimeProfile(ctx, current)
	if err != nil {
		return RuntimeProfileView{}, mapRuntimeProfileError(err)
	}
	action := "update"
	if input.Credentials != nil {
		action = "rotate_credentials"
	}
	s.audit(ctx, actor, updated, action, input.changedFields(), nil)
	return runtimeProfileView(updated)
}

func (s *RuntimeProfileService) Delete(ctx context.Context, actor Actor, externalID string, expectedVersion int64) (failure error) {
	current, err := s.store.GetRuntimeProfileByExternalID(ctx, externalID)
	defer func() {
		if failure != nil {
			s.audit(ctx, actor, current, "delete", nil, failure)
		}
	}()
	if err != nil {
		return mapRuntimeProfileError(err)
	}
	if current.Scope != "platform" {
		return ErrNotFound
	}
	if current.UUID == db.DefaultOMANativeRuntimeProfileUUID {
		return ErrRuntimeProfileProtected
	}
	if expectedVersion < 1 {
		return invalid("必须提供 expected_version")
	}
	if err := s.store.DeleteRuntimeProfile(ctx, current.UUID, expectedVersion); err != nil {
		return mapRuntimeProfileError(err)
	}
	s.audit(ctx, actor, current, "delete", nil, nil)
	return nil
}

func (s *RuntimeProfileService) TestConnection(ctx context.Context, actor Actor, externalID string) (RuntimeProfileTestResult, error) {
	current, err := s.store.GetRuntimeProfileByExternalID(ctx, externalID)
	if err != nil {
		return RuntimeProfileTestResult{}, mapRuntimeProfileError(err)
	}
	if current.Scope != "platform" {
		return RuntimeProfileTestResult{}, ErrNotFound
	}
	if s.healthChecker == nil {
		return RuntimeProfileTestResult{}, ErrRuntimeProfileUnavailable
	}
	res, checkErr := s.healthChecker.HealthCheckProfile(ctx, current.UUID)
	s.audit(ctx, actor, current, "test_connection", nil, checkErr)
	status := res.Status
	if status == "" {
		if checkErr != nil {
			status = "unhealthy"
		} else {
			status = "healthy"
		}
	}
	message := res.Message
	if message == "" && checkErr != nil {
		message = checkErr.Error()
	}
	checkedAt := res.CheckedAt
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	return RuntimeProfileTestResult{
		Status:    status,
		LatencyMs: res.Latency.Milliseconds(),
		Message:   message,
		CheckedAt: checkedAt,
	}, nil
}

func (s *RuntimeProfileService) GetOrganizationPolicy(ctx context.Context, organizationUUID string) (OrganizationRuntimePolicyView, error) {
	policy, err := s.store.GetOrganizationRuntimePolicy(ctx, organizationUUID)
	if err != nil {
		return OrganizationRuntimePolicyView{}, mapRuntimeProfileError(err)
	}
	profiles, err := s.store.ListPlatformRuntimeProfiles(ctx)
	if err != nil {
		return OrganizationRuntimePolicyView{}, ErrRuntimeProfileUnavailable
	}
	views, err := runtimeProfileViews(profiles)
	if err != nil {
		return OrganizationRuntimePolicyView{}, err
	}
	return OrganizationRuntimePolicyView{
		OrganizationUUID:           policy.OrganizationUUID,
		DefaultRuntimeProfileUUID:  policy.DefaultRuntimeProfileUUID,
		AllowedRuntimeProfileUUIDs: policy.AllowedRuntimeProfileUUIDs,
		Profiles:                   views,
	}, nil
}

func (s *RuntimeProfileService) UpdateOrganizationPolicy(
	ctx context.Context,
	actor Actor,
	organizationUUID string,
	input UpdateOrganizationRuntimePolicyInput,
) (OrganizationRuntimePolicyView, error) {
	defaultUUID := strings.TrimSpace(input.DefaultRuntimeProfileUUID)
	if _, err := uuid.Parse(defaultUUID); err != nil {
		return OrganizationRuntimePolicyView{}, invalid("default_runtime_profile_uuid 无效")
	}
	allowedUUIDs, err := normalizePolicyProfileUUIDs(input.AllowedRuntimeProfileUUIDs)
	if err != nil {
		return OrganizationRuntimePolicyView{}, err
	}
	if !slices.Contains(allowedUUIDs, db.DefaultOMANativeRuntimeProfileUUID) {
		return OrganizationRuntimePolicyView{}, invalid("allowed_runtime_profile_uuids 必须包含内置 oma_native 运行时档案")
	}
	if !slices.Contains(allowedUUIDs, defaultUUID) {
		return OrganizationRuntimePolicyView{}, invalid("default_runtime_profile_uuid 必须位于 allowed_runtime_profile_uuids 中")
	}
	profiles, err := s.store.ListPlatformRuntimeProfiles(ctx)
	if err != nil {
		return OrganizationRuntimePolicyView{}, ErrRuntimeProfileUnavailable
	}
	byUUID := make(map[string]db.RuntimeProfile, len(profiles))
	for _, profile := range profiles {
		byUUID[profile.UUID] = profile
	}
	for _, profileUUID := range allowedUUIDs {
		profile, exists := byUUID[profileUUID]
		if !exists {
			return OrganizationRuntimePolicyView{}, invalid("allowed_runtime_profile_uuids 包含不存在的平台运行时档案")
		}
		if profile.Status == "archived" {
			return OrganizationRuntimePolicyView{}, invalid("allowed_runtime_profile_uuids 不能包含已归档的运行时档案")
		}
	}
	if defaultProfile := byUUID[defaultUUID]; defaultProfile.Status != "active" {
		return OrganizationRuntimePolicyView{}, invalid("默认运行时档案必须处于 active 状态")
	}
	updated, err := s.store.UpdateOrganizationRuntimePolicy(ctx, organizationUUID, defaultUUID, allowedUUIDs)
	if err != nil {
		return OrganizationRuntimePolicyView{}, mapRuntimeProfileError(err)
	}
	views, err := runtimeProfileViews(profiles)
	if err != nil {
		return OrganizationRuntimePolicyView{}, err
	}
	s.logger.InfoContext(ctx, "platform organization runtime policy updated",
		"actor_account_uuid", actor.AccountUUID,
		"organization_uuid", organizationUUID,
		"default_runtime_profile_uuid", updated.DefaultRuntimeProfileUUID,
		"allowed_count", len(updated.AllowedRuntimeProfileUUIDs),
		"request_id", httpapi.RequestID(ctx),
	)
	return OrganizationRuntimePolicyView{
		OrganizationUUID:           updated.OrganizationUUID,
		DefaultRuntimeProfileUUID:  updated.DefaultRuntimeProfileUUID,
		AllowedRuntimeProfileUUIDs: updated.AllowedRuntimeProfileUUIDs,
		Profiles:                   views,
	}, nil
}

func (s *RuntimeProfileService) validateRuntimeProfileCreate(name string, kind agentruntime.ProviderKind, endpointURL string) error {
	if err := validateRuntimeProfileName(name); err != nil {
		return err
	}
	if kind == "" {
		return invalid("必须填写 provider_kind")
	}
	if kind.IsNative() {
		return invalid("内置 oma_native 运行时档案无需重复创建")
	}
	if !s.isValidProviderKind(kind) {
		return invalid("不支持的 provider_kind")
	}
	if err := validateRuntimeEndpointURL(endpointURL, s.environment); err != nil {
		return invalid("endpoint_url 必须是有效的 HTTP(S) 地址")
	}
	return nil
}

func (s *RuntimeProfileService) isValidProviderKind(kind agentruntime.ProviderKind) bool {
	if s.registry != nil {
		return s.registry.IsValidProviderKind(kind)
	}
	return kind == agentruntime.ProviderOMANative || kind == agentruntime.ProviderQoderCloud
}

func (s *RuntimeProfileService) applyRuntimeProfilePatch(ctx context.Context, current *db.RuntimeProfile, input RuntimeProfileUpdateInput) error {
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if err := validateRuntimeProfileName(name); err != nil {
			return err
		}
		current.Name = name
	}
	if input.EndpointURL != nil {
		endpointURL := strings.TrimSpace(*input.EndpointURL)
		if !agentruntime.ProviderKind(current.ProviderKind).IsNative() {
			if err := validateRuntimeEndpointURL(endpointURL, s.environment); err != nil {
				return invalid("endpoint_url 必须是有效的 HTTP(S) 地址")
			}
		}
		current.EndpointURL = endpointURL
	}
	if input.IsolationMode != nil {
		mode, err := normalizeRuntimeIsolationMode(*input.IsolationMode, current.IsolationMode)
		if err != nil {
			return err
		}
		current.IsolationMode = mode
	}
	if input.Status != nil {
		status, err := normalizeRuntimeProfileStatus(*input.Status, current.Status)
		if err != nil {
			return err
		}
		if current.UUID == db.DefaultOMANativeRuntimeProfileUUID && status != "active" {
			return ErrRuntimeProfileProtected
		}
		current.Status = status
	}
	if input.Config != nil {
		cfg, err := normalizeJSONObject(*input.Config, "config")
		if err != nil {
			return err
		}
		current.Config = cfg
	}
	if input.CapabilityOverrides != nil {
		overrides, err := normalizeCapabilityOverridesJSON(agentruntime.ProviderKind(current.ProviderKind), *input.CapabilityOverrides)
		if err != nil {
			return err
		}
		current.CapabilityOverrides = overrides
	}
	if input.Credentials != nil {
		if !hasCredentialPayload(*input.Credentials) {
			return invalid("credentials 不能为空")
		}
		current.CredentialRevision++
		if err := s.sealCredentials(ctx, current, *input.Credentials); err != nil {
			return err
		}
	}
	return nil
}

func (s *RuntimeProfileService) sealCredentials(ctx context.Context, profile *db.RuntimeProfile, raw json.RawMessage) error {
	if s.secretSvc == nil {
		return ErrRuntimeProfileUnavailable
	}
	trimmed := bytes.TrimSpace(raw)
	if !json.Valid(trimmed) {
		return invalid("credentials 必须是有效的 JSON")
	}
	envelope, err := s.secretSvc.SealRuntimeProfile(ctx, secrets.RuntimeProfileBinding{
		Scope:              profile.Scope,
		OrganizationUUID:   profile.OrganizationUUID,
		RuntimeProfileUUID: profile.UUID,
		CredentialRevision: profile.CredentialRevision,
	}, trimmed)
	if err != nil {
		return ErrRuntimeProfileUnavailable
	}
	profile.SecretEnvelope = &envelope
	profile.CredentialHint = buildCredentialHint(trimmed)
	return nil
}

func (s *RuntimeProfileService) audit(ctx context.Context, actor Actor, profile db.RuntimeProfile, action string, fields []string, failure error) {
	result, code := "success", ""
	if failure != nil {
		result, code = "denied", publicError(failure).Code
	}
	s.logger.InfoContext(ctx, "platform runtime profile audit",
		"actor_account_uuid", actor.AccountUUID,
		"profile_uuid", profile.UUID,
		"profile_id", profile.ExternalID,
		"provider_kind", profile.ProviderKind,
		"action", action,
		"changed_fields", fields,
		"result", result,
		"error_code", code,
		"request_id", httpapi.RequestID(ctx),
	)
}

func (i RuntimeProfileUpdateInput) changedFields() []string {
	fields := make([]string, 0, 7)
	if i.Name != nil {
		fields = append(fields, "name")
	}
	if i.EndpointURL != nil {
		fields = append(fields, "endpoint_url")
	}
	if i.IsolationMode != nil {
		fields = append(fields, "isolation_mode")
	}
	if i.Config != nil {
		fields = append(fields, "config")
	}
	if i.CapabilityOverrides != nil {
		fields = append(fields, "capability_overrides")
	}
	if i.Credentials != nil {
		fields = append(fields, "credentials")
	}
	if i.Status != nil {
		fields = append(fields, "status")
	}
	return fields
}

func runtimeProfileViews(profiles []db.RuntimeProfile) ([]RuntimeProfileView, error) {
	views := make([]RuntimeProfileView, 0, len(profiles))
	for _, profile := range profiles {
		view, err := runtimeProfileView(profile)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func runtimeProfileView(profile db.RuntimeProfile) (RuntimeProfileView, error) {
	base := agentruntime.DefaultCapabilities(agentruntime.ProviderKind(profile.ProviderKind))
	effective, err := agentruntime.MergeCapabilityOverrides(base, profile.CapabilityOverrides)
	if err != nil {
		return RuntimeProfileView{}, ErrRuntimeProfileUnavailable
	}
	cfg := profile.Config
	if len(cfg) == 0 {
		cfg = json.RawMessage(`{}`)
	}
	overrides := profile.CapabilityOverrides
	if len(overrides) == 0 {
		overrides = json.RawMessage(`{}`)
	}
	return RuntimeProfileView{
		UUID:                  profile.UUID,
		ID:                    profile.ExternalID,
		Scope:                 profile.Scope,
		Name:                  profile.Name,
		ProviderKind:          profile.ProviderKind,
		EndpointURL:           profile.EndpointURL,
		IsolationMode:         profile.IsolationMode,
		Config:                cfg,
		CapabilityOverrides:   overrides,
		EffectiveCapabilities: effective,
		HasCredentials:        profile.SecretEnvelope != nil,
		CredentialHint:        profile.CredentialHint,
		CredentialRevision:    profile.CredentialRevision,
		Builtin:               profile.UUID == db.DefaultOMANativeRuntimeProfileUUID,
		Status:                profile.Status,
		LastHealthStatus:      profile.LastHealthStatus,
		LastHealthCheckedAt:   profile.LastHealthCheckedAt,
		LastHealthError:       profile.LastHealthError,
		Version:               profile.Version,
		CreatedAt:             profile.CreatedAt,
		UpdatedAt:             profile.UpdatedAt,
	}, nil
}

func validateRuntimeProfileName(name string) error {
	if name == "" {
		return invalid("必须填写名称")
	}
	if utf8.RuneCountInString(name) > 200 {
		return invalid("名称不能超过 200 个字符")
	}
	return nil
}

func validateRuntimeEndpointURL(value, environment string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("invalid url")
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if environment == config.EnvironmentProd {
			return errors.New("http not allowed in prod")
		}
		return nil
	default:
		return errors.New("unsupported scheme")
	}
}

func normalizeRuntimeIsolationMode(value, fallback string) (string, error) {
	mode := strings.TrimSpace(value)
	if mode == "" {
		return fallback, nil
	}
	switch mode {
	case "isolated", "shared_upstream":
		return mode, nil
	default:
		return "", invalid("isolation_mode 无效")
	}
}

func normalizeRuntimeProfileStatus(value, fallback string) (string, error) {
	status := strings.TrimSpace(value)
	if status == "" {
		return fallback, nil
	}
	switch status {
	case "active", "disabled", "archived":
		return status, nil
	default:
		return "", invalid("status 无效")
	}
}

func normalizeJSONObject(raw json.RawMessage, fieldName string) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage(`{}`), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, invalid(fieldName + " 必须是有效的 JSON 对象")
	}
	return trimmed, nil
}

func normalizeCapabilityOverridesJSON(kind agentruntime.ProviderKind, raw json.RawMessage) (json.RawMessage, error) {
	normalized, err := normalizeJSONObject(raw, "capability_overrides")
	if err != nil {
		return nil, err
	}
	if _, err := agentruntime.MergeCapabilityOverrides(agentruntime.DefaultCapabilities(kind), normalized); err != nil {
		return nil, invalid("capability_overrides 包含无效字段或值")
	}
	return normalized, nil
}

func normalizePolicyProfileUUIDs(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, invalid("allowed_runtime_profile_uuids 不能为空")
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		id := strings.TrimSpace(item)
		if _, err := uuid.Parse(id); err != nil {
			return nil, invalid("allowed_runtime_profile_uuids 包含无效的 UUID")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func hasCredentialPayload(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) && !bytes.Equal(trimmed, []byte(`""`)) && !bytes.Equal(trimmed, []byte(`{}`))
}

func buildCredentialHint(raw []byte) string {
	var fields struct {
		APIKey string `json:"api_key"`
		Token  string `json:"token"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(raw, &fields); err == nil {
		for _, candidate := range []string{fields.APIKey, fields.Token, fields.Secret} {
			if candidate = strings.TrimSpace(candidate); candidate != "" {
				return maskSecretHint(candidate)
			}
		}
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && strings.TrimSpace(single) != "" {
		return maskSecretHint(strings.TrimSpace(single))
	}
	return "configured"
}

func maskSecretHint(secret string) string {
	runes := []rune(secret)
	if len(runes) <= 6 {
		return "****"
	}
	if len(runes) <= 10 {
		return string(runes[:2]) + "..." + string(runes[len(runes)-2:])
	}
	return string(runes[:4]) + "..." + string(runes[len(runes)-4:])
}
