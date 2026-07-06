package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const (
	platformMCPVaultAuthAlreadyExists             = "already_exists"
	platformMCPVaultAuthOAuthDiscoveryFailed      = "oauth_discovery_failed"
	platformMCPVaultAuthTokenExchangeFailed       = "token_exchange_failed"
	platformMCPVaultAuthVerificationRequestFailed = "verification_request_failed"

	platformMCPVaultAuthFlowTTL = 15 * time.Minute
)

var (
	platformMCPVaultAuthHTTPClient = &http.Client{Timeout: 15 * time.Second}
	platformMCPVaultAuthParamRE    = regexp.MustCompile(`(?i)(resource_metadata|scope)=("[^"]*"|[^,\s]+)`)
)

type platformMCPVaultAuthStartRequest struct {
	MCPServerURL string `json:"mcp_server_url"`
	VaultID      string `json:"vault_id"`
	WorkspaceID  string `json:"workspace_id"`
	RedirectURL  string `json:"redirect_url"`
	DisplayName  string `json:"display_name,omitempty"`
	Source       string `json:"source,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

type platformMCPVaultAuthStartResponse struct {
	OAuthFlowID string `json:"oauth_flow_id"`
	RedirectURL string `json:"redirect_url"`
}

type platformMCPVaultAuthErrorResponse struct {
	ErrorCode   string `json:"error_code,omitempty"`
	OAuthFlowID string `json:"oauth_flow_id,omitempty"`
}

type platformMCPVaultAuthCallbackPayload struct {
	Type         string `json:"type"`
	CredentialID string `json:"credential_id,omitempty"`
	VaultID      string `json:"vault_id,omitempty"`
	ErrorCode    string `json:"error_code,omitempty"`
	FlowID       string `json:"flow_id,omitempty"`
}

type platformMCPOAuthDiscovery struct {
	Issuer                   string
	Resource                 string
	Scope                    string
	AuthorizationEndpoint    string
	TokenEndpoint            string
	RegistrationEndpoint     string
	TokenEndpointAuthMethods []string
	CodeChallengeMethods     []string
}

type platformMCPOAuthProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type platformMCPOAuthAuthorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

type platformMCPOAuthClientRegistrationResponse struct {
	ClientID                string `json:"client_id"`
	ClientSecret            string `json:"client_secret"`
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

type platformMCPOAuthTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	ExpiresIn        any    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (s *Server) handlePlatformMCPVaultAuthStart(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		writePlatformMCPVaultAuthError(w, http.StatusUnauthorized, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}
	orgUUID := strings.TrimSpace(chi.URLParam(r, "orgUuid"))
	if orgUUID == "" || (orgUUID != principal.OrganizationUUID && orgUUID != principal.OrganizationExternalID) {
		writePlatformMCPVaultAuthError(w, http.StatusNotFound, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}

	var req platformMCPVaultAuthStartRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&req); err != nil {
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}
	req.trim()
	if req.VaultID == "" || req.WorkspaceID == "" || req.RedirectURL == "" || req.MCPServerURL == "" {
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}

	mcpServerURL, err := normalizePlatformMCPVaultAuthURL(req.MCPServerURL)
	if err != nil {
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthOAuthDiscoveryFailed, "")
		return
	}
	redirectURL, err := normalizePlatformMCPVaultAuthURL(req.RedirectURL)
	if err != nil {
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}

	workspaceID := req.WorkspaceID
	if workspaceID == "default" {
		workspaceID = principal.WorkspaceExternalID
	}
	workspace, err := s.db.GetAdminWorkspace(r.Context(), principal.OrganizationID, workspaceID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writePlatformMCPVaultAuthError(w, http.StatusNotFound, platformMCPVaultAuthVerificationRequestFailed, "")
			return
		}
		log.Printf("load mcp vault auth workspace: %v", err)
		writePlatformMCPVaultAuthError(w, http.StatusInternalServerError, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}
	if workspace.ArchivedAt != nil {
		writePlatformMCPVaultAuthError(w, http.StatusNotFound, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}

	vault, err := s.db.GetVaultByExternalIDOrUUID(r.Context(), workspace.ID, req.VaultID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writePlatformMCPVaultAuthError(w, http.StatusNotFound, platformMCPVaultAuthVerificationRequestFailed, "")
			return
		}
		log.Printf("load mcp vault auth vault: %v", err)
		writePlatformMCPVaultAuthError(w, http.StatusInternalServerError, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}
	if vault.ArchivedAt != nil {
		writePlatformMCPVaultAuthError(w, http.StatusConflict, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}

	credentials, _, err := s.db.ListVaultCredentialsPage(r.Context(), db.ListVaultCredentialsPageParams{
		WorkspaceID:     workspace.ID,
		VaultExternalID: vault.ExternalID,
		Limit:           50,
		IncludeArchived: false,
	})
	if err != nil {
		log.Printf("list mcp vault auth credentials: %v", err)
		writePlatformMCPVaultAuthError(w, http.StatusInternalServerError, platformMCPVaultAuthVerificationRequestFailed, "")
		return
	}
	if platformMCPVaultCredentialExists(credentials, mcpServerURL) {
		writePlatformMCPVaultAuthError(w, http.StatusConflict, platformMCPVaultAuthAlreadyExists, "")
		return
	}

	flowID := uuid.NewString()
	discovery, err := discoverPlatformMCPOAuth(r.Context(), platformMCPVaultAuthHTTPClient, mcpServerURL)
	if err != nil {
		log.Printf("discover mcp oauth for %s: %v", mcpServerURL, err)
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthOAuthDiscoveryFailed, flowID)
		return
	}

	clientID, clientSecret, tokenAuthMethod, err := resolvePlatformMCPOAuthClient(
		r.Context(),
		platformMCPVaultAuthHTTPClient,
		discovery,
		redirectURL,
		req.DisplayName,
		req.ClientID,
		req.ClientSecret,
	)
	if err != nil {
		log.Printf("resolve mcp oauth client for %s: %v", mcpServerURL, err)
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthOAuthDiscoveryFailed, flowID)
		return
	}

	codeVerifier, codeChallenge, codeChallengeMethod, err := newPlatformMCPOAuthPKCE(discovery.CodeChallengeMethods)
	if err != nil {
		log.Printf("create mcp oauth pkce for %s: %v", mcpServerURL, err)
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthOAuthDiscoveryFailed, flowID)
		return
	}
	authorizeURL, err := buildPlatformMCPVaultAuthorizeURL(discovery, redirectURL, flowID, clientID, codeChallenge, codeChallengeMethod)
	if err != nil {
		writePlatformMCPVaultAuthError(w, http.StatusBadRequest, platformMCPVaultAuthOAuthDiscoveryFailed, flowID)
		return
	}

	now := time.Now().UTC()
	displayName := req.DisplayName
	if displayName == "" {
		displayName = defaultPlatformMCPVaultCredentialName(mcpServerURL)
	}
	if _, err := s.db.CreateMCPOAuthFlow(r.Context(), db.MCPOAuthFlow{
		UUID:                      uuid.NewString(),
		ExternalID:                flowID,
		OrganizationID:            principal.OrganizationID,
		WorkspaceID:               workspace.ID,
		VaultID:                   vault.ID,
		VaultExternalID:           vault.ExternalID,
		UserID:                    principal.UserID,
		UserExternalID:            principal.UserExternalID,
		PlatformSessionExternalID: principal.PlatformSessionExternalID,
		MCPServerURL:              mcpServerURL,
		RedirectURL:               redirectURL,
		DisplayName:               displayName,
		Source:                    req.Source,
		AuthorizationEndpoint:     discovery.AuthorizationEndpoint,
		TokenEndpoint:             discovery.TokenEndpoint,
		RegistrationEndpoint:      discovery.RegistrationEndpoint,
		Issuer:                    discovery.Issuer,
		Resource:                  discovery.Resource,
		Scope:                     discovery.Scope,
		ClientID:                  clientID,
		ClientSecret:              clientSecret,
		TokenEndpointAuthMethod:   tokenAuthMethod,
		CodeVerifier:              codeVerifier,
		CodeChallengeMethod:       codeChallengeMethod,
		Status:                    "pending",
		CreatedAt:                 now,
		UpdatedAt:                 now,
		ExpiresAt:                 now.Add(platformMCPVaultAuthFlowTTL),
	}); err != nil {
		log.Printf("create mcp oauth flow: %v", err)
		writePlatformMCPVaultAuthError(w, http.StatusInternalServerError, platformMCPVaultAuthVerificationRequestFailed, flowID)
		return
	}

	httpapi.WriteJSON(w, http.StatusOK, platformMCPVaultAuthStartResponse{
		OAuthFlowID: flowID,
		RedirectURL: authorizeURL,
	})
}

func (s *Server) handlePlatformMCPVaultAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if state == "" {
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			ErrorCode: platformMCPVaultAuthVerificationRequestFailed,
		})
		return
	}

	flow, err := s.db.GetMCPOAuthFlow(r.Context(), state)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			log.Printf("load mcp oauth callback flow: %v", err)
		}
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    state,
			ErrorCode: platformMCPVaultAuthVerificationRequestFailed,
		})
		return
	}
	if flow.Status == "completed" {
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:         "vault_oauth_complete",
			FlowID:       flow.ExternalID,
			VaultID:      flow.VaultExternalID,
			CredentialID: flow.CredentialExternalID,
		})
		return
	}
	if flow.Status == "failed" {
		errorCode := flow.ErrorCode
		if errorCode == "" {
			errorCode = platformMCPVaultAuthVerificationRequestFailed
		}
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: errorCode,
		})
		return
	}
	now := time.Now().UTC()
	if now.After(flow.ExpiresAt) {
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, platformMCPVaultAuthVerificationRequestFailed, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: platformMCPVaultAuthVerificationRequestFailed,
		})
		return
	}

	if providerError := strings.TrimSpace(r.URL.Query().Get("error")); providerError != "" {
		log.Printf("mcp oauth callback provider error for flow %s: %s", flow.ExternalID, providerError)
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, platformMCPVaultAuthTokenExchangeFailed, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: platformMCPVaultAuthTokenExchangeFailed,
		})
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, platformMCPVaultAuthTokenExchangeFailed, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: platformMCPVaultAuthTokenExchangeFailed,
		})
		return
	}

	token, err := exchangePlatformMCPOAuthCode(r.Context(), platformMCPVaultAuthHTTPClient, flow, code)
	if err != nil {
		log.Printf("exchange mcp oauth code for flow %s: %v", flow.ExternalID, err)
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, platformMCPVaultAuthTokenExchangeFailed, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: platformMCPVaultAuthTokenExchangeFailed,
		})
		return
	}

	publicAuth, secretPayload, err := buildPlatformMCPVaultOAuthCredentialPayloads(flow, token, now)
	if err != nil {
		log.Printf("build mcp oauth credential payload for flow %s: %v", flow.ExternalID, err)
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, platformMCPVaultAuthVerificationRequestFailed, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: platformMCPVaultAuthVerificationRequestFailed,
		})
		return
	}

	credentialID, err := ids.New("vcrd_")
	if err != nil {
		log.Printf("generate mcp oauth credential id: %v", err)
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, platformMCPVaultAuthVerificationRequestFailed, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: platformMCPVaultAuthVerificationRequestFailed,
		})
		return
	}
	metadata, err := platformMCPVaultOAuthCredentialMetadata(flow)
	if err != nil {
		log.Printf("build mcp oauth credential metadata for flow %s: %v", flow.ExternalID, err)
		metadata = json.RawMessage(`{}`)
	}
	created, err := s.db.CreateVaultCredential(r.Context(), db.VaultCredential{
		UUID:              uuid.NewString(),
		ExternalID:        credentialID,
		OrganizationID:    flow.OrganizationID,
		WorkspaceID:       flow.WorkspaceID,
		VaultID:           flow.VaultID,
		VaultExternalID:   flow.VaultExternalID,
		CreatedByAPIKeyID: 0,
		DisplayName:       flow.DisplayName,
		Metadata:          metadata,
		AuthType:          "mcp_oauth",
		CredentialKey:     flow.MCPServerURL,
		Auth:              publicAuth,
		SecretPayload:     secretPayload,
		CreatedAt:         now,
		UpdatedAt:         now,
	})
	if err != nil {
		errorCode := platformMCPVaultAuthVerificationRequestFailed
		if errors.Is(err, db.ErrDuplicate) {
			errorCode = platformMCPVaultAuthAlreadyExists
		} else if !errors.Is(err, db.ErrNotFound) && !errors.Is(err, db.ErrLimitExceeded) {
			log.Printf("create mcp oauth vault credential for flow %s: %v", flow.ExternalID, err)
		}
		s.failPlatformMCPVaultAuthFlow(r.Context(), flow.ExternalID, errorCode, now)
		writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
			Type:      "vault_oauth_complete",
			FlowID:    flow.ExternalID,
			VaultID:   flow.VaultExternalID,
			ErrorCode: errorCode,
		})
		return
	}
	if err := s.db.CompleteMCPOAuthFlow(r.Context(), flow.ExternalID, created.ExternalID, now); err != nil {
		log.Printf("complete mcp oauth flow %s after credential %s: %v", flow.ExternalID, created.ExternalID, err)
	}
	writePlatformMCPVaultAuthCallback(w, platformMCPVaultAuthCallbackPayload{
		Type:         "vault_oauth_complete",
		FlowID:       flow.ExternalID,
		VaultID:      flow.VaultExternalID,
		CredentialID: created.ExternalID,
	})
}

func (r *platformMCPVaultAuthStartRequest) trim() {
	r.MCPServerURL = strings.TrimSpace(r.MCPServerURL)
	r.VaultID = strings.TrimSpace(r.VaultID)
	r.WorkspaceID = strings.TrimSpace(r.WorkspaceID)
	r.RedirectURL = strings.TrimSpace(r.RedirectURL)
	r.DisplayName = strings.TrimSpace(r.DisplayName)
	r.Source = strings.TrimSpace(r.Source)
	r.ClientID = strings.TrimSpace(r.ClientID)
	r.ClientSecret = strings.TrimSpace(r.ClientSecret)
}

func normalizePlatformMCPVaultAuthURL(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("invalid url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("url must use http or https")
	}
	return trimmed, nil
}

func platformMCPVaultCredentialExists(credentials []db.VaultCredential, mcpServerURL string) bool {
	for _, credential := range credentials {
		if credential.CredentialKey != mcpServerURL {
			continue
		}
		if credential.AuthType == "mcp_oauth" || credential.AuthType == "static_bearer" {
			return true
		}
	}
	return false
}

func discoverPlatformMCPOAuth(ctx context.Context, client *http.Client, mcpServerURL string) (platformMCPOAuthDiscovery, error) {
	challengeParams, _ := fetchPlatformMCPWWWAuthenticateParams(ctx, client, mcpServerURL)
	if metadataURL := strings.TrimSpace(challengeParams["resource_metadata"]); metadataURL != "" {
		if discovery, err := discoverPlatformMCPOAuthFromProtectedResource(ctx, client, metadataURL, mcpServerURL, challengeParams["scope"]); err == nil {
			return discovery, nil
		} else {
			log.Printf("fetch mcp oauth protected resource metadata %s: %v", metadataURL, err)
		}
	}

	var lastErr error
	for _, metadataURL := range platformMCPProtectedResourceMetadataURLs(mcpServerURL) {
		discovery, err := discoverPlatformMCPOAuthFromProtectedResource(ctx, client, metadataURL, mcpServerURL, challengeParams["scope"])
		if err == nil {
			return discovery, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no protected resource metadata URLs")
	}
	return platformMCPOAuthDiscovery{}, lastErr
}

func fetchPlatformMCPWWWAuthenticateParams(ctx context.Context, client *http.Client, mcpServerURL string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mcpServerURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return parsePlatformMCPWWWAuthenticate(resp.Header.Values("WWW-Authenticate")), nil
}

func parsePlatformMCPWWWAuthenticate(values []string) map[string]string {
	params := map[string]string{}
	for _, value := range values {
		for _, match := range platformMCPVaultAuthParamRE.FindAllStringSubmatch(value, -1) {
			if len(match) < 3 {
				continue
			}
			key := strings.ToLower(match[1])
			rawValue := strings.TrimSpace(match[2])
			if strings.HasPrefix(rawValue, `"`) && strings.HasSuffix(rawValue, `"`) {
				if unquoted, err := strconv.Unquote(rawValue); err == nil {
					rawValue = unquoted
				} else {
					rawValue = strings.Trim(rawValue, `"`)
				}
			}
			if rawValue != "" {
				params[key] = rawValue
			}
		}
	}
	return params
}

