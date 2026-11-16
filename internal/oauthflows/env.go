package oauthflows

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
	"net/url"
	"time"
	"uuid"
)

const envFlowTTL = 15 * time.Minute

func CreateEnv(ctx context.Context, store *db.DB, cipher *secrets.Service, provider vaults.EnvOAuthProviderSnapshot, flow db.EnvOAuthFlow) (db.EnvOAuthFlow, string, error) {
	var err error
	flow.ExternalID, err = ids.New("eof_")
	if err != nil {
		return db.EnvOAuthFlow{}, "", err
	}
	flow.UUID = uuid.NewV4().String()
	flow.ProviderVersion, flow.ProviderKey = provider.Version, provider.ProviderKey
	flow.AuthorizeEndpoint, flow.TokenEndpoint = provider.AuthorizeEndpoint, provider.TokenEndpoint
	flow.Scope = provider.Scope
	flow.TokenEndpointAuthMethod, flow.TokenEndpointEncoding = provider.TokenEndpointAuthMethod, provider.TokenEndpointEncoding
	flow.Status = "pending"
	flow.CreatedAt = time.Now().UTC()
	flow.UpdatedAt = flow.CreatedAt
	flow.ExpiresAt = flow.CreatedAt.Add(envFlowTTL)
	if flow.DisplayName == "" {
		flow.DisplayName = provider.Name + " OAuth"
	}
	link, err := EnvAuthorizeURL(provider.AuthorizeEndpoint, provider.ClientID, provider.Scope, flow.RedirectURL, flow.ExternalID)
	if err != nil {
		return db.EnvOAuthFlow{}, "", err
	}
	link, err = PrepareEnvPKCE(ctx, cipher, &flow, link)
	if err != nil {
		return db.EnvOAuthFlow{}, "", err
	}
	if !provider.Builtin {
		flow.ClientIDEnvelope, err = platformoauth.SealClientField(ctx, cipher, platformoauth.FlowClientIDBinding(flow), provider.ClientID)
		if err != nil {
			return db.EnvOAuthFlow{}, "", err
		}
	}
	flow, err = store.CreateEnvOAuthFlow(ctx, flow)
	return flow, link, err
}

func PrepareEnvPKCE(ctx context.Context, cipher *secrets.Service, flow *db.EnvOAuthFlow, authorizeURL string) (string, error) {
	verifier := rand.Text() + rand.Text()
	envelope, err := platformoauth.SealClientField(ctx, cipher, platformoauth.FlowCodeVerifierBinding(*flow), verifier)
	if err != nil {
		return "", err
	}
	target, err := url.Parse(authorizeURL)
	if err != nil {
		return "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	query := target.Query()
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	target.RawQuery = query.Encode()
	flow.CodeVerifierEnvelope = envelope
	return target.String(), nil
}

func EnvAuthorizeURL(endpoint, clientID, scope, redirectURL, flowID string) (string, error) {
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
