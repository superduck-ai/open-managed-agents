package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"

	"github.com/go-chi/chi/v5"
)

const (
	envOAuthFlowTTL = 15 * time.Minute

	envOAuthHTTPTimeout = 15 * time.Second

	envOAuthRedirectPath = "/oauth/vault/env-success"
)

var envOAuthTokenHTTPClient = &http.Client{
	Timeout: envOAuthHTTPTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

type envOAuthNetworkingInput struct {
	Type         string   `json:"type"`
	AllowedHosts []string `json:"allowed_hosts"`
}

type envOAuthVaultAuthStartRequest struct {
	CredentialID string                   `json:"credential_id"`
	ProviderID   string                   `json:"provider_id"`
	VaultID      string                   `json:"vault_id"`
	WorkspaceID  string                   `json:"workspace_id"`
	SecretName   string                   `json:"secret_name"`
	DisplayName  string                   `json:"display_name"`
	Networking   *envOAuthNetworkingInput `json:"networking"`
	RedirectURL  string                   `json:"redirect_url"`
}

type envOAuthVaultAuthStartResponse struct {
	OAuthFlowID  string `json:"oauth_flow_id"`
	AuthorizeURL string `json:"authorize_url"`
}

type envOAuthProviderResponse struct {
	Enabled             bool     `json:"enabled"`
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	LogoURL             string   `json:"logo_url"`
	Builtin             bool     `json:"builtin"`
	Configured          bool     `json:"configured"`
	AuthorizeEndpoint   string   `json:"authorize_endpoint"`
	TokenEndpoint       string   `json:"token_endpoint"`
	Scope               string   `json:"scope"`
	DefaultSecretName   string   `json:"default_secret_name"`
	DefaultAllowedHosts []string `json:"default_allowed_hosts"`
}

type envOAuthErrorResponse struct {
	ErrorCode   string `json:"error_code,omitempty"`
	OAuthFlowID string `json:"oauth_flow_id,omitempty"`
}

type envOAuthCallbackPayload struct {
	Type         string `json:"type"`
	Status       string `json:"status"`
	CredentialID string `json:"credential_id,omitempty"`
	VaultID      string `json:"vault_id,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	FlowID       string `json:"flow_id,omitempty"`
	Message      string `json:"message,omitempty"`
}

func (s *Server) handleEnvOAuthProviders(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorizeEnvOAuthOrganization(w, r)
	if !ok {
		return
	}
	workspace, ok := s.resolveEnvOAuthWorkspace(w, r, principal, strings.TrimSpace(r.URL.Query().Get("workspace_id")))
	if !ok {
		return
	}
	providers, err := s.envOAuthProviders.ListProviders(r.Context(), workspace.UUID)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list env oauth providers", "error", err)
		httpapi.WriteJSON(w, http.StatusServiceUnavailable, envOAuthErrorResponse{ErrorCode: envOAuthErrorProviderUnavailable})
		return
	}
	response := make([]envOAuthProviderResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, envOAuthProviderResponse{
			Enabled:             provider.Enabled,
			ID:                  provider.ProviderKey,
			Name:                provider.Name,
			LogoURL:             provider.LogoURL,
			Builtin:             provider.Builtin,
			Configured:          provider.Configured,
			AuthorizeEndpoint:   provider.AuthorizeEndpoint,
			TokenEndpoint:       provider.TokenEndpoint,
			Scope:               provider.Scope,
			DefaultSecretName:   provider.DefaultSecretName,
			DefaultAllowedHosts: append([]string(nil), provider.DefaultAllowedHosts...),
		})
	}
	httpapi.WriteJSON(w, http.StatusOK, response)
}

func (s *Server) handleEnvOAuthVaultAuthStart(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorizeEnvOAuthOrganization(w, r)
	if !ok {
		return
	}
	req, redirectURL, secretName, ok := s.decodeEnvOAuthStartRequest(w, r)
	if !ok {
		return
	}

	workspace, ok := s.resolveEnvOAuthWorkspace(w, r, principal, req.WorkspaceID)
	if !ok {
		return
	}
	vault, err := s.db.GetVaultByExternalIDOrUUID(r.Context(), workspace.UUID, req.VaultID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeEnvOAuthStartError(w, status, envOAuthErrorInvalidRequest, "")
		return
	}
	if vault.ArchivedAt != nil {
		writeEnvOAuthStartError(w, http.StatusConflict, envOAuthErrorInvalidRequest, "")
		return
	}

	provider, err := s.envOAuthProviders.ResolveEnvOAuthProvider(r.Context(), req.ProviderID)
	if err != nil || provider.AuthorizeEndpoint == "" || provider.TokenEndpoint == "" || provider.ClientID == "" || (provider.WorkspaceUUID != "" && provider.WorkspaceUUID != workspace.UUID) {
		writeEnvOAuthStartError(w, http.StatusServiceUnavailable, envOAuthErrorProviderUnavailable, "")
		return
	}

	credentials, _, err := s.db.ListVaultCredentialsPage(r.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceUUID:   workspace.UUID,
		VaultExternalID: vault.ExternalID,
		Limit:           50,
	})
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list env oauth vault credentials", "error", err)
		writeEnvOAuthStartError(w, http.StatusInternalServerError, envOAuthErrorInvalidRequest, "")
		return
	}
	if !envOAuthSecretNameAvailable(credentials, secretName, req.ProviderID, req.CredentialID) {
		writeEnvOAuthStartError(w, http.StatusConflict, envOAuthErrorCredentialKeyConflict, "")
		return
	}

	if !provider.Enabled && !slices.ContainsFunc(credentials, func(credential db.VaultCredential) bool { return credential.CredentialKey == secretName }) {
		writeEnvOAuthStartError(w, http.StatusConflict, envOAuthErrorProviderUnavailable, "")
		return
	}
	flowID, err := ids.New("eof_")
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusInternalServerError, envOAuthErrorInvalidRequest, "")
		return
	}
	authorizeURL, err := buildEnvOAuthAuthorizeURL(provider.AuthorizeEndpoint, provider.ClientID, provider.Scope, redirectURL, flowID)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusInternalServerError, envOAuthErrorInvalidRequest, "")
		return
	}
	now := time.Now().UTC()
	displayName := req.DisplayName
	if displayName == "" {
		displayName = provider.Name + " OAuth"
	}
	flow := db.EnvOAuthFlow{
		UUID:                    uuid.NewV4().String(),
		ExternalID:              flowID,
		OrganizationUUID:        principal.OrganizationUUID,
		WorkspaceUUID:           workspace.UUID,
		VaultUUID:               vault.UUID,
		VaultExternalID:         vault.ExternalID,
		CredentialExternalID:    req.CredentialID,
		ProviderVersion:         provider.Version,
		ProviderKey:             provider.ProviderKey,
		SecretName:              secretName,
		DisplayName:             displayName,
		RedirectURL:             redirectURL,
		AuthorizeEndpoint:       provider.AuthorizeEndpoint,
		TokenEndpoint:           provider.TokenEndpoint,
		Scope:                   provider.Scope,
		TokenEndpointAuthMethod: provider.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   provider.TokenEndpointEncoding,
		NetworkingType:          req.Networking.Type,
		AllowedHosts:            req.Networking.AllowedHosts,
		Status:                  "pending",
		CreatedByAPIKeyUUID:     principal.APIKeyUUID,
		CreatedAt:               now,
		UpdatedAt:               now,
		ExpiresAt:               now.Add(envOAuthFlowTTL),
	}
	authorizeURL, err = s.prepareEnvOAuthPKCE(r.Context(), &flow, authorizeURL)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusServiceUnavailable, envOAuthErrorProviderUnavailable, "")
		return
	}
	if !provider.Builtin {
		envelope, err := platformoauth.SealClientField(r.Context(), s.vaultSecrets, platformoauth.FlowClientIDBinding(flow), provider.ClientID)
		if err != nil {
			writeEnvOAuthStartError(w, http.StatusServiceUnavailable, envOAuthErrorProviderUnavailable, "")
			return
		}
		flow.ClientIDEnvelope = envelope
	}
	if _, err := s.db.CreateEnvOAuthFlow(r.Context(), flow); err != nil {
		s.logger.ErrorContext(r.Context(), "create env oauth flow", "error", err)
		writeEnvOAuthStartError(w, http.StatusInternalServerError, envOAuthErrorInvalidRequest, "")
		return
	}
	httpapi.WriteJSON(w, http.StatusOK, envOAuthVaultAuthStartResponse{OAuthFlowID: flowID, AuthorizeURL: authorizeURL})
}

func (s *Server) handleEnvOAuthVaultAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{Type: "vault_oauth_complete", Status: "error", ErrorCode: envOAuthErrorInvalidRequest})
		return
	}
	flow, err := s.db.GetEnvOAuthFlow(r.Context(), state)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			s.logger.ErrorContext(r.Context(), "load env oauth flow", "error", err)
		}
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{Type: "vault_oauth_complete", Status: "error", FlowID: state, ErrorCode: envOAuthErrorInvalidRequest})
		return
	}
	switch flow.Status {
	case "completed":
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorInvalidRequest,
		})
		return
	case "processing":
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorFlowInProgress,
		})
		return
	case "failed":
		code := flow.ErrorCode
		if code == "" {
			code = envOAuthErrorInvalidRequest
		}
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: code,
		})
		return
	}
	now := time.Now().UTC()
	if now.After(flow.ExpiresAt) {
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorInvalidRequest, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorInvalidRequest,
		})
		return
	}
	if providerError := strings.TrimSpace(r.URL.Query().Get("error")); providerError != "" {
		s.logger.WarnContext(r.Context(), "env oauth callback provider error", "flow_uuid", flow.UUID, "error_code", envOAuthErrorTokenExchangeFailed)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorTokenExchangeFailed, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorTokenExchangeFailed,
		})
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorTokenExchangeFailed, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorTokenExchangeFailed,
		})
		return
	}
	provider, err := s.envOAuthProviders.ResolveEnvOAuthProvider(r.Context(), flow.ProviderKey)
	if err != nil || provider.ClientID == "" || provider.ClientSecret == "" || (provider.WorkspaceUUID != "" && provider.WorkspaceUUID != flow.WorkspaceUUID) {
		s.logger.ErrorContext(r.Context(), "resolve env oauth provider for callback", "flow_uuid", flow.UUID, "error_code", envOAuthErrorProviderUnavailable)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorProviderUnavailable, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorProviderUnavailable,
		})
		return
	}
	if flow.ProviderVersion != provider.Version || flow.ProviderVersion == 0 {
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorInvalidRequest, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID, VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorInvalidRequest})
		return
	}
	clientID := provider.ClientID
	if !vaultsapi.IsBuiltinEnvOAuthProviderKey(flow.ProviderKey) {
		clientID, err = platformoauth.OpenClientField(r.Context(), s.vaultSecrets, platformoauth.FlowClientIDBinding(flow), flow.ClientIDEnvelope)
		if err != nil {
			s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorProviderUnavailable, now)
			writeEnvOAuthCallback(w, envOAuthCallbackPayload{Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID, VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorProviderUnavailable})
			return
		}
	}
	claimed, err := s.db.ClaimEnvOAuthFlow(r.Context(), flow.ExternalID, now)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "claim env oauth flow", "flow_uuid", flow.UUID, "error", err)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorInvalidRequest,
		})
		return
	}
	if !claimed {
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorFlowInProgress,
		})
		return
	}
	token, err := s.exchangeEnvOAuthFlowCode(r.Context(), flow, code, clientID, provider.ClientSecret)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "exchange env oauth code", "flow_uuid", flow.UUID, "error_code", envOAuthErrorTokenExchangeFailed)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorTokenExchangeFailed, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorTokenExchangeFailed,
		})
		return
	}
	credentialID, err := s.saveEnvOAuthCredential(r.Context(), flow, token, time.Now().UTC())
	if err != nil {
		errorCode := envOAuthCredentialSaveErrorCode(err)
		s.logger.ErrorContext(r.Context(), "save env oauth credential", "flow_uuid", flow.UUID, "error", err)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, errorCode, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: errorCode,
		})
		return
	}
	writeEnvOAuthCallback(w, envOAuthCallbackPayload{
		Type: "vault_oauth_complete", Status: "ok", FlowID: flow.ExternalID,
		VaultID: flow.VaultExternalID, CredentialID: credentialID,
	})
}

func (s *Server) saveEnvOAuthCredential(ctx context.Context, flow db.EnvOAuthFlow, token vaultsapi.OAuthTokenEndpointResult, now time.Time) (string, error) {
	credentials, _, err := s.db.ListVaultCredentialsPage(ctx, db.ListVaultCredentialsPageParams{
		WorkspaceUUID:   flow.WorkspaceUUID,
		VaultExternalID: flow.VaultExternalID,
		Limit:           50,
	})
	if err != nil {
		return "", err
	}
	scope := token.Scope
	if scope == "" {
		scope = flow.Scope
	}
	credentialID, err := ids.New("vcrd_")
	if err != nil {
		return "", err
	}
	credential := db.VaultCredential{
		UUID:                uuid.NewV4().String(),
		ExternalID:          credentialID,
		OrganizationUUID:    flow.OrganizationUUID,
		WorkspaceUUID:       flow.WorkspaceUUID,
		VaultUUID:           flow.VaultUUID,
		VaultExternalID:     flow.VaultExternalID,
		CreatedByAPIKeyUUID: flow.CreatedByAPIKeyUUID,
		DisplayName:         flow.DisplayName,
		Metadata:            json.RawMessage(`{}`),
		AuthType:            "env_oauth",
		CredentialKey:       flow.SecretName,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	placeholder := ""
	existing := false
	for _, current := range credentials {
		if current.CredentialKey != flow.SecretName {
			continue
		}
		if !envOAuthSecretNameAvailable([]db.VaultCredential{current}, flow.SecretName, flow.ProviderKey, flow.CredentialExternalID) {
			return "", db.ErrDuplicate
		}
		existingPlaceholder, ok := vaultsapi.EnvOAuthCredentialPlaceholder(current.Auth)
		if !ok {
			return "", errors.New("existing env_oauth credential is invalid")
		}
		var publicAuth struct {
			Networking envOAuthNetworkingInput `json:"networking"`
		}
		if err := json.Unmarshal(current.Auth, &publicAuth); err != nil {
			return "", err
		}
		flow.NetworkingType = publicAuth.Networking.Type
		flow.AllowedHosts = publicAuth.Networking.AllowedHosts
		placeholder = existingPlaceholder
		credential.UUID = current.UUID
		credential.ExternalID = current.ExternalID
		credential.CreatedAt = current.CreatedAt
		credential.CreatedByAPIKeyUUID = current.CreatedByAPIKeyUUID
		credential.DisplayName = current.DisplayName
		credential.Metadata = current.Metadata
		credential.SecretVersion = current.SecretVersion
		existing = true
		break
	}
	if flow.CredentialExternalID != "" && !existing {
		return "", db.ErrNotFound
	}
	publicAuth, secretPayload, err := vaultsapi.BuildEnvOAuthCredentialPayloads(vaultsapi.EnvOAuthCredentialBuildInput{
		Provider:         flow.ProviderKey,
		SecretName:       flow.SecretName,
		Placeholder:      placeholder,
		NetworkingType:   flow.NetworkingType,
		AllowedHosts:     flow.AllowedHosts,
		AccessToken:      token.AccessToken,
		RefreshToken:     token.RefreshToken,
		Scope:            scope,
		ExpiresInSeconds: int64(token.ExpiresIn),
		Now:              now,
	})
	if err != nil {
		return "", err
	}
	credential.Auth = publicAuth
	credential.SecretPayload = secretPayload
	if err := vaultsapi.SealCredentialSecret(ctx, s.vaultSecrets, &credential); err != nil {
		return "", err
	}
	saved, err := s.db.SaveEnvOAuthCredential(ctx, flow.ExternalID, credential, existing)
	if err != nil {
		return "", err
	}
	return saved.ExternalID, nil
}

func (s *Server) authorizeEnvOAuthOrganization(w http.ResponseWriter, r *http.Request) (auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		httpapi.WriteJSON(w, http.StatusUnauthorized, envOAuthErrorResponse{ErrorCode: envOAuthErrorInvalidRequest})
		return auth.Principal{}, false
	}
	orgUUID := strings.TrimSpace(chi.URLParam(r, "orgUuid"))
	if orgUUID == "" || orgUUID != principal.OrganizationUUID {
		httpapi.WriteJSON(w, http.StatusNotFound, envOAuthErrorResponse{ErrorCode: envOAuthErrorInvalidRequest})
		return auth.Principal{}, false
	}
	return principal, true
}

func (s *Server) decodeEnvOAuthStartRequest(
	w http.ResponseWriter,
	r *http.Request,
) (*envOAuthVaultAuthStartRequest, string, string, bool) {
	req, err := httpapi.DecodeObjectBodyAs[envOAuthVaultAuthStartRequest](w, r, 1<<20)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return req, "", "", false
	}
	req.ProviderID = strings.TrimSpace(req.ProviderID)
	req.VaultID = strings.TrimSpace(req.VaultID)
	req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
	req.SecretName = strings.TrimSpace(req.SecretName)
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.RedirectURL = strings.TrimSpace(req.RedirectURL)
	if req.ProviderID == "" || req.VaultID == "" || req.WorkspaceID == "" || req.SecretName == "" || req.RedirectURL == "" || req.Networking == nil || utf8.RuneCountInString(req.DisplayName) > 255 {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return req, "", "", false
	}
	redirectURL, err := s.normalizeEnvOAuthRedirectURL(req.RedirectURL)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return req, "", "", false
	}
	secretName, err := vaultsapi.ParseEnvOAuthSecretName(req.SecretName)
	if err != nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return req, "", "", false
	}
	if err := vaultsapi.ValidateEnvOAuthNetworking(req.Networking.Type, req.Networking.AllowedHosts); err != nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return req, "", "", false
	}
	return req, redirectURL, secretName, true
}

func envOAuthSecretNameAvailable(credentials []db.VaultCredential, secretName, providerID, credentialID string) bool {
	found := credentialID == ""
	for _, credential := range credentials {
		if credential.CredentialKey != secretName {
			continue
		}
		if credential.ExternalID != credentialID || credential.AuthType != "env_oauth" {
			return false
		}
		provider, err := vaultsapi.EnvOAuthCredentialProvider(credential.Auth)
		if err != nil || provider != providerID {
			return false
		}
		found = true
	}
	return found
}

func buildEnvOAuthAuthorizeURL(endpoint, clientID, scope, redirectURL, flowID string) (string, error) {
	authorizeURL, err := url.Parse(endpoint)
	if err != nil || authorizeURL.Scheme == "" || authorizeURL.Host == "" {
		return "", errors.New("invalid authorize endpoint")
	}
	values := authorizeURL.Query()
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("redirect_uri", redirectURL)
	values.Set("state", flowID)
	if scope != "" {
		values.Set("scope", scope)
	}
	authorizeURL.RawQuery = values.Encode()
	return authorizeURL.String(), nil
}

func exchangeEnvOAuthCode(ctx context.Context, flow db.EnvOAuthFlow, code, clientID, clientSecret, verifier string) (vaultsapi.OAuthTokenEndpointResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", flow.RedirectURL)
	form.Set("client_id", clientID)
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return vaultsapi.ExchangeOAuthTokenEndpoint(ctx, envOAuthTokenHTTPClient, vaultsapi.OAuthTokenEndpointExchange{
		TokenEndpoint:           flow.TokenEndpoint,
		ClientID:                clientID,
		ClientSecret:            clientSecret,
		TokenEndpointAuthMethod: flow.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   flow.TokenEndpointEncoding,
		Form:                    form,
	})
}

func (s *Server) normalizeEnvOAuthRedirectURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(value, "#") {
		return "", errors.New("invalid url")
	}
	if parsed.Path != envOAuthRedirectPath {
		return "", errors.New("invalid redirect path")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && s.cfg.Env != config.EnvironmentProd) {
		return "", errors.New("url must use http or https")
	}
	consoleURL := strings.TrimSpace(s.cfg.Auth.ConsoleURL)
	if consoleURL == "" {
		return value, nil
	}
	origin, err := url.Parse(consoleURL)
	if err != nil {
		return "", errors.New("invalid url")
	}
	if !strings.EqualFold(parsed.Scheme, origin.Scheme) || !strings.EqualFold(parsed.Host, origin.Host) {
		return "", errors.New("url must match the console origin")
	}
	return value, nil
}

func (s *Server) failEnvOAuthFlow(ctx context.Context, flowID, errorCode string, failedAt time.Time) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.db.FailEnvOAuthFlow(cleanupCtx, flowID, errorCode, failedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.logger.ErrorContext(cleanupCtx, "fail env oauth flow", "error", err)
	}
}

func writeEnvOAuthStartError(w http.ResponseWriter, status int, errorCode, flowID string) {
	httpapi.WriteJSON(w, status, envOAuthErrorResponse{ErrorCode: errorCode, OAuthFlowID: flowID})
}

func writeEnvOAuthCallback(w http.ResponseWriter, payload envOAuthCallbackPayload) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte(`{"type":"vault_oauth_complete","status":"error","error_code":"invalid_request"}`)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>OAuth complete</title>
</head>
<body>
<script>
const payload = %s;
try {
  new BroadcastChannel("env-oauth").postMessage(payload);
} catch (error) {}
try {
  if (window.opener) window.opener.postMessage(payload, window.location.origin);
} catch (error) {}
setTimeout(() => window.close(), 250);
</script>
</body>
</html>`, payloadJSON)
}
