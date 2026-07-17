package codesessions

import (
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// AuthenticateSessionIngress 验证 JWT 的密码学身份并将 session_id 绑定到请求路径。
// 当前不回查 session 状态或 worker lease；worker epoch/lease 由对应 handler 的状态机处理。
func (s *Service) AuthenticateSessionIngress(rawToken, expectedCodeSessionID string) (SessionCredentialClaims, error) {
	claims, err := s.credentials.Verify(strings.TrimSpace(rawToken))
	if err != nil {
		return SessionCredentialClaims{}, err
	}
	if strings.TrimSpace(expectedCodeSessionID) != "" && claims.SessionID != strings.TrimSpace(expectedCodeSessionID) {
		return SessionCredentialClaims{}, errors.New("session ingress token does not match request path")
	}
	return claims, nil
}

func (s *Service) issueSessionIngressToken(credentialContext db.CodeSessionCredentialContext) (string, error) {
	return s.credentials.Issue(SessionCredentialIdentity{
		SessionID:        credentialContext.CodeSessionExternalID,
		PublicSessionID:  credentialContext.PublicSessionExternalID,
		AgentID:          credentialContext.AgentExternalID,
		AgentVersion:     credentialContext.AgentVersion,
		OrganizationUUID: credentialContext.OrganizationUUID,
		WorkspaceUUID:    credentialContext.WorkspaceUUID,
		AccountEmail:     credentialContext.AccountEmail,
	})
}
