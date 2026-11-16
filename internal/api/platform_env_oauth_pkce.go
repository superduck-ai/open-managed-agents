package api

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func (s *Server) exchangeEnvOAuthFlowCode(ctx context.Context, flow db.EnvOAuthFlow, code, clientID, clientSecret string) (vaultsapi.OAuthTokenEndpointResult, error) {
	verifier := ""
	if flow.CodeVerifierEnvelope != nil {
		var err error
		verifier, err = platformoauth.OpenClientField(ctx, s.vaultSecrets, platformoauth.FlowCodeVerifierBinding(flow), flow.CodeVerifierEnvelope)
		if err != nil {
			return vaultsapi.OAuthTokenEndpointResult{}, err
		}
	}
	return exchangeEnvOAuthCode(ctx, flow, code, clientID, clientSecret, verifier)
}
