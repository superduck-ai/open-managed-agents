package codesessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/superduck-ai/open-managed-agents/internal/db"
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
		if errors.Is(err, db.ErrNotFound) {
			return SessionCredentialClaims{}, err
		}
		return SessionCredentialClaims{}, fmt.Errorf("%w: %w", errRuntimeCredentialUnavailable, err)
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

func (s *Service) authorizeRuntimeCredential(ctx context.Context, token, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.authenticateRuntimeCredential(ctx, token, expectedCodeSessionID)
	if errors.Is(err, errRuntimeCredentialUnavailable) {
		return SessionCredentialClaims{}, err
	}
	if err != nil {
		return SessionCredentialClaims{}, fmt.Errorf("%w: %w", errRuntimeCredentialInvalid, err)
	}
	if claims.Role == "worker" {
		if err := s.authorizeWorkerClaims(ctx, claims); err != nil {
			return SessionCredentialClaims{}, err
		}
	}
	return claims, nil
}
