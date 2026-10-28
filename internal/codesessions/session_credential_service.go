package codesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func (s *Service) AuthenticateSessionIngress(_ context.Context, rawToken, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.credentials.Verify(strings.TrimSpace(rawToken))
	if err != nil {
		return SessionCredentialClaims{}, err
	}
	if expectedCodeSessionID != "" && claims.SessionID != expectedCodeSessionID {
		return SessionCredentialClaims{}, errors.New("session ingress token does not match request path")
	}
	return claims, nil
}

func (s *Service) authorizeIngressIdentity(ctx context.Context, claims SessionCredentialClaims) error {
	if s.db == nil {
		return nil
	}
	identity, err := s.db.GetCodeSessionCredentialContextForIssue(ctx, claims.OrganizationUUID, claims.WorkspaceUUID, claims.SessionID)
	if errors.Is(err, db.ErrNotFound) {
		return errSessionIngressIdentity
	}
	if err != nil {
		return err
	}
	if !credentialContextMatchesClaims(identity, claims) {
		return errSessionIngressIdentity
	}
	return nil
}

func (s *Service) authorizeWorkerClaims(ctx context.Context, claims SessionCredentialClaims) error {
	if s.db == nil {
		return nil
	}
	if claims.WorkerEpoch <= 0 {
		return s.authorizeIngressIdentity(ctx, claims)
	}
	err := s.db.ValidateCodeSessionIngressWorkerEpoch(ctx, claims.OrganizationUUID, claims.WorkspaceUUID, claims.SessionID, claims.WorkerEpoch)
	if errors.Is(err, db.ErrNotFound) {
		if err := s.authorizeIngressIdentity(ctx, claims); err != nil {
			return err
		}
		return db.ErrWorkerEpochMismatch
	}
	return err
}

func (s *Service) issueSessionIngressToken(credentialContext db.CodeSessionCredentialContext, workerEpoch int64) (string, error) {
	return s.credentials.Issue(sessionCredentialIdentity(credentialContext, workerEpoch))
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
