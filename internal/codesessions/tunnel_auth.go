package codesessions

import (
	"context"

	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

func (s *Service) AuthenticateTunnel(ctx context.Context, token, codeSessionExternalID string) (porttunnel.Identity, error) {
	claims, err := s.authorizeRuntimeCredential(ctx, token, codeSessionExternalID)
	if err != nil {
		return porttunnel.Identity{}, tunnelCredentialFailure(err)
	}
	credentialContext, err := s.db.GetCodeSessionCredentialContextForIssue(ctx, claims.OrganizationUUID, claims.WorkspaceUUID, claims.SessionID)
	if err != nil {
		return porttunnel.Identity{}, tunnelCredentialFailure(err)
	}
	return porttunnel.Identity{
		OrganizationUUID:      credentialContext.OrganizationUUID,
		WorkspaceUUID:         credentialContext.WorkspaceUUID,
		CodeSessionUUID:       credentialContext.CodeSessionUUID,
		CodeSessionExternalID: credentialContext.CodeSessionExternalID,
	}, nil
}
