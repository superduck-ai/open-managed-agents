package codesessions

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestSessionCredentialsRejectInvalidTokens(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	credentials := newTestSessionCredentials(t, &now)
	token, err := credentials.Issue(testSessionCredentialIdentity())
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	claims, err := credentials.Verify(token)
	if err != nil {
		t.Fatalf("verify issued token: %v", err)
	}

	tests := map[string]string{
		"failure prefix":    strings.TrimPrefix(token, sessionIngressTokenPrefix),
		"failure tampering": tamperJWT(t, token),
		"failure kid":       signTestJWT(t, jwt.SigningMethodEdDSA, credentials.privateKey, "wrong-kid", claims),
		"failure algorithm": signTestJWT(t, jwt.SigningMethodHS256, []byte("not-ed25519"), credentials.kid, claims),
	}
	for name, rawToken := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := credentials.Verify(rawToken); err == nil {
				t.Fatal("Verify() succeeded, want error")
			}
		})
	}

	t.Run("failure signature", func(t *testing.T) {
		other := newTestSessionCredentials(t, &now)
		rawToken := signTestJWT(t, jwt.SigningMethodEdDSA, other.privateKey, credentials.kid, claims)
		if _, err := credentials.Verify(rawToken); err == nil {
			t.Fatal("Verify() accepted token from another signing key")
		}
	})

	t.Run("failure expiry", func(t *testing.T) {
		expiredClaims := claims
		expiredClaims.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
		expiredToken := signTestJWT(t, jwt.SigningMethodEdDSA, credentials.privateKey, credentials.kid, expiredClaims)
		if _, err := credentials.Verify(expiredToken); err == nil {
			t.Fatal("Verify() accepted expired token")
		}
	})
}

func TestSessionCredentialsClaimsAndLifecycle(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	credentials := newTestSessionCredentials(t, &now)
	identity := testSessionCredentialIdentity()
	token, err := credentials.Issue(identity)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if !strings.HasPrefix(token, sessionIngressTokenPrefix) {
		t.Fatalf("token = %q, want %q prefix", token, sessionIngressTokenPrefix)
	}
	claims, err := credentials.Verify(token)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if claims.Issuer != sessionIngressIssuer || claims.Subject != identity.SessionID ||
		len(claims.Audience) != 1 || claims.Audience[0] != sessionIngressAudience || claims.ID == "" {
		t.Fatalf("unexpected registered claims: %#v", claims.RegisteredClaims)
	}
	if claims.ExpiresAt != nil {
		t.Fatalf("expires_at = %v, want no independent expiry", claims.ExpiresAt)
	}
	if claims.SessionID != identity.SessionID || claims.PublicSessionID != identity.PublicSessionID ||
		claims.AgentID != identity.AgentID || claims.AgentVersion != identity.AgentVersion ||
		claims.OrganizationUUID != identity.OrganizationUUID || claims.WorkspaceUUID != identity.WorkspaceUUID ||
		claims.AccountEmail != identity.AccountEmail || claims.Application != "ccr" || claims.Role != "worker" {
		t.Fatalf("unexpected identity claims: %#v", claims)
	}
	header, payload := decodeTestJWT(t, token)
	if header["alg"] != "EdDSA" || header["typ"] != "JWT" || header["kid"] != credentials.kid {
		t.Fatalf("unexpected JWT header: %#v", header)
	}
	if payload["iss"] != sessionIngressIssuer || payload["application"] != "ccr" {
		t.Fatalf("unexpected JWT payload: %#v", payload)
	}
	if _, exists := payload["exp"]; exists {
		t.Fatalf("JWT payload unexpectedly contains exp: %#v", payload)
	}
	if _, exists := payload["sources"]; exists {
		t.Fatalf("JWT payload unexpectedly contains sources: %#v", payload)
	}
	now = now.Add(24 * time.Hour)
	if _, err := credentials.Verify(token); err != nil {
		t.Fatalf("lifecycle-bound token failed verification after 24 hours: %v", err)
	}

	oauthToken, err := newOAuthCompatibleToken()
	if err != nil {
		t.Fatalf("newOAuthCompatibleToken() error = %v", err)
	}
	if !strings.HasPrefix(oauthToken, oauthCompatibleTokenPrefix) || strings.Contains(oauthToken, ".") {
		t.Fatalf("unexpected OAuth-compatible token format: %q", oauthToken)
	}
}

