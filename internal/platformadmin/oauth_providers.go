package platformadmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"log/slog"
	"net/url"
	"slices"
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
)

type OAuthProviderCreateInput struct {
	Name                    string  `json:"name"`
	AuthorizeEndpoint       string  `json:"authorize_endpoint"`
	TokenEndpoint           string  `json:"token_endpoint"`
	Scope                   string  `json:"scope"`
	ClientID                string  `json:"client_id"`
	ClientSecret            string  `json:"client_secret"`
	TokenEndpointAuthMethod *string `json:"token_endpoint_auth_method"`
	TokenEndpointEncoding   *string `json:"token_endpoint_encoding"`
}

type OAuthProviderUpdateInput struct {
	fixedFields             bool
	ExpectedVersion         int64   `json:"expected_version"`
	Name                    *string `json:"name"`
	AuthorizeEndpoint       *string `json:"authorize_endpoint"`
	TokenEndpoint           *string `json:"token_endpoint"`
	Scope                   *string `json:"scope"`
	ClientID                *string `json:"client_id"`
	ClientSecret            *string `json:"client_secret"`
	TokenEndpointAuthMethod *string `json:"token_endpoint_auth_method"`
	TokenEndpointEncoding   *string `json:"token_endpoint_encoding"`
}

type OAuthProviderView struct {
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
	if missing := platformoauth.MissingBuiltinProviderKey(providers); missing != "" {
		s.audit(ctx, Actor{}, db.PlatformOAuthProvider{BuiltinKey: strings.TrimPrefix(missing, "builtin:")}, "integrity", nil, ErrOAuthProviderUnavailable)
		return nil, ErrOAuthProviderUnavailable
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
	if err := validateOAuthProviderFields(name, authorizeEndpoint, tokenEndpoint, clientID, s.environment); err != nil {
		return OAuthProviderView{}, err
	}
	if clientSecret == "" {
		return OAuthProviderView{}, invalid("必须填写 client_secret")
	}
	authMethod, err := oauthProviderAuthMethod(input.TokenEndpointAuthMethod, "client_secret_post")
	if err != nil {
		return OAuthProviderView{}, err
	}
	encoding, err := oauthProviderEncoding(input.TokenEndpointEncoding, "form")
	if err != nil {
		return OAuthProviderView{}, err
	}
	externalID, err := ids.New("poap_")
	if err != nil {
		return OAuthProviderView{}, err
	}
	now := time.Now().UTC()
	provider := db.PlatformOAuthProvider{
		UUID:              uuid.NewV4().String(),
		ExternalID:        externalID,
		Name:              name,
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
	if err := s.sealClient(ctx, &provider, clientID, clientSecret); err != nil {
		return OAuthProviderView{}, err
	}
	created, err := s.db.CreatePlatformOAuthProvider(ctx, provider)
	if err != nil {
		if isOAuthProviderNameConflict(err) {
			return OAuthProviderView{}, ErrOAuthProviderNameConflict
		}
		return OAuthProviderView{}, err
	}
	s.audit(ctx, actor, created, "create", []string{"name", "authorize_endpoint", "token_endpoint", "scope", "client_id", "client_secret", "token_endpoint_auth_method", "token_endpoint_encoding"}, nil)
	return s.view(ctx, created)
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
	if input.ExpectedVersion < 1 {
		return OAuthProviderView{}, invalid("必须提供 expected_version")
	}
	if input.ExpectedVersion != current.Version {
		return OAuthProviderView{}, ErrOAuthProviderVersion
	}
	if current.Protected && input.fixedFieldsPresent() {
		return OAuthProviderView{}, invalid("内置服务方固定字段不可编辑")
	}
	initialConfiguration := current.ClientIDEnvelope == nil
	clientID := ""
	if current.ClientIDEnvelope != nil {
		clientID, err = platformoauth.OpenClientField(ctx, s.secretSvc, platformoauth.ProviderClientIDBinding(current.ExternalID), current.ClientIDEnvelope)
		if err != nil {
			return OAuthProviderView{}, ErrOAuthProviderUnavailable
		}
	}
	if err := applyOAuthProviderFields(&current, input); err != nil {
		return OAuthProviderView{}, err
	}
	if current.BuiltinKey == "feishu" && !slices.Contains(strings.Fields(current.Scope), "offline_access") {
		return OAuthProviderView{}, invalid("飞书 scope 必须包含 offline_access")
	}
	if current.BuiltinKey == "friday" && current.Scope != "read" {
		return OAuthProviderView{}, invalid("Friday scope 必须为 read")
	}
	if err := s.updateClient(ctx, &current, clientID, input); err != nil {
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
	if initialConfiguration {
		action = "configure"
	}
	if input.ClientSecret != nil && action != "configure" {
		action = "rotate"
	}
	s.audit(ctx, actor, updated, action, input.changedFields(), nil)
	return s.view(ctx, updated)
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
	if current.Protected {
		return ErrOAuthProviderProtected
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

func (s *OAuthProviderService) updateClient(ctx context.Context, provider *db.PlatformOAuthProvider, oldID string, input OAuthProviderUpdateInput) error {
	clientID := oldID
	if input.ClientID != nil {
		clientID = strings.TrimSpace(*input.ClientID)
	}
	if err := validateOAuthProviderFields(provider.Name, provider.AuthorizeEndpoint, provider.TokenEndpoint, clientID, s.environment); err != nil {
		return err
	}
	if (clientID != oldID || provider.ClientSecretEnvelope == nil) && input.ClientSecret == nil {
		return invalid("更换或首次配置 client_id 时必须同时填写 client_secret")
	}
	if input.ClientSecret == nil {
		return nil
	}
	secret := strings.TrimSpace(*input.ClientSecret)
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
	return OAuthProviderView{
		ID: provider.ExternalID, ProviderKey: platformoauth.ProviderKey(provider), Builtin: provider.BuiltinKey != "", Protected: provider.Protected,
		HasClientID: provider.ClientIDEnvelope != nil, HasClientSecret: provider.ClientSecretEnvelope != nil,
		Configured: provider.ClientIDEnvelope != nil && provider.ClientSecretEnvelope != nil, Version: provider.Version,
		Name: provider.Name, AuthorizeEndpoint: provider.AuthorizeEndpoint, TokenEndpoint: provider.TokenEndpoint,
		Scope: provider.Scope, ClientID: clientID, DefaultSecretName: provider.DefaultSecretName, DefaultAllowedHosts: provider.DefaultAllowedHosts, TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding: provider.TokenEndpointEncoding, CreatedAt: provider.CreatedAt, UpdatedAt: provider.UpdatedAt,
	}, nil
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
		"provider_uuid", provider.UUID, "provider_key", key, "action", action,
		"changed_fields", fields, "result", result, "error_code", code, "request_id", httpapi.RequestID(ctx))
}

func (i OAuthProviderUpdateInput) fixedFieldsPresent() bool {
	return i.fixedFields || i.Name != nil || i.AuthorizeEndpoint != nil || i.TokenEndpoint != nil || i.TokenEndpointAuthMethod != nil || i.TokenEndpointEncoding != nil
}

func (i OAuthProviderUpdateInput) changedFields() []string {
	fields := make([]string, 0)
	for _, field := range []struct {
		name  string
		value *string
	}{
		{"name", i.Name}, {"authorize_endpoint", i.AuthorizeEndpoint}, {"token_endpoint", i.TokenEndpoint}, {"scope", i.Scope},
		{"client_id", i.ClientID}, {"client_secret", i.ClientSecret}, {"token_endpoint_auth_method", i.TokenEndpointAuthMethod}, {"token_endpoint_encoding", i.TokenEndpointEncoding},
	} {
		if field.value != nil {
			fields = append(fields, field.name)
		}
	}
	return fields
}

func (i *OAuthProviderUpdateInput) UnmarshalJSON(data []byte) error {
	type plain OAuthProviderUpdateInput
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return invalid("请求 JSON 无效")
	}
	for _, key := range []string{"client_id", "client_secret"} {
		if value, exists := fields[key]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return invalid("客户端字段不能为 null")
		}
	}
	for _, key := range []string{"name", "authorize_endpoint", "token_endpoint", "token_endpoint_auth_method", "token_endpoint_encoding"} {
		if _, exists := fields[key]; exists {
			i.fixedFields = true
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode((*plain)(i))
}

func applyOAuthProviderFields(provider *db.PlatformOAuthProvider, input OAuthProviderUpdateInput) error {
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

func validateOAuthProviderFields(name, authorizeEndpoint, tokenEndpoint, clientID, environment string) error {
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
	if clientID == "" {
		return invalid("必须填写 client_id")
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
