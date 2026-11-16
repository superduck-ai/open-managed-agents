package platformadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/logging"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

type OAuthProviderCreateInput struct {
	WorkspaceUUID           string   `json:"-"`
	DefaultAllowedHosts     []string `json:"default_allowed_hosts"`
	DefaultSecretName       string   `json:"default_secret_name"`
	Enabled                 *bool    `json:"enabled"`
	Name                    string   `json:"name"`
	LogoURL                 string   `json:"logo_url"`
	AuthorizeEndpoint       string   `json:"authorize_endpoint"`
	TokenEndpoint           string   `json:"token_endpoint"`
	Scope                   string   `json:"scope"`
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret"`
	TokenEndpointAuthMethod *string  `json:"token_endpoint_auth_method"`
	TokenEndpointEncoding   *string  `json:"token_endpoint_encoding"`
}

type OAuthProviderUpdateInput struct {
	WorkspaceUUID           string    `json:"-"`
	DefaultAllowedHosts     *[]string `json:"default_allowed_hosts"`
	Archived                *bool     `json:"archived"`
	DefaultSecretName       *string   `json:"default_secret_name"`
	Enabled                 *bool     `json:"enabled"`
	ExpectedVersion         int64     `json:"expected_version"`
	Name                    *string   `json:"name"`
	LogoURL                 *string   `json:"logo_url"`
	AuthorizeEndpoint       *string   `json:"authorize_endpoint"`
	TokenEndpoint           *string   `json:"token_endpoint"`
	Scope                   *string   `json:"scope"`
	ClientID                *string   `json:"client_id"`
	ClientSecret            *string   `json:"client_secret"`
	TokenEndpointAuthMethod *string   `json:"token_endpoint_auth_method"`
	TokenEndpointEncoding   *string   `json:"token_endpoint_encoding"`
}

