package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"

	"github.com/go-chi/chi/v5"
)

const (
	envOAuthFlowTTL = 15 * time.Minute

	envOAuthHTTPTimeout = 15 * time.Second

	envOAuthErrorInvalidRequest        = "invalid_request"
	envOAuthErrorProviderUnavailable   = "platform_oauth_provider_unavailable"
	envOAuthErrorCredentialKeyConflict = "credential_key_conflict"
	envOAuthErrorTokenExchangeFailed   = "token_exchange_failed"
)

var envOAuthTokenHTTPClient = &http.Client{Timeout: envOAuthHTTPTimeout}

type envOAuthNetworkingInput struct {
	Type         string   `json:"type"`
	AllowedHosts []string `json:"allowed_hosts"`
}

type envOAuthVaultAuthStartRequest struct {
	ProviderID  string                   `json:"provider_id"`
	VaultID     string                   `json:"vault_id"`
	WorkspaceID string                   `json:"workspace_id"`
	SecretName  string                   `json:"secret_name"`
	DisplayName string                   `json:"display_name"`
	Networking  *envOAuthNetworkingInput `json:"networking"`
	RedirectURL string                   `json:"redirect_url"`
}

type envOAuthVaultAuthStartResponse struct {
	OAuthFlowID  string `json:"oauth_flow_id"`
	AuthorizeURL string `json:"authorize_url"`
}

