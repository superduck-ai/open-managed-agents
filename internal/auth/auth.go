package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

type contextKey struct{}
type platformMirrorOrganizationAliasKey struct{}

const (
	CredentialTypeAPIKey           = "api_key"
	CredentialTypeCodeSessionOAuth = "code_session_oauth"
	CredentialTypeEnvironmentKey   = "environment_key"
	CredentialTypePlatformSession  = "platform_session"
)

type Principal struct {
	CredentialType            string
	APIKeyID                  int64
	APIKeyExternalID          string
	OrganizationID            int64
	OrganizationUUID          string
	OrganizationExternalID    string
	WorkspaceID               int64
	WorkspaceUUID             string
	WorkspaceExternalID       string
	UserID                    int64
	UserExternalID            string
	PlatformSessionExternalID string
	EnvironmentKeyID          int64
	EnvironmentID             int64
	EnvironmentExternalID     string
	// code-session OAuth 鉴权会填充以下关联字段，供 Messages 请求审计使用；
	// 实际授权仍由 active session 数据库查询决定，不能只信任这些上下文值。
	CodeSessionID           int64
	CodeSessionExternalID   string
	PublicSessionID         int64
	PublicSessionExternalID string
	AgentID                 int64
	AgentExternalID         string
	AgentVersion            int
}

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func HashAPIKey(key string) string {
	return HashSecret(key)
}

func ExtractAPIKey(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("X-Api-Key")); key != "" {
		return key
	}
	return ExtractBearerToken(r)
}

// ExtractBearerToken 只读取 Authorization: Bearer，供不接受 API key 的资源使用。
func ExtractBearerToken(r *http.Request) string {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	scheme, token, ok := strings.Cut(authz, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func ExtractPlatformSessionKey(r *http.Request) string {
	cookie, err := r.Cookie("sessionKey")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, contextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(contextKey{}).(Principal)
	return principal, ok
}

func WithPlatformMirrorOrganizationAlias(ctx context.Context, orgUUID string) context.Context {
	return context.WithValue(ctx, platformMirrorOrganizationAliasKey{}, strings.TrimSpace(orgUUID))
}

func PlatformMirrorOrganizationAliasFromContext(ctx context.Context) string {
	orgUUID, _ := ctx.Value(platformMirrorOrganizationAliasKey{}).(string)
	return strings.TrimSpace(orgUUID)
}