func discoverPlatformMCPOAuthFromProtectedResource(ctx context.Context, client *http.Client, metadataURL, mcpServerURL, challengeScope string) (platformMCPOAuthDiscovery, error) {
	var prm platformMCPOAuthProtectedResourceMetadata
	if err := getPlatformMCPJSON(ctx, client, metadataURL, &prm); err != nil {
		return platformMCPOAuthDiscovery{}, err
	}
	if len(prm.AuthorizationServers) == 0 {
		return platformMCPOAuthDiscovery{}, errors.New("protected resource metadata has no authorization servers")
	}
	resource := strings.TrimSpace(prm.Resource)
	if resource == "" {
		resource = mcpServerURL
	}

	var lastErr error
	for _, issuer := range prm.AuthorizationServers {
		metadata, err := fetchPlatformMCPAuthorizationServerMetadata(ctx, client, issuer)
		if err != nil {
			lastErr = err
			continue
		}
		scope := strings.TrimSpace(challengeScope)
		if scope == "" && len(prm.ScopesSupported) == 1 {
			scope = prm.ScopesSupported[0]
		}
		return platformMCPOAuthDiscovery{
			Issuer:                   firstNonEmpty(metadata.Issuer, issuer),
			Resource:                 resource,
			Scope:                    scope,
			AuthorizationEndpoint:    metadata.AuthorizationEndpoint,
			TokenEndpoint:            metadata.TokenEndpoint,
			RegistrationEndpoint:     metadata.RegistrationEndpoint,
			TokenEndpointAuthMethods: metadata.TokenEndpointAuthMethodsSupported,
			CodeChallengeMethods:     metadata.CodeChallengeMethodsSupported,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no authorization server metadata")
	}
	return platformMCPOAuthDiscovery{}, lastErr
}

func fetchPlatformMCPAuthorizationServerMetadata(ctx context.Context, client *http.Client, issuer string) (platformMCPOAuthAuthorizationServerMetadata, error) {
	var lastErr error
	for _, metadataURL := range platformMCPAuthorizationServerMetadataURLs(issuer) {
		var metadata platformMCPOAuthAuthorizationServerMetadata
		if err := getPlatformMCPJSON(ctx, client, metadataURL, &metadata); err != nil {
			lastErr = err
			continue
		}
		if metadata.AuthorizationEndpoint == "" || metadata.TokenEndpoint == "" {
			lastErr = fmt.Errorf("authorization server metadata %s missing endpoints", metadataURL)
			continue
		}
		return metadata, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no authorization server metadata URLs")
	}
	return platformMCPOAuthAuthorizationServerMetadata{}, lastErr
}

func getPlatformMCPJSON(ctx context.Context, client *http.Client, targetURL string, out any) error {
	if _, err := normalizePlatformMCPVaultAuthURL(targetURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s status %d", targetURL, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func platformMCPProtectedResourceMetadataURLs(mcpServerURL string) []string {
	parsed, err := url.Parse(mcpServerURL)
	if err != nil {
		return nil
	}
	var urls []string
	cleanPath := strings.TrimRight(parsed.Path, "/")
	if cleanPath != "" {
		endpoint := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/.well-known/oauth-protected-resource" + cleanPath}
		urls = append(urls, endpoint.String())
	}
	root := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/.well-known/oauth-protected-resource"}
	urls = append(urls, root.String())
	return dedupeStrings(urls)
}

func platformMCPAuthorizationServerMetadataURLs(issuer string) []string {
	parsed, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	basePath := strings.TrimRight(parsed.Path, "/")
	var urls []string
	if strings.Contains(basePath, "/.well-known/") {
		urls = append(urls, parsed.String())
	}
	oauth := *parsed
	oauth.Path = "/.well-known/oauth-authorization-server" + basePath
	oauth.RawQuery = ""
	oauth.Fragment = ""
	urls = append(urls, oauth.String())
	oidc := *parsed
	oidc.Path = "/.well-known/openid-configuration" + basePath
	oidc.RawQuery = ""
	oidc.Fragment = ""
	urls = append(urls, oidc.String())
	if basePath != "" {
		oauthSuffix := *parsed
		oauthSuffix.Path = basePath + "/.well-known/oauth-authorization-server"
		oauthSuffix.RawQuery = ""
		oauthSuffix.Fragment = ""
		urls = append(urls, oauthSuffix.String())
		oidcSuffix := *parsed
		oidcSuffix.Path = basePath + "/.well-known/openid-configuration"
		oidcSuffix.RawQuery = ""
		oidcSuffix.Fragment = ""
		urls = append(urls, oidcSuffix.String())
	}
	return dedupeStrings(urls)
}

func resolvePlatformMCPOAuthClient(ctx context.Context, client *http.Client, discovery platformMCPOAuthDiscovery, redirectURL, displayName, clientID, clientSecret string) (string, string, string, error) {
	if clientID != "" {
		tokenAuthMethod, err := choosePlatformMCPTokenEndpointAuthMethod(discovery.TokenEndpointAuthMethods, clientSecret != "")
		if err != nil {
			return "", "", "", err
		}
		return clientID, clientSecret, tokenAuthMethod, nil
	}
	if discovery.RegistrationEndpoint == "" {
		return "", "", "", errors.New("authorization server has no dynamic registration endpoint")
	}
	return registerPlatformMCPOAuthClient(ctx, client, discovery, redirectURL, displayName)
}

func registerPlatformMCPOAuthClient(ctx context.Context, client *http.Client, discovery platformMCPOAuthDiscovery, redirectURL, displayName string) (string, string, string, error) {
	clientName := strings.TrimSpace(displayName)
	if clientName == "" {
		clientName = "Claude API Server"
	}
	requestedAuthMethod, err := choosePlatformMCPRegistrationTokenEndpointAuthMethod(discovery.TokenEndpointAuthMethods)
	if err != nil {
		return "", "", "", err
	}
	payload := map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURL},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": requestedAuthMethod,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, discovery.RegistrationEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("dynamic client registration status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var registered platformMCPOAuthClientRegistrationResponse
	if err := json.Unmarshal(respBody, &registered); err != nil {
		return "", "", "", err
	}
	registered.ClientID = strings.TrimSpace(registered.ClientID)
	if registered.ClientID == "" {
		return "", "", "", errors.New("dynamic client registration returned no client_id")
	}
	tokenAuthMethod := strings.TrimSpace(registered.TokenEndpointAuthMethod)
	if tokenAuthMethod == "" {
		tokenAuthMethod = requestedAuthMethod
	}
	if (tokenAuthMethod == "client_secret_basic" || tokenAuthMethod == "client_secret_post") && registered.ClientSecret == "" {
		return "", "", "", errors.New("dynamic client registration returned no client_secret")
	}
	return registered.ClientID, registered.ClientSecret, tokenAuthMethod, nil
}

func choosePlatformMCPRegistrationTokenEndpointAuthMethod(methods []string) (string, error) {
	normalized := normalizeStringSet(methods)
	if len(normalized) == 0 || normalized["none"] {
		return "none", nil
	}
	if normalized["client_secret_basic"] {
		return "client_secret_basic", nil
	}
	if normalized["client_secret_post"] {
		return "client_secret_post", nil
	}
	return "", errors.New("unsupported dynamic registration token endpoint auth method")
}

func choosePlatformMCPTokenEndpointAuthMethod(methods []string, hasSecret bool) (string, error) {
	normalized := normalizeStringSet(methods)
	if !hasSecret {
		if len(normalized) == 0 || normalized["none"] {
			return "none", nil
		}
		return "", errors.New("authorization server requires client authentication")
	}
	if len(normalized) == 0 || normalized["client_secret_basic"] {
		return "client_secret_basic", nil
	}
	if normalized["client_secret_post"] {
		return "client_secret_post", nil
	}
	if normalized["none"] {
		return "none", nil
	}
	return "", errors.New("unsupported token endpoint auth method")
}

func newPlatformMCPOAuthPKCE(methods []string) (string, string, string, error) {
	var randomBytes [32]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", "", "", err
	}
	verifier := base64.RawURLEncoding.EncodeToString(randomBytes[:])
	methodSet := normalizeStringSet(methods)
	if len(methodSet) == 0 || methodSet["S256"] || methodSet["s256"] {
		sum := sha256.Sum256([]byte(verifier))
		return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), "S256", nil
	}
	if methodSet["plain"] {
		return verifier, verifier, "plain", nil
	}
	return "", "", "", errors.New("authorization server does not support S256 or plain PKCE")
}

