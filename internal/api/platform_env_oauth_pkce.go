package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/url"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func (s *Server) prepareEnvOAuthPKCE(ctx context.Context, flow *db.EnvOAuthFlow, authorizeURL string) (string, error) {
	if flow.ProviderKey != vaultsapi.EnvOAuthProviderBuiltinFriday {
		return authorizeURL, nil
	}
	verifier := rand.Text() + rand.Text()
	envelope, err := platformoauth.SealClientField(ctx, s.vaultSecrets, platformoauth.FlowCodeVerifierBinding(*flow), verifier)
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

func (s *Server) exchangeEnvOAuthFlowCode(ctx context.Context, flow db.EnvOAuthFlow, code, clientID, clientSecret string) (vaultsapi.OAuthTokenEndpointResult, error) {
	verifier := ""
	if flow.ProviderKey == vaultsapi.EnvOAuthProviderBuiltinFriday {
		var err error
		verifier, err = platformoauth.OpenClientField(ctx, s.vaultSecrets, platformoauth.FlowCodeVerifierBinding(flow), flow.CodeVerifierEnvelope)
		if err != nil {
			return vaultsapi.OAuthTokenEndpointResult{}, err
		}
	}
	return exchangeEnvOAuthCode(ctx, flow, code, clientID, clientSecret, verifier)
}
