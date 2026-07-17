package api

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func (s *Server) authenticateService(r *http.Request) (auth.Principal, *httpapi.Error) {
	// workspace API key 始终优先；只有查不到普通 key 时，才按请求路径尝试用途受限的凭证。
	// 这样 OAuth-compatible token 不会意外获得 workspace API key 的完整权限。
	apiKey := auth.ExtractAPIKey(r)
	if apiKey == "" {
		return auth.Principal{}, httpapi.NewError(http.StatusUnauthorized, "authentication_error", "Missing API key")
	}
	key, err := s.db.GetAPIKey(r.Context(), auth.HashAPIKey(apiKey))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return s.authenticateScopedServiceCredential(r, apiKey)
		}
		log.Printf("authenticate api key: %v", err)
		return auth.Principal{}, httpapi.NewError(http.StatusInternalServerError, "api_error", "Authentication failed")
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
	}, nil
}

func (s *Server) authenticateScopedServiceCredential(r *http.Request, apiKey string) (auth.Principal, *httpapi.Error) {
	// 每类 scoped credential 都必须先命中自己的路由白名单，再访问对应的 hash 查询。
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
	return auth.Principal{
		CredentialType:          auth.CredentialTypeCodeSessionOAuth,
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
	}, nil
}

func isMessagesPath(requestPath string) bool {
	// 只接受 canonical Messages 路径；尾部斜杠兼容由这里显式处理，不开放其他 /v1 子资源。
	return strings.TrimRight(strings.TrimSpace(requestPath), "/") == "/v1/messages"
}