func buildPlatformMCPVaultAuthorizeURL(discovery platformMCPOAuthDiscovery, redirectURL, flowID, clientID, codeChallenge, codeChallengeMethod string) (string, error) {
	authorizeURL, err := url.Parse(discovery.AuthorizationEndpoint)
	if err != nil || authorizeURL.Scheme == "" || authorizeURL.Host == "" {
		return "", errors.New("invalid authorization endpoint")
	}
	values := authorizeURL.Query()
	values.Set("response_type", "code")
	values.Set("redirect_uri", redirectURL)
	values.Set("state", flowID)
	values.Set("client_id", clientID)
	values.Set("code_challenge", codeChallenge)
	values.Set("code_challenge_method", codeChallengeMethod)
	if discovery.Resource != "" {
		values.Set("resource", discovery.Resource)
	}
	if discovery.Scope != "" {
		values.Set("scope", discovery.Scope)
	}
	authorizeURL.RawQuery = values.Encode()
	return authorizeURL.String(), nil
}

func exchangePlatformMCPOAuthCode(ctx context.Context, client *http.Client, flow db.MCPOAuthFlow, code string) (platformMCPOAuthTokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", flow.RedirectURL)
	values.Set("client_id", flow.ClientID)
	values.Set("code_verifier", flow.CodeVerifier)
	if flow.Resource != "" {
		values.Set("resource", flow.Resource)
	}
	authMethod := strings.TrimSpace(flow.TokenEndpointAuthMethod)
	switch authMethod {
	case "", "none":
	case "client_secret_basic":
		if flow.ClientSecret == "" {
			return platformMCPOAuthTokenResponse{}, errors.New("client_secret_basic selected without client secret")
		}
	case "client_secret_post":
		if flow.ClientSecret == "" {
			return platformMCPOAuthTokenResponse{}, errors.New("client_secret_post selected without client secret")
		}
		values.Set("client_secret", flow.ClientSecret)
	default:
		return platformMCPOAuthTokenResponse{}, fmt.Errorf("unsupported token auth method %q", authMethod)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, flow.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return platformMCPOAuthTokenResponse{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if authMethod == "client_secret_basic" {
		basic := url.QueryEscape(flow.ClientID) + ":" + url.QueryEscape(flow.ClientSecret)
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(basic)))
	}
	resp, err := client.Do(req)
	if err != nil {
		return platformMCPOAuthTokenResponse{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return platformMCPOAuthTokenResponse{}, err
	}
	var token platformMCPOAuthTokenResponse
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &token)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if token.Error != "" {
			return platformMCPOAuthTokenResponse{}, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, token.Error)
		}
		return platformMCPOAuthTokenResponse{}, fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if token.AccessToken == "" {
		return platformMCPOAuthTokenResponse{}, errors.New("token endpoint returned no access_token")
	}
	return token, nil
}

