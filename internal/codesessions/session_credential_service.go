package codesessions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// AuthenticateSessionIngress verifies the signed identity, binds session_id to
// the request path, and checks a claimed worker epoch against the active Code
// Session. Legacy credentials without worker_epoch remain signature-only and
// are fenced during worker registration after a credential rotation.
func (s *Service) AuthenticateSessionIngress(ctx context.Context, rawToken, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.credentials.Verify(strings.TrimSpace(rawToken))
	if err != nil {
		return SessionCredentialClaims{}, err
	}
	if strings.TrimSpace(expectedCodeSessionID) != "" && claims.SessionID != strings.TrimSpace(expectedCodeSessionID) {
		return SessionCredentialClaims{}, errors.New("session ingress token does not match request path")
	}
	if s.db != nil && claims.WorkerEpoch > 0 {
		if err := s.db.ValidateCodeSessionIngressWorkerEpoch(
			ctx,
			claims.OrganizationUUID,
			claims.WorkspaceUUID,
			claims.SessionID,
			claims.WorkerEpoch,
		); err != nil {
			return SessionCredentialClaims{}, fmt.Errorf("session ingress worker epoch is no longer active: %w", err)
		}
	}
	return claims, nil
}

// AuthenticateMCPProxy verifies the narrow MCP gateway capability and fences it
// with the active worker epoch just like other sandbox-scoped credentials.
func (s *Service) AuthenticateMCPProxy(ctx context.Context, rawToken, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.credentials.VerifyMCPProxy(strings.TrimSpace(rawToken))
	if err != nil {
		return SessionCredentialClaims{}, err
	}
	if strings.TrimSpace(expectedCodeSessionID) != "" && claims.SessionID != strings.TrimSpace(expectedCodeSessionID) {
		return SessionCredentialClaims{}, errors.New("MCP proxy token does not match request path")
	}
	if s.db != nil && claims.WorkerEpoch > 0 {
		if err := s.db.ValidateCodeSessionIngressWorkerEpoch(
			ctx,
			claims.OrganizationUUID,
			claims.WorkspaceUUID,
			claims.SessionID,
			claims.WorkerEpoch,
		); err != nil {
			return SessionCredentialClaims{}, fmt.Errorf("MCP proxy worker epoch is no longer active: %w", err)
		}
	}
	return claims, nil
}

func (s *Service) issueSessionIngressToken(credentialContext db.CodeSessionCredentialContext, workerEpoch int64) (string, error) {
	return s.credentials.Issue(sessionCredentialIdentity(credentialContext, workerEpoch))
}

func (s *Service) issueMCPProxyToken(credentialContext db.CodeSessionCredentialContext, workerEpoch int64) (string, error) {
	return s.credentials.IssueMCPProxy(sessionCredentialIdentity(credentialContext, workerEpoch))
}

func sessionCredentialIdentity(credentialContext db.CodeSessionCredentialContext, workerEpoch int64) SessionCredentialIdentity {
	return SessionCredentialIdentity{
		SessionID:        credentialContext.CodeSessionExternalID,
		PublicSessionID:  credentialContext.PublicSessionExternalID,
		AgentID:          credentialContext.AgentExternalID,
		AgentVersion:     credentialContext.AgentVersion,
		OrganizationUUID: credentialContext.OrganizationUUID,
		WorkspaceUUID:    credentialContext.WorkspaceUUID,
		AccountEmail:     credentialContext.AccountEmail,
		WorkerEpoch:      workerEpoch,
	}
}
