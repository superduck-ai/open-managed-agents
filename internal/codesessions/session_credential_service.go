package codesessions

import (
	"context"
	"errors"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// IssueSessionIngressToken 是进程内 Service 能力，不对应公开的 token 换发接口；
// 当前主要供受控测试构造与 code session 身份一致的合法凭证。
func (s *Service) IssueSessionIngressToken(ctx context.Context, organizationID, workspaceID int64, codeSessionID string) (string, int, error) {
	credentialContext, err := s.db.GetCodeSessionCredentialContextForIssue(ctx, organizationID, workspaceID, codeSessionID)
	if err != nil {
		return "", 0, err
	}
	token, err := s.issueSessionIngressToken(credentialContext)
	if err != nil {
		return "", 0, err
	}
	// 0 表示没有独立的墙钟 expiry。
	return token, 0, nil
}

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