func buildPlatformMCPVaultOAuthCredentialPayloads(flow db.MCPOAuthFlow, token platformMCPOAuthTokenResponse, now time.Time) (json.RawMessage, json.RawMessage, error) {
	publicAuth := map[string]any{
		"type":           "mcp_oauth",
		"mcp_server_url": flow.MCPServerURL,
	}
	if expiresIn := parsePlatformMCPOAuthExpiresIn(token.ExpiresIn); expiresIn > 0 {
		publicAuth["expires_at"] = now.Add(time.Duration(expiresIn) * time.Second).UTC().Format(time.RFC3339)
	}
	secretPayload := map[string]any{
		"type":         "mcp_oauth",
		"access_token": token.AccessToken,
	}
	if token.RefreshToken != "" {
		scope := firstNonEmpty(strings.TrimSpace(token.Scope), flow.Scope)
		publicRefresh := map[string]any{
			"token_endpoint":      flow.TokenEndpoint,
			"client_id":           flow.ClientID,
			"token_endpoint_auth": platformMCPVaultPublicTokenEndpointAuth(flow.TokenEndpointAuthMethod),
		}
		if scope != "" {
			publicRefresh["scope"] = scope
		}
		if flow.Resource != "" {
			publicRefresh["resource"] = flow.Resource
		}
		publicAuth["refresh"] = publicRefresh
		secretPayload["refresh"] = map[string]any{
			"refresh_token":       token.RefreshToken,
			"token_endpoint_auth": platformMCPVaultSecretTokenEndpointAuth(flow.TokenEndpointAuthMethod, flow.ClientSecret),
		}
	}
	publicJSON, err := json.Marshal(publicAuth)
	if err != nil {
		return nil, nil, err
	}
	secretJSON, err := json.Marshal(secretPayload)
	if err != nil {
		return nil, nil, err
	}
	return copyJSONRaw(publicJSON), copyJSONRaw(secretJSON), nil
}

