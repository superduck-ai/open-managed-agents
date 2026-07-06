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
	CredentialTypeAPIKey          = "api_key"
	CredentialTypeEnvironmentKey  = "environment_key"
	CredentialTypePlatformSession = "platform_session"
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
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return strings.TrimSpace(authz[len("bearer "):])
	}
	return ""
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
