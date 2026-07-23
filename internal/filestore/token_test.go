package filestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenCredentialsRejectInvalidTokens(t *testing.T) {
	t.Parallel()

	credentials := newTokenCredentialsForTest(t)
	validToken, err := credentials.Issue(filestoreTokenIdentityForTest())
	if err != nil {
		t.Fatalf("issue valid token: %v", err)
	}
	otherCredentials := newTokenCredentialsForTest(t)
	tampered := tamperFilestoreToken(t, validToken)
	unsupportedClaims := decodeFilestoreTokenPayload(t, validToken)
	unsupportedClaims["iss"] = "session-ingress"
	readonlyFalseClaims := decodeFilestoreTokenPayload(t, validToken)
	readonlyFalseClaims["readonly"] = false
	readonlyNullClaims := decodeFilestoreTokenPayload(t, validToken)
	readonlyNullClaims["readonly"] = nil

	for _, test := range []struct {
		name  string
		token string
	}{
		{name: "tampered signature", token: tampered},
		{name: "wrong signing key", token: mustIssueFilestoreToken(t, otherCredentials, filestoreTokenIdentityForTest())},
		{name: "session ingress prefix", token: "sk-ant-si-" + validToken},
		{name: "unsupported extra claim", token: signFilestoreTokenPayload(t, credentials, unsupportedClaims)},
		{name: "readonly false", token: signFilestoreTokenPayload(t, credentials, readonlyFalseClaims)},
		{name: "readonly null", token: signFilestoreTokenPayload(t, credentials, readonlyNullClaims)},
		{name: "not a jwt", token: "not-a-jwt"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := credentials.Verify(test.token); err == nil {
				t.Fatalf("Verify(%q) succeeded, want error", test.name)
			}
		})
	}

	incomplete := filestoreTokenIdentityForTest()
	incomplete.FilesystemID = ""
	if _, err := credentials.Issue(incomplete); err == nil {
		t.Fatal("Issue() succeeded with missing filesystem_id")
	}
}

func TestTokenCredentialsIssueDocumentedClaims(t *testing.T) {
	t.Parallel()

	credentials := newTokenCredentialsForTest(t)
	for _, test := range []struct {
		name         string
		readonly     bool
		wantReadonly bool
		wantPresent  bool
	}{
		{name: "read write token omits readonly"},
		{name: "readonly token", readonly: true, wantReadonly: true, wantPresent: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			identity := filestoreTokenIdentityForTest()
			var rawToken string
			if test.readonly {
				rawToken = mustIssueReadonlyFilestoreToken(t, credentials, identity)
			} else {
				rawToken = mustIssueFilestoreToken(t, credentials, identity)
			}
			payload := decodeFilestoreTokenPayload(t, rawToken)

			for key, want := range map[string]any{
				"sub":                          identity.Subject,
				"org_uuid":                     identity.OrgUUID,
				"account_uuid":                 identity.AccountUUID,
				"workspace_uuid":               identity.WorkspaceUUID,
				"workspace_tagged_id":          identity.WorkspaceTaggedID,
				"resolved_workspace_tagged_id": identity.ResolvedWorkspaceTaggedID,
				"filesystem_id":                identity.FilesystemID,
				"workspace_cmek_enabled":       identity.WorkspaceCMEKEnabled,
			} {
				if got := payload[key]; got != want {
					t.Fatalf("claim %s = %#v, want %#v", key, got, want)
				}
			}
			if _, exists := payload["session_id"]; exists {
				t.Fatalf("filestore token unexpectedly contains session ingress claims: %#v", payload)
			}
			readonly, present := payload["readonly"]
			if present != test.wantPresent || (present && readonly != test.wantReadonly) {
				t.Fatalf("readonly = %#v, present=%t, want %t present=%t", readonly, present, test.wantReadonly, test.wantPresent)
			}
			taints, ok := payload["org_taints"].([]any)
			if !ok || len(taints) != 2 || taints[0] != "compliance" || taints[1] != "restricted" {
				t.Fatalf("org_taints = %#v", payload["org_taints"])
			}
			wantClaimCount := 9
			if test.wantPresent {
				wantClaimCount++
			}
			if len(payload) != wantClaimCount {
				t.Fatalf("JWT claims = %#v, want exactly %d Filestore claims", payload, wantClaimCount)
			}

			claims, err := credentials.Verify(rawToken)
			if err != nil {
				t.Fatalf("verify issued token: %v", err)
			}
			if claims.FilesystemID != identity.FilesystemID || claims.Subject != identity.Subject {
				t.Fatalf("verified claims = %#v", claims)
			}
		})
	}
}

func newTokenCredentialsForTest(t *testing.T) *TokenCredentials {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate filestore token key: %v", err)
	}
	return newTokenCredentials(privateKey)
}

func filestoreTokenIdentityForTest() TokenIdentity {
	return TokenIdentity{
		Subject:                   "account-subject",
		OrgUUID:                   "11111111-1111-4111-8111-111111111111",
		AccountUUID:               "22222222-2222-4222-8222-222222222222",
		WorkspaceUUID:             "33333333-3333-4333-8333-333333333333",
		WorkspaceTaggedID:         "workspace_test",
		ResolvedWorkspaceTaggedID: "workspace_test",
		FilesystemID:              "fs_test",
		OrgTaints:                 []string{"restricted", "compliance", "restricted"},
		WorkspaceCMEKEnabled:      true,
	}
}

func mustIssueReadonlyFilestoreToken(t *testing.T, credentials *TokenCredentials, identity TokenIdentity) string {
	t.Helper()
	token, err := credentials.IssueReadonly(identity)
	if err != nil {
		t.Fatalf("issue readonly filestore token: %v", err)
	}
	return token
}

func mustIssueFilestoreToken(t *testing.T, credentials *TokenCredentials, identity TokenIdentity) string {
	t.Helper()
	token, err := credentials.Issue(identity)
	if err != nil {
		t.Fatalf("issue filestore token: %v", err)
	}
	return token
}

func decodeFilestoreTokenPayload(t *testing.T, rawToken string) map[string]any {
	t.Helper()
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode token payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		t.Fatalf("unmarshal token payload: %v", err)
	}
	return payload
}

func signFilestoreTokenPayload(t *testing.T, credentials *TokenCredentials, payload map[string]any) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims(payload))
	token.Header["kid"] = credentials.kid
	signed, err := token.SignedString(credentials.privateKey)
	if err != nil {
		t.Fatalf("sign custom filestore token payload: %v", err)
	}
	return signed
}

func tamperFilestoreToken(t *testing.T, rawToken string) string {
	t.Helper()
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 || parts[2] == "" {
		t.Fatalf("cannot tamper malformed test token %q", rawToken)
	}
	parts[2] = differentTokenCharacter(parts[2][0]) + parts[2][1:]
	return strings.Join(parts, ".")
}

func differentTokenCharacter(value byte) string {
	if value == 'A' {
		return "B"
	}
	return "A"
}
