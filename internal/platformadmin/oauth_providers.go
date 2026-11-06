package platformadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"uuid"

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

type OAuthProviderService struct {
	db        *db.DB
	secretSvc *secrets.Service
	logger    *slog.Logger
}

func NewOAuthProviderService(database *db.DB, secretSvc *secrets.Service, logger *slog.Logger) *OAuthProviderService {
	return &OAuthProviderService{db: database, secretSvc: secretSvc, logger: logging.LoggerOrDefault(logger)}
}

func (s *OAuthProviderService) List(ctx context.Context) ([]OAuthProviderView, error) {
	providers, err := s.db.ListPlatformOAuthProviders(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]OAuthProviderView, 0, len(providers))
	for _, provider := range providers {
		views = append(views, oauthProviderView(provider))
	}
	return views, nil
}

func (s *OAuthProviderService) Create(ctx context.Context, actor Actor, input OAuthProviderCreateInput) (OAuthProviderView, error) {
	name := strings.TrimSpace(input.Name)
	authorizeEndpoint := strings.TrimSpace(input.AuthorizeEndpoint)
	tokenEndpoint := strings.TrimSpace(input.TokenEndpoint)
	clientID := strings.TrimSpace(input.ClientID)
	clientSecret := strings.TrimSpace(input.ClientSecret)
	if err := validateOAuthProviderFields(name, authorizeEndpoint, tokenEndpoint, clientID); err != nil {
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
		UUID:                       uuid.NewV4().String(),
		ExternalID:                 externalID,
		Name:                       name,
		AuthorizeEndpoint:          authorizeEndpoint,
		TokenEndpoint:              tokenEndpoint,
		Scope:                      strings.TrimSpace(input.Scope),
		ClientID:                   clientID,
		TokenEndpointAuthMethod:    authMethod,
		TokenEndpointEncoding:      encoding,
		CreatedByPlatformAdminUUID: actor.AccountUUID,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	if err := s.sealClientSecret(ctx, &provider, clientSecret); err != nil {
		return OAuthProviderView{}, err
	}
	created, err := s.db.CreatePlatformOAuthProvider(ctx, provider)
	if err != nil {
		if isOAuthProviderNameConflict(err) {
			return OAuthProviderView{}, ErrOAuthProviderNameConflict
		}
		return OAuthProviderView{}, err
	}
	return oauthProviderView(created), nil
}

func (s *OAuthProviderService) Update(ctx context.Context, actor Actor, externalID string, input OAuthProviderUpdateInput) (OAuthProviderView, error) {
	current, err := s.db.GetPlatformOAuthProviderByExternalID(ctx, externalID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return OAuthProviderView{}, ErrNotFound
		}
		return OAuthProviderView{}, err
	}
	if input.Name != nil {
		current.Name = strings.TrimSpace(*input.Name)
	}
	if input.AuthorizeEndpoint != nil {
		current.AuthorizeEndpoint = strings.TrimSpace(*input.AuthorizeEndpoint)
	}
	if input.TokenEndpoint != nil {
		current.TokenEndpoint = strings.TrimSpace(*input.TokenEndpoint)
	}
	if input.Scope != nil {
		current.Scope = strings.TrimSpace(*input.Scope)
	}
	if input.ClientID != nil {
		current.ClientID = strings.TrimSpace(*input.ClientID)
	}
	if err := validateOAuthProviderFields(current.Name, current.AuthorizeEndpoint, current.TokenEndpoint, current.ClientID); err != nil {
		return OAuthProviderView{}, err
	}
	if input.TokenEndpointAuthMethod != nil {
		authMethod, err := oauthProviderAuthMethod(input.TokenEndpointAuthMethod, "")
		if err != nil {
			return OAuthProviderView{}, err
		}
		current.TokenEndpointAuthMethod = authMethod
	}
	if input.TokenEndpointEncoding != nil {
		encoding, err := oauthProviderEncoding(input.TokenEndpointEncoding, "")
		if err != nil {
			return OAuthProviderView{}, err
		}
		current.TokenEndpointEncoding = encoding
	}
	if input.ClientSecret != nil {
		clientSecret := strings.TrimSpace(*input.ClientSecret)
		if clientSecret == "" {
			return OAuthProviderView{}, invalid("client_secret 不能为空")
		}
		if err := s.sealClientSecret(ctx, &current, clientSecret); err != nil {
			return OAuthProviderView{}, err
		}
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
		return OAuthProviderView{}, err
	}
	return oauthProviderView(updated), nil
}

func (s *OAuthProviderService) Delete(ctx context.Context, externalID string) error {
	if err := s.db.DeletePlatformOAuthProvider(ctx, externalID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *OAuthProviderService) sealClientSecret(ctx context.Context, provider *db.PlatformOAuthProvider, clientSecret string) error {
	if s.secretSvc == nil {
		return errors.New("platform oauth provider secret service is unavailable")
	}
	envelope, err := s.secretSvc.Seal(ctx, platformoauth.ProviderSecretBinding(provider.ExternalID), []byte(clientSecret))
	if err != nil {
		return fmt.Errorf("seal platform oauth provider secret: %w", err)
	}
	provider.ClientSecretEnvelope = &envelope
	return nil
}

func oauthProviderView(provider db.PlatformOAuthProvider) OAuthProviderView {
	return OAuthProviderView{
		ID:                      provider.ExternalID,
		Name:                    provider.Name,
		AuthorizeEndpoint:       provider.AuthorizeEndpoint,
		TokenEndpoint:           provider.TokenEndpoint,
		Scope:                   provider.Scope,
		ClientID:                provider.ClientID,
		HasClientSecret:         provider.ClientSecretEnvelope != nil,
		TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   provider.TokenEndpointEncoding,
		CreatedAt:               provider.CreatedAt,
		UpdatedAt:               provider.UpdatedAt,
	}
}

func validateOAuthProviderFields(name, authorizeEndpoint, tokenEndpoint, clientID string) error {
	if name == "" {
		return invalid("必须填写名称")
	}
	if len(name) > 200 {
		return invalid("名称不能超过 200 个字符")
	}
	if err := validateOAuthProviderURL(authorizeEndpoint); err != nil {
		return invalid("Authorize Endpoint 必须是有效的 HTTP(S) 地址")
	}
	if err := validateOAuthProviderURL(tokenEndpoint); err != nil {
		return invalid("Token Endpoint 必须是有效的 HTTP(S) 地址")
	}
	if clientID == "" {
		return invalid("必须填写 client_id")
	}
	return nil
}

func validateOAuthProviderURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Host == "" {
		return errors.New("missing host")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
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