type envOAuthProviderResponse struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Builtin             bool     `json:"builtin"`
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
	if _, ok := s.authorizeEnvOAuthOrganization(w, r); !ok {
		return
	}
	providers, err := s.envOAuthProviders.ListProviders(r.Context())
	if err != nil {
		s.logger.ErrorContext(r.Context(), "list env oauth providers", "error", err)
		httpapi.WriteJSON(w, http.StatusInternalServerError, envOAuthErrorResponse{ErrorCode: "internal_error"})
		return
	}
	response := make([]envOAuthProviderResponse, 0, len(providers))
	for _, provider := range providers {
		response = append(response, envOAuthProviderResponse{
			ID:                  provider.ProviderKey,
			Name:                provider.Name,
			Builtin:             provider.Builtin,
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
	req, redirectURL, secretName, ok := decodeEnvOAuthStartRequest(w, r)
	if !ok {
		return
	}

	workspaceID := req.WorkspaceID
	if workspaceID == "default" {
		workspaceID = principal.WorkspaceExternalID
	}
	workspace, err := s.db.GetAdminWorkspace(r.Context(), principal.OrganizationUUID, workspaceID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, db.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeEnvOAuthStartError(w, status, envOAuthErrorInvalidRequest, "")
		return
	}
	if workspace.ArchivedAt != nil {
		writeEnvOAuthStartError(w, http.StatusNotFound, envOAuthErrorInvalidRequest, "")
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
	if err != nil || provider.AuthorizeEndpoint == "" || provider.TokenEndpoint == "" || provider.ClientID == "" {
		status := http.StatusBadRequest
		if vaultsapi.IsBuiltinEnvOAuthProviderKey(req.ProviderID) {
			status = http.StatusServiceUnavailable
		}
		writeEnvOAuthStartError(w, status, envOAuthErrorProviderUnavailable, "")
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
	if !envOAuthSecretNameAvailable(credentials, secretName, req.ProviderID) {
		writeEnvOAuthStartError(w, http.StatusConflict, envOAuthErrorCredentialKeyConflict, "")
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
		ProviderKey:             provider.ProviderKey,
		SecretName:              secretName,
		DisplayName:             displayName,
		RedirectURL:             redirectURL,
		AuthorizeEndpoint:       provider.AuthorizeEndpoint,
		TokenEndpoint:           provider.TokenEndpoint,
		ClientID:                provider.ClientID,
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
			Type: "vault_oauth_complete", Status: "ok", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, CredentialID: flow.CredentialExternalID,
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
		s.logger.WarnContext(r.Context(), "env oauth callback provider error", "flow_external_id", flow.ExternalID, "error", providerError)
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
	if err != nil || provider.ClientID == "" || provider.ClientSecret == "" {
		s.logger.ErrorContext(r.Context(), "resolve env oauth provider for callback", "flow_external_id", flow.ExternalID, "error", err)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorProviderUnavailable, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorProviderUnavailable,
		})
		return
	}
	token, err := exchangeEnvOAuthCode(r.Context(), flow, code, provider.ClientSecret)
	if err != nil {
		s.logger.ErrorContext(r.Context(), "exchange env oauth code", "flow_external_id", flow.ExternalID, "error", err)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, envOAuthErrorTokenExchangeFailed, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: envOAuthErrorTokenExchangeFailed,
		})
		return
	}
	credentialID, err := s.saveEnvOAuthCredential(r.Context(), flow, token, now)
	if err != nil {
		errorCode := envOAuthErrorTokenExchangeFailed
		if errors.Is(err, db.ErrDuplicate) {
			errorCode = envOAuthErrorCredentialKeyConflict
		}
		s.logger.ErrorContext(r.Context(), "save env oauth credential", "flow_external_id", flow.ExternalID, "error", err)
		s.failEnvOAuthFlow(r.Context(), flow.ExternalID, errorCode, now)
		writeEnvOAuthCallback(w, envOAuthCallbackPayload{
			Type: "vault_oauth_complete", Status: "error", FlowID: flow.ExternalID,
			VaultID: flow.VaultExternalID, ErrorCode: errorCode,
		})
		return
	}
	if err := s.db.CompleteEnvOAuthFlow(r.Context(), flow.ExternalID, credentialID, now); err != nil {
		s.logger.ErrorContext(r.Context(), "complete env oauth flow", "flow_external_id", flow.ExternalID, "error", err)
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
		if current.AuthType != "env_oauth" {
			return "", db.ErrDuplicate
		}
		existingPlaceholder, ok := vaultsapi.EnvOAuthCredentialPlaceholder(current.Auth)
		if !ok {
			return "", errors.New("existing env_oauth credential is invalid")
		}
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
	if existing {
		updated, err := s.db.UpdateVaultCredential(ctx, credential.WorkspaceUUID, credential.VaultExternalID, credential.ExternalID, credential)
		if err != nil {
			return "", err
		}
		return updated.ExternalID, nil
	}
	created, err := s.db.CreateVaultCredential(ctx, credential)
	if err != nil {
		return "", err
	}
	return created.ExternalID, nil
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

func decodeEnvOAuthStartRequest(
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
	if req.ProviderID == "" || req.VaultID == "" || req.WorkspaceID == "" || req.SecretName == "" || req.RedirectURL == "" || req.Networking == nil {
		writeEnvOAuthStartError(w, http.StatusBadRequest, envOAuthErrorInvalidRequest, "")
		return req, "", "", false
	}
	redirectURL, err := normalizeEnvOAuthURL(req.RedirectURL)
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

func envOAuthSecretNameAvailable(credentials []db.VaultCredential, secretName, providerID string) bool {
	for _, credential := range credentials {
		if credential.CredentialKey != secretName {
			continue
		}
		if credential.AuthType != "env_oauth" {
			return false
		}
		provider, err := vaultsapi.EnvOAuthCredentialProvider(credential.Auth)
		if err != nil || provider != providerID {
			return false
		}
	}
	return true
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

func exchangeEnvOAuthCode(ctx context.Context, flow db.EnvOAuthFlow, code, clientSecret string) (vaultsapi.OAuthTokenEndpointResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", flow.RedirectURL)
	form.Set("client_id", flow.ClientID)
	return vaultsapi.ExchangeOAuthTokenEndpoint(ctx, envOAuthTokenHTTPClient, vaultsapi.OAuthTokenEndpointExchange{
		TokenEndpoint:           flow.TokenEndpoint,
		ClientID:                flow.ClientID,
		ClientSecret:            clientSecret,
		TokenEndpointAuthMethod: flow.TokenEndpointAuthMethod,
		TokenEndpointEncoding:   flow.TokenEndpointEncoding,
		Form:                    form,
	})
}

func normalizeEnvOAuthURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("url must use http or https")
	}
	return value, nil
}

func (s *Server) failEnvOAuthFlow(ctx context.Context, flowID, errorCode string, failedAt time.Time) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.db.FailEnvOAuthFlow(cleanupCtx, flowID, errorCode, failedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.logger.ErrorContext(cleanupCtx, "fail env oauth flow", "flow_id", flowID, "error", err)
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
