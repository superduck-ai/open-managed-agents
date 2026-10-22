package codesessions

import (
	"context"
)

func (s *Service) authenticateSandboxRuntime(ctx context.Context, token, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.credentials.VerifySandbox(token)
	if err != nil {
		return SessionCredentialClaims{}, err
	}
	if expectedCodeSessionID != "" && claims.SessionID != expectedCodeSessionID {
		return SessionCredentialClaims{}, errSandboxCredentialPath
	}
	if s.db == nil {
		return SessionCredentialClaims{}, errSandboxCredentialLifecycle
	}
	if err := s.db.ValidateCodeSessionSandbox(ctx, claims.OrganizationUUID, claims.WorkspaceUUID, claims.SessionID, claims.SandboxUUID); err != nil {
		return SessionCredentialClaims{}, err
	}
	return claims, nil
}

func (s *Service) authenticateRuntimeCredential(ctx context.Context, token, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.AuthenticateSessionIngress(ctx, token, expectedCodeSessionID)
	if err == nil {
		return claims, nil
	}
	return s.authenticateSandboxRuntime(ctx, token, expectedCodeSessionID)
}