type OAuthProviderView struct {
	Archived                bool      `json:"archived"`
	Enabled                 bool      `json:"enabled"`
	ProviderKey             string    `json:"provider_key"`
	Builtin                 bool      `json:"builtin"`
	Protected               bool      `json:"protected"`
	HasClientID             bool      `json:"has_client_id"`
	Configured              bool      `json:"configured"`
	DefaultSecretName       string    `json:"default_secret_name"`
	DefaultAllowedHosts     []string  `json:"default_allowed_hosts"`
	Version                 int64     `json:"version"`
	ID                      string    `json:"id"`
	Name                    string    `json:"name"`
	LogoURL                 string    `json:"logo_url"`
	AuthorizeEndpoint       string    `json:"authorize_endpoint"`
	TokenEndpoint           string    `json:"token_endpoint"`
	Scope                   string    `json:"scope"`
	ClientID                string    `json:"client_id"`
	HasClientSecret         bool      `json:"has_client_secret"`
	TokenEndpointAuthMethod string    `json:"token_endpoint_auth_method"`
	TokenEndpointEncoding   string    `json:"token_endpoint_encoding"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type oauthProviderStore interface {
	ListPlatformOAuthProviders(context.Context) ([]db.PlatformOAuthProvider, error)
	GetPlatformOAuthProviderByExternalID(context.Context, string) (db.PlatformOAuthProvider, error)
	CreatePlatformOAuthProvider(context.Context, db.PlatformOAuthProvider) (db.PlatformOAuthProvider, error)
	UpdatePlatformOAuthProvider(context.Context, db.PlatformOAuthProvider) (db.PlatformOAuthProvider, error)
	DeletePlatformOAuthProvider(context.Context, string, int64) error
}

type OAuthProviderService struct {
	db          oauthProviderStore
	secretSvc   *secrets.Service
	logger      *slog.Logger
	environment string
}

func NewOAuthProviderService(database oauthProviderStore, secretSvc *secrets.Service, logger *slog.Logger) *OAuthProviderService {
	return &OAuthProviderService{db: database, secretSvc: secretSvc, logger: logging.LoggerOrDefault(logger)}
}

func (s *OAuthProviderService) WithEnvironment(environment string) *OAuthProviderService {
	if s == nil {
		return nil
	}
	scoped := *s
	scoped.environment = environment
	return &scoped
}

func (s *OAuthProviderService) List(ctx context.Context) ([]OAuthProviderView, error) {
	providers, err := s.db.ListPlatformOAuthProviders(ctx)
	if err != nil {
		return nil, ErrOAuthProviderUnavailable
	}
	views := make([]OAuthProviderView, 0, len(providers))
	for _, provider := range providers {
		view, err := s.view(ctx, provider)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *OAuthProviderService) Create(ctx context.Context, actor Actor, input OAuthProviderCreateInput) (view OAuthProviderView, failure error) {
	auditProvider := db.PlatformOAuthProvider{}
	defer func() {
		if failure != nil {
			s.audit(ctx, actor, auditProvider, "create", nil, failure)
		}
	}()
	name := strings.TrimSpace(input.Name)
	authorizeEndpoint := strings.TrimSpace(input.AuthorizeEndpoint)
	tokenEndpoint := strings.TrimSpace(input.TokenEndpoint)
	clientID := strings.TrimSpace(input.ClientID)
	clientSecret := strings.TrimSpace(input.ClientSecret)
	if err := validateOAuthProviderFields(name, authorizeEndpoint, tokenEndpoint, s.environment); err != nil {
		return OAuthProviderView{}, err
	}
	if (clientID == "") != (clientSecret == "") {
		return OAuthProviderView{}, invalidOAuthProviderClients()
	}
	logoURL, err := normalizeOAuthProviderLogoURL(input.LogoURL)
	if err != nil {
		return OAuthProviderView{}, err
	}
	authMethod, err := oauthProviderAuthMethod(input.TokenEndpointAuthMethod, "client_secret_post")
	if err != nil {
		return OAuthProviderView{}, err
	}
	encoding, err := oauthProviderEncoding(input.TokenEndpointEncoding, "form")
	if err != nil {
		return OAuthProviderView{}, err
	}
	secretName, err := normalizeOAuthProviderSecretName(input.DefaultSecretName)
	if err != nil {
		return OAuthProviderView{}, err
	}
	hosts, err := normalizeOAuthProviderHosts(input.DefaultAllowedHosts)
	if err != nil {
		return OAuthProviderView{}, err
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	externalID, err := ids.New("poap_")
	if err != nil {
		return OAuthProviderView{}, err
	}
	now := time.Now().UTC()
	provider := db.PlatformOAuthProvider{
		WorkspaceUUID: input.WorkspaceUUID, DefaultAllowedHosts: hosts,
		UUID:              uuid.NewV4().String(),
		DefaultSecretName: secretName,
		Enabled:           enabled,
		ExternalID:        externalID,
		Name:              name,
		LogoURL:           logoURL,
		AuthorizeEndpoint: authorizeEndpoint,
		TokenEndpoint:     tokenEndpoint,
		Scope:             strings.TrimSpace(input.Scope),

		TokenEndpointAuthMethod:    authMethod,
		TokenEndpointEncoding:      encoding,
		CreatedByPlatformAdminUUID: actor.AccountUUID,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	auditProvider = provider
	if clientID != "" {
		if err := s.sealClient(ctx, &provider, clientID, clientSecret); err != nil {
			return OAuthProviderView{}, err
		}
	}
	created, err := s.db.CreatePlatformOAuthProvider(ctx, provider)
	if err != nil {
		if isOAuthProviderNameConflict(err) {
			return OAuthProviderView{}, ErrOAuthProviderNameConflict
		}
		return OAuthProviderView{}, err
	}
	s.audit(ctx, actor, created, "create", []string{"default_allowed_hosts", "default_secret_name", "enabled", "name", "logo_url", "authorize_endpoint", "token_endpoint", "scope", "client_id", "client_secret", "token_endpoint_auth_method", "token_endpoint_encoding"}, nil)
	return oauthProviderView(created, clientID), nil
}

func (s *OAuthProviderService) GetWorkspace(ctx context.Context, workspaceUUID, externalID string) (OAuthProviderView, error) {
	provider, err := s.db.GetPlatformOAuthProviderByExternalID(ctx, externalID)
	if err != nil {
		return OAuthProviderView{}, mapOAuthProviderError(err)
	}
	if provider.WorkspaceUUID != workspaceUUID {
		return OAuthProviderView{}, ErrNotFound
	}
	return s.view(ctx, provider)
}

func (s *OAuthProviderService) Update(ctx context.Context, actor Actor, externalID string, input OAuthProviderUpdateInput) (view OAuthProviderView, failure error) {
	current, err := s.db.GetPlatformOAuthProviderByExternalID(ctx, externalID)
	defer func() {
		if failure != nil {
			s.audit(ctx, actor, current, "update", input.changedFields(), failure)
		}
	}()
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return OAuthProviderView{}, ErrNotFound
		}
		return OAuthProviderView{}, err
	}
	if current.WorkspaceUUID != input.WorkspaceUUID {
		return OAuthProviderView{}, ErrNotFound
	}
	if input.ExpectedVersion < 1 {
		return OAuthProviderView{}, invalid("必须提供 expected_version")
	}
	if err := platformoauth.ValidateProviderDefaults(current); err != nil {
		return OAuthProviderView{}, ErrOAuthProviderUnavailable
	}
	if input.ExpectedVersion != current.Version {
		return OAuthProviderView{}, ErrOAuthProviderVersion
	}
	initialConfiguration := current.ClientIDEnvelope == nil
	clientID := ""
	if current.ClientIDEnvelope != nil {
		clientID, err = platformoauth.OpenClientField(ctx, s.secretSvc, platformoauth.ProviderClientIDBinding(current.ExternalID), current.ClientIDEnvelope)
		if err != nil {
			return OAuthProviderView{}, ErrOAuthProviderUnavailable
		}
	}
	if input.LogoURL != nil {
		current.LogoURL, err = normalizeOAuthProviderLogoURL(*input.LogoURL)
		if err != nil {
			return OAuthProviderView{}, err
		}
	}
	if err := applyOAuthProviderFields(&current, input); err != nil {
		return OAuthProviderView{}, err
	}
	nextClientID := clientID
	if input.ClientID != nil {
		nextClientID = strings.TrimSpace(*input.ClientID)
	}
	if err := s.updateClient(ctx, &current, clientID, nextClientID, input); err != nil {
		return OAuthProviderView{}, err
	}
	current.UpdatedAt = time.Now().UTC()
	updated, err := s.db.UpdatePlatformOAuthProvider(ctx, current)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return OAuthProviderView{}, ErrNotFound
		}
		if isOAuthProviderNameConflict(err) {
			return OAuthProviderView{}, ErrOAuthProviderNameConflict
		}
		return OAuthProviderView{}, mapOAuthProviderError(err)
	}
	action := "update"
	if initialConfiguration && updated.ClientIDEnvelope != nil {
		action = "configure"
	}
	if input.ClientSecret != nil && action != "configure" {
		action = "rotate"
	}
	s.audit(ctx, actor, updated, action, input.changedFields(), nil)
	return oauthProviderView(updated, nextClientID), nil
}

func (s *OAuthProviderService) Delete(ctx context.Context, actor Actor, externalID string, expectedVersion int64) (failure error) {
	current, err := s.db.GetPlatformOAuthProviderByExternalID(ctx, externalID)
	defer func() {
		if failure != nil {
			s.audit(ctx, actor, current, "delete", nil, failure)
		}
	}()
	if err != nil {
		return mapOAuthProviderError(err)
	}
	if current.WorkspaceUUID != "" {
		return ErrNotFound
	}
	if expectedVersion < 1 {
		return invalid("必须提供 expected_version")
	}
	if err := s.db.DeletePlatformOAuthProvider(ctx, current.UUID, expectedVersion); err != nil {
		return mapOAuthProviderError(err)
	}
	s.audit(ctx, actor, current, "delete", nil, nil)
	return nil
}

func (s *OAuthProviderService) sealClient(ctx context.Context, provider *db.PlatformOAuthProvider, clientID, clientSecret string) error {
	idEnvelope, err := platformoauth.SealClientField(ctx, s.secretSvc, platformoauth.ProviderClientIDBinding(provider.ExternalID), clientID)
	if err != nil {
		return ErrOAuthProviderUnavailable
	}
	secretEnvelope, err := platformoauth.SealClientField(ctx, s.secretSvc, platformoauth.ProviderSecretBinding(provider.ExternalID), clientSecret)
	if err != nil {
		return ErrOAuthProviderUnavailable
	}
	provider.ClientIDEnvelope, provider.ClientSecretEnvelope = idEnvelope, secretEnvelope
	return nil
}

func (s *OAuthProviderService) updateClient(ctx context.Context, provider *db.PlatformOAuthProvider, oldID, clientID string, input OAuthProviderUpdateInput) error {
	if err := validateOAuthProviderFields(provider.Name, provider.AuthorizeEndpoint, provider.TokenEndpoint, s.environment); err != nil {
		return err
	}
	secret := ""
	if input.ClientSecret != nil {
		secret = strings.TrimSpace(*input.ClientSecret)
	}
	if clientID == "" {
		if oldID == "" && secret == "" {
			return nil
		}
		return invalidOAuthProviderClients()
	}
	if (clientID != oldID || provider.ClientSecretEnvelope == nil) && input.ClientSecret == nil {
		return invalid("更换或首次配置 client_id 时必须同时填写 client_secret")
	}
	if input.ClientSecret == nil {
		return nil
	}
	if secret == "" {
		return invalid("client_secret 不能为空")
	}
	if clientID != oldID || provider.ClientIDEnvelope == nil {
		return s.sealClient(ctx, provider, clientID, secret)
	}
	envelope, err := platformoauth.SealClientField(ctx, s.secretSvc, platformoauth.ProviderSecretBinding(provider.ExternalID), secret)
	if err != nil {
		return ErrOAuthProviderUnavailable
	}
	provider.ClientSecretEnvelope = envelope
	return nil
}

func (s *OAuthProviderService) view(ctx context.Context, provider db.PlatformOAuthProvider) (OAuthProviderView, error) {
	if err := platformoauth.ValidateProviderDefaults(provider); err != nil {
		return OAuthProviderView{}, ErrOAuthProviderUnavailable
	}
	clientID := ""
	if provider.ClientIDEnvelope != nil {
		var err error
		clientID, err = platformoauth.OpenClientField(ctx, s.secretSvc, platformoauth.ProviderClientIDBinding(provider.ExternalID), provider.ClientIDEnvelope)
		if err != nil {
			return OAuthProviderView{}, ErrOAuthProviderUnavailable
		}
	}
	return oauthProviderView(provider, clientID), nil
}

func oauthProviderView(provider db.PlatformOAuthProvider, clientID string) OAuthProviderView {
	return OAuthProviderView{
		Archived: provider.Archived, Enabled: provider.Enabled, ID: provider.ExternalID, ProviderKey: platformoauth.ProviderKey(provider), Builtin: provider.BuiltinKey != "", Protected: provider.Protected,
		HasClientID: provider.ClientIDEnvelope != nil, HasClientSecret: provider.ClientSecretEnvelope != nil,
		Configured: provider.ClientIDEnvelope != nil && provider.ClientSecretEnvelope != nil, Version: provider.Version,
		Name: provider.Name, LogoURL: provider.LogoURL, AuthorizeEndpoint: provider.AuthorizeEndpoint, TokenEndpoint: provider.TokenEndpoint,
		Scope: provider.Scope, ClientID: clientID, DefaultSecretName: provider.DefaultSecretName, DefaultAllowedHosts: provider.DefaultAllowedHosts, TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding: provider.TokenEndpointEncoding, CreatedAt: provider.CreatedAt, UpdatedAt: provider.UpdatedAt,
	}
}

func (s *OAuthProviderService) audit(ctx context.Context, actor Actor, provider db.PlatformOAuthProvider, action string, fields []string, failure error) {
	result, code := "success", ""
	key := platformoauth.ProviderKey(provider)
	if provider.ExternalID == "" {
		key = ""
	}
	actorType := "account"
	if actor.AccountUUID == "" {
		actorType = "system"
	}
	if action == "integrity" {
		key = platformoauth.ProviderKey(provider)
	}
	if failure != nil {
		result, code = "denied", publicError(failure).Code
	}
	s.logger.InfoContext(ctx, "platform oauth provider audit", "actor_account_uuid", actor.AccountUUID, "actor_type", actorType,
		"workspace_uuid", provider.WorkspaceUUID, "provider_uuid", provider.UUID, "provider_key", key, "action", action,
		"changed_fields", fields, "result", result, "error_code", code, "request_id", httpapi.RequestID(ctx))
}

func (i OAuthProviderUpdateInput) changedFields() []string {
	fields := make([]string, 0)
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"default_secret_name", i.DefaultSecretName}, {"name", i.Name}, {"logo_url", i.LogoURL}, {"authorize_endpoint", i.AuthorizeEndpoint}, {"token_endpoint", i.TokenEndpoint}, {"scope", i.Scope},
		{"client_id", i.ClientID}, {"client_secret", i.ClientSecret}, {"token_endpoint_auth_method", i.TokenEndpointAuthMethod}, {"token_endpoint_encoding", i.TokenEndpointEncoding},
	} {
		if field.value != nil {
			fields = append(fields, field.name)
		}
	}
	if i.DefaultAllowedHosts != nil {
		fields = append(fields, "default_allowed_hosts")
	}
	if i.Archived != nil {
		fields = append(fields, "archived")
	}
	if i.Enabled != nil {
		fields = append(fields, "enabled")
	}
	return fields
}

func (i *OAuthProviderUpdateInput) UnmarshalJSON(data []byte) error {
	type plain OAuthProviderUpdateInput
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return invalid("请求 JSON 无效")
	}
	for _, key := range []string{"client_id", "client_secret", "default_secret_name", "enabled", "archived", "default_allowed_hosts"} {
		if value, exists := fields[key]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid("配置字段不能为 null")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(i))
}

func applyOAuthProviderFields(provider *db.PlatformOAuthProvider, input OAuthProviderUpdateInput) error {
	if input.DefaultAllowedHosts != nil {
		hosts, err := normalizeOAuthProviderHosts(*input.DefaultAllowedHosts)
		if err != nil {
			return err
		}
		provider.DefaultAllowedHosts = hosts
	}

	if input.Archived != nil {
		provider.Archived = *input.Archived
	}
	if input.DefaultSecretName != nil {
		name, err := normalizeOAuthProviderSecretName(*input.DefaultSecretName)
		if err != nil {
			return err
		}
		provider.DefaultSecretName = name
	}
	if input.Enabled != nil {
		provider.Enabled = *input.Enabled
	}
	for _, field := range []struct {
		source *string
		target *string
	}{
		{input.Name, &provider.Name}, {input.AuthorizeEndpoint, &provider.AuthorizeEndpoint}, {input.TokenEndpoint, &provider.TokenEndpoint}, {input.Scope, &provider.Scope},
	} {
		if field.source != nil {
			*field.target = strings.TrimSpace(*field.source)
		}
	}
	var err error
	provider.TokenEndpointAuthMethod, err = oauthProviderAuthMethod(input.TokenEndpointAuthMethod, provider.TokenEndpointAuthMethod)
	if err != nil {
		return err
	}
	provider.TokenEndpointEncoding, err = oauthProviderEncoding(input.TokenEndpointEncoding, provider.TokenEndpointEncoding)
	return err
}

func validateOAuthProviderFields(name, authorizeEndpoint, tokenEndpoint, environment string) error {
	if name == "" {
		return invalid("必须填写名称")
	}
	if utf8.RuneCountInString(name) > 200 {
		return invalid("名称不能超过 200 个字符")
	}
	if err := validateOAuthProviderURL(authorizeEndpoint, environment); err != nil {
		return invalid("Authorize Endpoint 必须是有效的 HTTP(S) 地址")
	}
	if err := validateOAuthProviderURL(tokenEndpoint, environment); err != nil {
		return invalid("Token Endpoint 必须是有效的 HTTP(S) 地址")
	}
	return nil
}

func validateOAuthProviderURL(value, environment string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Host == "" {
		return errors.New("missing host")
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if environment == config.EnvironmentProd {
			return errors.New("http is not allowed in production")
		}
	default:
		return errors.New("unsupported scheme")
	}
	return nil
}

func oauthProviderAuthMethod(value *string, fallback string) (string, error) {
	if value == nil {
		return fallback, nil
	}
	method := strings.TrimSpace(*value)
	switch method {
	case "client_secret_post", "client_secret_basic", "none":
		return method, nil
	default:
		return "", invalid("token_endpoint_auth_method 无效")
	}
}

func oauthProviderEncoding(value *string, fallback string) (string, error) {
	if value == nil {
		return fallback, nil
	}
	encoding := strings.TrimSpace(*value)
	switch encoding {
	case "form", "json":
		return encoding, nil
	default:
		return "", invalid("token_endpoint_encoding 无效")
	}
}

func isOAuthProviderNameConflict(err error) bool {
	return errors.Is(err, db.ErrDuplicate)
}

func normalizeOAuthProviderLogoURL(value string) (string, error) {
	logoURL := strings.TrimSpace(value)
	if logoURL == "" {
		return "", nil
	}
	parsed, err := url.Parse(logoURL)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", invalidOAuthProviderLogoURL()
	}
	return logoURL, nil
}

func normalizeOAuthProviderSecretName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name != "" {
		if _, err := vaults.ParseEnvOAuthSecretName(name); err != nil {
			return "", invalid("默认变量名必须是有效的环境变量名称")
		}
	}
	return name, nil
}

func normalizeOAuthProviderHosts(hosts []string) ([]string, error) {
	normalized := make([]string, len(hosts))
	for i, host := range hosts {
		normalized[i] = strings.TrimSpace(host)
	}
	if len(normalized) > 0 {
		if err := vaults.ValidateEnvOAuthNetworking("limited", normalized); err != nil {
			return nil, invalidOAuthProviderHosts()
		}
	}
	return normalized, nil
}
