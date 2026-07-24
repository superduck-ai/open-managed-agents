package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	filestoreapi "github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (s *Server) authenticateService(r *http.Request) (auth.Principal, *httpapi.Error) {
	apiKey := auth.ExtractAPIKey(r)
	if apiKey == "" {
		return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key")
	}
	principal, found, apiErr := s.authenticateWorkspaceAPIKey(r, apiKey)
	if found || apiErr != nil {
		return principal, apiErr
	}
	return s.authenticateScopedServiceCredential(r, apiKey)
}

// authenticateFilestore 是 Filestore 命名空间的唯一鉴权入口。
// 该资源只接受专用 Bearer JWT，既不复用 workspace API key，也不回退到 code-session 凭证。
func (s *Server) authenticateFilestore(r *http.Request) (filestoreapi.Principal, *httpapi.Error) {
	rawToken := auth.ExtractBearerToken(r)
	if rawToken == "" {
		return filestoreapi.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing bearer token")
	}
	return s.authenticateFilestoreToken(r, rawToken)
}

func (s *Server) authenticateWorkspaceAPIKey(r *http.Request, apiKey string) (auth.Principal, bool, *httpapi.Error) {
	// workspace API key 始终优先；只有查不到普通 key 时，才尝试用途受限的凭证。
	// 这样 OAuth-compatible token 不会意外获得 workspace API key 的完整权限。
	if s.db == nil {
		return auth.Principal{}, false, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	key, err := s.db.GetAPIKey(r.Context(), auth.HashAPIKey(apiKey))
	if errors.Is(err, db.ErrNotFound) {
		return auth.Principal{}, false, nil
	}
	if err != nil {
		log.Printf("authenticate api key: %v", err)
		return auth.Principal{}, false, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	return auth.Principal{
		CredentialType:         auth.CredentialTypeAPIKey,
		APIKeyID:               key.ID,
		APIKeyExternalID:       key.ExternalID,
		OrganizationID:         key.OrganizationID,
		OrganizationExternalID: key.OrganizationExternalID,
		WorkspaceID:            key.WorkspaceID,
		WorkspaceUUID:          key.WorkspaceUUID,
		WorkspaceExternalID:    key.WorkspaceExternalID,
	}, true, nil
}

func (s *Server) authenticateScopedServiceCredential(r *http.Request, apiKey string) (auth.Principal, *httpapi.Error) {
	if isEnvironmentCredentialPath(r.URL.Path) {
		principal, found, apiErr := s.authenticateEnvironmentCredential(r, apiKey)
		if found || apiErr != nil {
			return principal, apiErr
		}
	}
	if r.Method == http.MethodPost && isMessagesPath(r.URL.Path) {
		return s.authenticateCodeSessionMessagesCredential(r, apiKey)
	}
	return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid API key")
}

func (s *Server) authenticateEnvironmentCredential(r *http.Request, apiKey string) (auth.Principal, bool, *httpapi.Error) {
	// found 用来区分“不是 environment key”和“查询失败”，前者允许继续尝试其他受限凭证。
	envKey, err := s.db.GetEnvironmentKey(r.Context(), auth.HashAPIKey(apiKey))
	if err == nil {
		return auth.Principal{
			CredentialType:         auth.CredentialTypeEnvironmentKey,
			EnvironmentKeyID:       envKey.ID,
			OrganizationID:         envKey.OrganizationID,
			OrganizationExternalID: envKey.OrganizationExternalID,
			WorkspaceID:            envKey.WorkspaceID,
			WorkspaceUUID:          envKey.WorkspaceUUID,
			WorkspaceExternalID:    envKey.WorkspaceExternalID,
			EnvironmentID:          envKey.EnvironmentID,
			EnvironmentExternalID:  envKey.EnvironmentExternalID,
		}, true, nil
	}
	if errors.Is(err, db.ErrNotFound) {
		return auth.Principal{}, false, nil
	}
	log.Printf("authenticate environment key: %v", err)
	return auth.Principal{}, false, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
}

func (s *Server) authenticateCodeSessionMessagesCredential(r *http.Request, apiKey string) (auth.Principal, *httpapi.Error) {
	// 数据库查询同时要求 session/code session 未终止，且 CCR worker lease 仍有效。
	// 下游 Messages handler 不读取 body，也能从 Principal 获得审计上下文。
	codeSession, err := s.db.GetCodeSessionByOAuthAccessTokenHash(r.Context(), auth.HashAPIKey(apiKey))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid API key")
		}
		log.Printf("authenticate code session OAuth-compatible credential: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	return principalFromCodeSessionCredential(codeSession, auth.CredentialTypeCodeSessionOAuth), nil
}

func (s *Server) authenticateFilestoreToken(r *http.Request, rawToken string) (filestoreapi.Principal, *httpapi.Error) {
	if s.filestoreCredentials == nil || s.db == nil {
		return filestoreapi.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	claims, err := s.filestoreCredentials.Verify(rawToken)
	if err != nil {
		return filestoreapi.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid bearer token")
	}
	scope, err := s.db.ResolveFilestoreTokenScope(
		r.Context(),
		claims.OrgUUID,
		claims.AccountUUID,
		claims.WorkspaceUUID,
		claims.WorkspaceTaggedID,
		claims.ResolvedWorkspaceTaggedID,
		claims.FilesystemID,
	)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return filestoreapi.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid bearer token")
		}
		log.Printf("resolve filestore token scope: %v", err)
		return filestoreapi.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
	}
	// 签名只证明签发时的快照未被篡改；组织 taints 与 CMEK 配置状态还须匹配数据库现值。
	// 因此策略变更后，旧 token 会在下一次请求时失效，不能继续携带过期的租户安全属性。
	// 这里校验的是 CMEK 配置状态；具体密钥选择和 S3 加密参数仍属于对象存储边界。
	if !filestoreapi.OrgTaintsEqual(scope.OrgTaints, claims.OrgTaints) ||
		scope.WorkspaceCMEKEnabled != claims.WorkspaceCMEKEnabled {
		return filestoreapi.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Invalid bearer token")
	}
	readonly := claims.Readonly != nil && *claims.Readonly
	return filestoreapi.Principal{
		Subject:                claims.Subject,
		OrganizationID:         scope.OrganizationID,
		OrganizationUUID:       scope.OrganizationUUID,
		OrganizationExternalID: scope.OrganizationExternalID,
		WorkspaceID:            scope.WorkspaceID,
		WorkspaceUUID:          scope.WorkspaceUUID,
		WorkspaceExternalID:    scope.WorkspaceExternalID,
		AccountID:              scope.AccountID,
		AccountUUID:            scope.AccountUUID,
		AccountExternalID:      scope.AccountExternalID,
		FilesystemInternalID:   scope.FilesystemID,
		FilesystemUUID:         scope.FilesystemUUID,
		FilesystemExternalID:   scope.FilesystemExternalID,
		Readonly:               readonly,
		OrganizationTaints:     append([]string(nil), claims.OrgTaints...),
		WorkspaceCMEKEnabled:   claims.WorkspaceCMEKEnabled,
	}, nil
}

func principalFromCodeSessionCredential(codeSession db.CodeSessionCredentialContext, credentialType string) auth.Principal {
	return auth.Principal{
		CredentialType:          credentialType,
		OrganizationID:          codeSession.OrganizationID,
		OrganizationUUID:        codeSession.OrganizationUUID,
		OrganizationExternalID:  codeSession.OrganizationExternalID,
		WorkspaceID:             codeSession.WorkspaceID,
		WorkspaceUUID:           codeSession.WorkspaceUUID,
		WorkspaceExternalID:     codeSession.WorkspaceExternalID,
		CodeSessionID:           codeSession.CodeSessionID,
		CodeSessionExternalID:   codeSession.CodeSessionExternalID,
		PublicSessionID:         codeSession.PublicSessionID,
		PublicSessionExternalID: codeSession.PublicSessionExternalID,
		AgentID:                 codeSession.AgentID,
		AgentExternalID:         codeSession.AgentExternalID,
		AgentVersion:            codeSession.AgentVersion,
	}
}

func isMessagesPath(requestPath string) bool {
	// 只接受 canonical Messages 路径；尾部斜杠兼容由这里显式处理，不开放其他 /v1 子资源。
	return strings.TrimRight(strings.TrimSpace(requestPath), "/") == "/v1/messages"
}