func platformMCPVaultPublicTokenEndpointAuth(method string) map[string]any {
	method = strings.TrimSpace(method)
	if method == "" {
		method = "none"
	}
	return map[string]any{"type": method}
}

func platformMCPVaultSecretTokenEndpointAuth(method, clientSecret string) map[string]any {
	method = strings.TrimSpace(method)
	if method == "" {
		method = "none"
	}
	auth := map[string]any{"type": method}
	if (method == "client_secret_basic" || method == "client_secret_post") && clientSecret != "" {
		auth["client_secret"] = clientSecret
	}
	return auth
}

func parsePlatformMCPOAuthExpiresIn(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func platformMCPVaultOAuthCredentialMetadata(flow db.MCPOAuthFlow) (json.RawMessage, error) {
	metadata := map[string]string{
		"oauth_flow_id": flow.ExternalID,
	}
	if flow.Source != "" {
		metadata["source"] = flow.Source
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	return copyJSONRaw(raw), nil
}

func defaultPlatformMCPVaultCredentialName(mcpServerURL string) string {
	parsed, err := url.Parse(mcpServerURL)
	if err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return "MCP OAuth"
}

func (s *Server) failPlatformMCPVaultAuthFlow(ctx context.Context, flowID, errorCode string, failedAt time.Time) {
	if err := s.db.FailMCPOAuthFlow(ctx, flowID, errorCode, failedAt); err != nil && !errors.Is(err, db.ErrNotFound) {
		log.Printf("fail mcp oauth flow %s: %v", flowID, err)
	}
}

func writePlatformMCPVaultAuthError(w http.ResponseWriter, status int, errorCode, flowID string) {
	httpapi.WriteJSON(w, status, platformMCPVaultAuthErrorResponse{
		ErrorCode:   errorCode,
		OAuthFlowID: flowID,
	})
}

func writePlatformMCPVaultAuthCallback(w http.ResponseWriter, payload platformMCPVaultAuthCallbackPayload) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte(`{"type":"vault_oauth_complete","error_code":"verification_request_failed"}`)
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
  new BroadcastChannel("vault-oauth").postMessage(payload);
} catch (error) {}
try {
  if (window.opener) window.opener.postMessage(payload, window.location.origin);
} catch (error) {}
setTimeout(() => window.close(), 250);
</script>
</body>
</html>`, payloadJSON)
}

func normalizeStringSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
			set[strings.ToLower(value)] = true
		}
	}
	return set
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	var deduped []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		deduped = append(deduped, value)
	}
	return deduped
}

func copyJSONRaw(raw []byte) json.RawMessage {
	if raw == nil {
		return nil
	}
	copied := make([]byte, len(raw))
	copy(copied, raw)
	return copied
}