func TestAuthenticateSessionIngressUsesSignedIdentityWithoutDatabaseLifecycle(t *testing.T) {
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	credentials := newTestSessionCredentials(t, &now)
	token, err := credentials.Issue(testSessionCredentialIdentity())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	service := NewServiceWithCredentials(nil, credentials)
	claims, err := service.AuthenticateSessionIngress(token, "cse_test")
	if err != nil {
		t.Fatalf("AuthenticateSessionIngress() error = %v", err)
	}
	if claims.SessionID != "cse_test" {
		t.Fatalf("session_id = %q, want cse_test", claims.SessionID)
	}
	if _, err := service.AuthenticateSessionIngress(token, "cse_other"); err == nil {
		t.Fatal("AuthenticateSessionIngress() accepted token for another request path")
	}
}

func TestSessionCredentialsConfiguration(t *testing.T) {
	if _, err := NewSessionCredentials(config.Config{Env: config.EnvironmentProd}); err == nil {
		t.Fatal("NewSessionCredentials() accepted missing production signing key")
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	keyFile := filepath.Join(t.TempDir(), "code-session-signing-key.pem")
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
		t.Fatalf("write signing key: %v", err)
	}
	credentials, err := NewSessionCredentials(config.Config{
		Env:         config.EnvironmentProd,
		CodeSession: config.CodeSessionConfig{JWTSigningPrivateKeyFile: keyFile},
	})
	if err != nil {
		t.Fatalf("NewSessionCredentials() error = %v", err)
	}
	if len(credentials.privateKey) != ed25519.PrivateKeySize || !strings.HasPrefix(credentials.kid, "ed25519-") {
		t.Fatalf("unexpected loaded credentials: key_bytes=%d kid=%q", len(credentials.privateKey), credentials.kid)
	}
}

func newTestSessionCredentials(t *testing.T, now *time.Time) *SessionCredentials {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return newSessionCredentials(privateKey, func() time.Time { return *now })
}

func testSessionCredentialIdentity() SessionCredentialIdentity {
	return SessionCredentialIdentity{
		SessionID:        "cse_test",
		PublicSessionID:  "session_test",
		AgentID:          "agent_test",
		AgentVersion:     3,
		OrganizationUUID: "100ef143-ea93-46a8-b4e1-7718be112d66",
		WorkspaceUUID:    "f7362a7a-e4d0-4513-80e7-13d4c25fcd81",
		AccountEmail:     "owner@example.com",
	}
}

func signTestJWT(t *testing.T, method jwt.SigningMethod, key any, kid string, claims SessionCredentialClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}
	return sessionIngressTokenPrefix + signed
}

func tamperJWT(t *testing.T, rawToken string) string {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(rawToken, sessionIngressTokenPrefix), ".")
	if len(parts) != 3 || len(parts[1]) == 0 {
		t.Fatalf("invalid test JWT: %q", rawToken)
	}
	if parts[1][0] == 'A' {
		parts[1] = "B" + parts[1][1:]
	} else {
		parts[1] = "A" + parts[1][1:]
	}
	return sessionIngressTokenPrefix + strings.Join(parts, ".")
}

func decodeTestJWT(t *testing.T, rawToken string) (map[string]any, map[string]any) {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(rawToken, sessionIngressTokenPrefix), ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT: %q", rawToken)
	}
	decode := func(value string) map[string]any {
		data, err := base64.RawURLEncoding.DecodeString(value)
		if err != nil {
			t.Fatalf("decode JWT section: %v", err)
		}
		var object map[string]any
		if err := json.Unmarshal(data, &object); err != nil {
			t.Fatalf("decode JWT JSON: %v", err)
		}
		return object
	}
	return decode(parts[0]), decode(parts[1])
}
