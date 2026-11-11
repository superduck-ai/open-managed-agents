package vaults

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestNormalizeCredentialAuthForUpdateRequiresCompleteReplacementWithoutSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current db.VaultCredential
		update  string
	}{
		{
			name: "static bearer token",
			current: db.VaultCredential{
				AuthType:      "static_bearer",
				CredentialKey: "https://mcp.example.com",
				Auth:          json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com"}`),
			},
			update: `{"type":"static_bearer"}`,
		},
		{
			name: "environment variable value",
			current: db.VaultCredential{
				AuthType:      "environment_variable",
				CredentialKey: "TOKEN",
				Auth: json.RawMessage(`{
					"type":"environment_variable",
					"secret_name":"TOKEN",
					"placeholder":"oma_ph_testplaceholder0123456789abcd",
					"networking":{"type":"unrestricted"},
					"injection_location":{"header":true,"body":false}
				}`),
			},
			update: `{"type":"environment_variable"}`,
		},
		{
			name: "oauth refresh secret",
			current: db.VaultCredential{
				AuthType:      "mcp_oauth",
				CredentialKey: "https://mcp.example.com",
				Auth: json.RawMessage(`{
					"type":"mcp_oauth",
					"mcp_server_url":"https://mcp.example.com",
					"refresh":{
						"token_endpoint":"https://auth.example.com/token",
						"client_id":"client-id",
						"token_endpoint_auth":{"type":"client_secret_basic"}
					}
				}`),
			},
			update: `{"type":"mcp_oauth","access_token":"replacement-token"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeCredentialAuthForUpdate(test.current, nil, json.RawMessage(test.update))
			if !errors.Is(err, ErrMissingSecretEnvelope) {
				t.Fatalf("normalize update error = %v, want ErrMissingSecretEnvelope", err)
			}
		})
	}
}

func TestNormalizeCredentialAuthForCreateRejectsSchemaMismatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "string field",
			raw:  `{"type":"static_bearer","mcp_server_url":42,"token":"token"}`,
		},
		{
			name: "nested object",
			raw:  `{"type":"mcp_oauth","mcp_server_url":"https://mcp.example.com","access_token":"token","refresh":"invalid"}`,
		},
		{
			name: "networking discriminator",
			raw:  `{"type":"environment_variable","secret_name":"TOKEN","secret_value":"secret","networking":{"type":42}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeCredentialAuthForCreate(json.RawMessage(test.raw))
			if err == nil {
				t.Fatal("normalize create succeeded")
			}
		})
	}
}

func TestNormalizeCredentialAuthForUpdateRejectsInvalidStoredPublicSchema(t *testing.T) {
	t.Parallel()

	_, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "static_bearer",
		CredentialKey: "https://mcp.example.com",
		Auth:          json.RawMessage(`{"type":"static_bearer","mcp_server_url":42}`),
	}, []byte(`{"type":"static_bearer","token":"old-token"}`), json.RawMessage(`{"type":"static_bearer","token":"new-token"}`))
	if err == nil || !strings.Contains(err.Error(), "decode stored credential auth") {
		t.Fatalf("normalize update error = %v", err)
	}
}

func TestNormalizeCredentialAuthForUpdateRejectsWrongSecretSchema(t *testing.T) {
	t.Parallel()

	_, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "static_bearer",
		CredentialKey: "https://mcp.example.com",
		Auth:          json.RawMessage(`{"type":"static_bearer","mcp_server_url":"https://mcp.example.com"}`),
	}, []byte(`{"type":"environment_variable","secret_value":"value"}`), json.RawMessage(`{"type":"static_bearer","token":"new-token"}`))
	if err == nil || !strings.Contains(err.Error(), "static_bearer secret has type") {
		t.Fatalf("normalize update error = %v", err)
	}
}

func TestNormalizeCredentialAuthForUpdateReplacesMissingOAuthSecret(t *testing.T) {
	t.Parallel()

	state, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "mcp_oauth",
		CredentialKey: "https://mcp.example.com",
		Auth: json.RawMessage(`{
			"type":"mcp_oauth",
			"mcp_server_url":"https://mcp.example.com",
			"refresh":{
				"token_endpoint":"https://auth.example.com/token",
				"client_id":"client-id",
				"token_endpoint_auth":{"type":"client_secret_basic"}
			}
		}`),
	}, nil, json.RawMessage(`{"type":"mcp_oauth","access_token":"replacement-token","refresh":null}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.AccessToken != "replacement-token" || secret.Refresh != nil {
		t.Fatalf("unexpected replacement secret: %+v", secret)
	}
}

func TestNormalizeCredentialAuthForUpdateAppliesTypedNullPatch(t *testing.T) {
	t.Parallel()

	state, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "mcp_oauth",
		CredentialKey: "https://mcp.example.com",
		Auth: json.RawMessage(`{
			"type":"mcp_oauth",
			"mcp_server_url":"https://mcp.example.com",
			"refresh":{
				"token_endpoint":"https://auth.example.com/token",
				"client_id":"client-id",
				"token_endpoint_auth":{"type":"client_secret_basic"},
				"scope":"read"
			}
		}`),
	}, []byte(`{
		"type":"mcp_oauth",
		"access_token":"access-token",
		"refresh":{
			"refresh_token":"refresh-token",
			"token_endpoint_auth":{"type":"client_secret_basic","client_secret":"client-secret"}
		}
	}`), json.RawMessage(`{
		"type":"mcp_oauth",
		"refresh":{"scope":null,"token_endpoint_auth":null}
	}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}

	public, err := decodeCredentialAuth(state.PublicAuth)
	if err != nil {
		t.Fatalf("decode public auth: %v", err)
	}
	oauth, ok := public.value.(*mcpOAuthCredentialAuth)
	if !ok || oauth.Refresh == nil {
		t.Fatalf("unexpected public auth: %#v", public.value)
	}
	if oauth.Refresh.Scope != nil || oauth.Refresh.TokenEndpointAuth.Type != "none" {
		t.Fatalf("unexpected public refresh: %+v", oauth.Refresh)
	}
	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.Refresh == nil || secret.Refresh.TokenEndpointAuth == nil || secret.Refresh.TokenEndpointAuth.Type != "none" {
		t.Fatalf("unexpected secret refresh: %+v", secret.Refresh)
	}
}

func TestNormalizeCredentialAuthForUpdateAllowsPlatformOAuthWithoutSealedClientSecret(t *testing.T) {
	t.Parallel()

	state, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "mcp_oauth",
		CredentialKey: "https://mcp.example.com/mcp",
		Auth: json.RawMessage(`{
			"type":"mcp_oauth",
			"mcp_server_url":"https://mcp.example.com/mcp",
			"refresh":{
				"token_endpoint":"https://auth.example.com/token",
				"client_id":"platform-client",
				"token_endpoint_auth":{"type":"client_secret_post"}
			}
		}`),
	}, []byte(`{
		"type":"mcp_oauth",
		"access_token":"access-token",
		"refresh":{
			"refresh_token":"refresh-token",
			"token_endpoint_auth":{"type":"client_secret_post"}
		}
	}`), json.RawMessage(`{"type":"mcp_oauth"}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.AccessToken != "access-token" || secret.Refresh == nil || secret.Refresh.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected secret: %+v", secret)
	}
	if secret.Refresh.TokenEndpointAuth == nil || secret.Refresh.TokenEndpointAuth.ClientSecret != "" {
		t.Fatalf("platform sealed client_secret = %#v, want empty", secret.Refresh.TokenEndpointAuth)
	}
}

func TestNormalizeCredentialAuthForUpdatePreservesMCPOAuthRefresh(t *testing.T) {
	t.Parallel()

	state, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "mcp_oauth",
		CredentialKey: "https://mcp.example.com",
		Auth: json.RawMessage(`{
			"type":"mcp_oauth",
			"mcp_server_url":"https://mcp.example.com",
			"refresh":{
				"token_endpoint":"https://auth.example.com/token",
				"client_id":"client-id",
				"token_endpoint_auth":{"type":"client_secret_basic"}
			}
		}`),
	}, []byte(`{
			"type":"mcp_oauth",
			"access_token":"old-access-token",
			"refresh":{
				"refresh_token":"refresh-token",
				"token_endpoint_auth":{"type":"client_secret_basic","client_secret":"client-secret"}
			}
		}`), json.RawMessage(`{"type":"mcp_oauth","access_token":"new-access-token"}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}

	secret, err := decodeMCPOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode normalized secret: %v", err)
	}
	if secret.AccessToken != "new-access-token" || secret.Refresh == nil || secret.Refresh.RefreshToken != "refresh-token" {
		t.Fatalf("unexpected normalized secret: %+v", secret)
	}
	if secret.Refresh.TokenEndpointAuth == nil || secret.Refresh.TokenEndpointAuth.ClientSecret != "client-secret" {
		t.Fatalf("unexpected token endpoint auth secret: %+v", secret.Refresh.TokenEndpointAuth)
	}
}

func TestNormalizeEnvironmentVariablePreservesSecretValueWhitespace(t *testing.T) {
	t.Parallel()

	const createValue = "  line-one\nline-two\n"
	createState, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"environment_variable",
		"secret_name":" API_KEY ",
		"secret_value":"  line-one\nline-two\n",
		"networking":{"type":"unrestricted"}
	}`))
	if err != nil {
		t.Fatalf("normalize create: %v", err)
	}
	createSecret, err := decodeEnvironmentVariableCredentialSecret(createState.SecretPayload)
	if err != nil {
		t.Fatalf("decode create secret: %v", err)
	}
	if createSecret.SecretValue != createValue {
		t.Fatalf("create secret_value = %q, want verbatim %q", createSecret.SecretValue, createValue)
	}
	if createState.Key != "API_KEY" {
		t.Fatalf("create credential key = %q, want trimmed secret_name", createState.Key)
	}

	const updateValue = "  rotated\n"
	updateState, err := normalizeCredentialAuthForUpdate(db.VaultCredential{
		AuthType:      "environment_variable",
		CredentialKey: "API_KEY",
		Auth: json.RawMessage(`{
			"type":"environment_variable",
			"secret_name":"API_KEY",
			"networking":{"type":"unrestricted"},
			"placeholder":"oma_ph_test",
			"injection_location":{"header":true,"body":false}
		}`),
	}, []byte(`{"type":"environment_variable","secret_value":"old"}`), json.RawMessage(`{
		"type":"environment_variable",
		"secret_value":"  rotated\n"
	}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	updateSecret, err := decodeEnvironmentVariableCredentialSecret(updateState.SecretPayload)
	if err != nil {
		t.Fatalf("decode update secret: %v", err)
	}
	if updateSecret.SecretValue != updateValue {
		t.Fatalf("update secret_value = %q, want verbatim %q", updateSecret.SecretValue, updateValue)
	}
}

func TestNormalizeEnvOAuthCredentialForCreate(t *testing.T) {
	t.Parallel()

	_, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"env_oauth",
		"provider":"builtin:feishu",
		"secret_name":"LARKSUITE_CLI_USER_ACCESS_TOKEN",
		"access_token":"u-token"
	}`))
	if err == nil || !strings.Contains(err.Error(), "auth.networking") {
		t.Fatalf("error = %v, want auth.networking required", err)
	}

	state, err := normalizeCredentialAuthForCreate(json.RawMessage(`{
		"type":"env_oauth",
		"provider":"builtin:feishu",
		"secret_name":" LARKSUITE_CLI_USER_ACCESS_TOKEN ",
		"networking":{"type":"limited","allowed_hosts":["open.feishu.cn","accounts.feishu.cn"]},
		"access_token":" u-token ",
		"refresh_token":"ur-token",
		"expires_at":"2099-12-31T23:59:59Z",
		"scope":"offline_access"
	}`))
	if err != nil {
		t.Fatalf("normalize create: %v", err)
	}
	if state.AuthType != "env_oauth" || state.Key != EnvOAuthFeishuDefaultSecretName {
		t.Fatalf("state = %+v", state)
	}
	auth, err := decodeCredentialAuth(state.PublicAuth)
	if err != nil {
		t.Fatalf("decode public auth: %v", err)
	}
	value := auth.value.(*envOAuthCredentialAuth)
	if value.Provider != EnvOAuthProviderBuiltinFeishu || value.SecretName != EnvOAuthFeishuDefaultSecretName {
		t.Fatalf("unexpected auth: %+v", value)
	}
	if !value.InjectionLocation.Header || value.InjectionLocation.Body {
		t.Fatalf("injection_location = %+v, want header only", value.InjectionLocation)
	}
	if !value.HasRefreshToken || value.ExpiresAt == nil || value.Scope == nil {
		t.Fatalf("unexpected auth metadata: %+v", value)
	}
	if !strings.HasPrefix(value.Placeholder, opaquePlaceholderPrefix) {
		t.Fatalf("placeholder = %q", value.Placeholder)
	}
	secret, err := decodeEnvOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if secret.AccessToken != "u-token" || secret.RefreshToken != "ur-token" {
		t.Fatalf("unexpected secret: %+v", secret)
	}
}

func TestNormalizeEnvOAuthCredentialForCreateRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "missing provider", raw: `{"type":"env_oauth","secret_name":"TOKEN","access_token":"token","networking":{"type":"unrestricted"}}`, want: "auth.provider"},
		{name: "invalid provider", raw: `{"type":"env_oauth","provider":"unknown","secret_name":"TOKEN","access_token":"token","networking":{"type":"unrestricted"}}`, want: "auth.provider"},
		{name: "reserved secret name", raw: `{"type":"env_oauth","provider":"builtin:feishu","secret_name":"ANTHROPIC_API_KEY","access_token":"token","networking":{"type":"unrestricted"}}`, want: "is reserved"},
		{name: "limited without hosts", raw: `{"type":"env_oauth","provider":"builtin:feishu","secret_name":"TOKEN","access_token":"token","networking":{"type":"limited","allowed_hosts":[]}}`, want: "allowed_hosts"},
		{name: "missing access token", raw: `{"type":"env_oauth","provider":"builtin:feishu","secret_name":"TOKEN","networking":{"type":"unrestricted"}}`, want: "auth.access_token"},
		{name: "invalid expiry", raw: `{"type":"env_oauth","provider":"builtin:feishu","secret_name":"TOKEN","access_token":"token","networking":{"type":"unrestricted"},"expires_at":"tomorrow"}`, want: "RFC3339"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeCredentialAuthForCreate(json.RawMessage(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNormalizeEnvOAuthCredentialForUpdate(t *testing.T) {
	t.Parallel()

	current := db.VaultCredential{
		AuthType:      "env_oauth",
		CredentialKey: EnvOAuthFeishuDefaultSecretName,
		Auth: json.RawMessage(`{
			"type":"env_oauth",
			"provider":"builtin:feishu",
			"secret_name":"LARKSUITE_CLI_USER_ACCESS_TOKEN",
			"placeholder":"oma_ph_testplaceholder",
			"networking":{"type":"limited","allowed_hosts":["open.feishu.cn"]},
			"injection_location":{"header":true,"body":false},
			"expires_at":"2099-12-31T23:59:59Z",
			"has_refresh_token":true
		}`),
	}
	secret := []byte(`{"type":"env_oauth","access_token":"old-access","refresh_token":"old-refresh"}`)

	state, err := normalizeCredentialAuthForUpdate(current, secret, json.RawMessage(`{
		"type":"env_oauth",
		"networking":{"type":"unrestricted"},
		"access_token":"new-access",
		"refresh_token":"new-refresh",
		"expires_at":"2098-01-01T00:00:00Z",
		"scope":"docx:read"
	}`))
	if err != nil {
		t.Fatalf("normalize update: %v", err)
	}
	auth, err := decodeCredentialAuth(state.PublicAuth)
	if err != nil {
		t.Fatalf("decode public auth: %v", err)
	}
	value := auth.value.(*envOAuthCredentialAuth)
	if value.Networking.Type != "unrestricted" || !value.HasRefreshToken || value.ExpiresAt == nil || value.Scope == nil {
		t.Fatalf("unexpected auth: %+v", value)
	}
	nextSecret, err := decodeEnvOAuthCredentialSecret(state.SecretPayload)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	if nextSecret.AccessToken != "new-access" || nextSecret.RefreshToken != "new-refresh" {
		t.Fatalf("unexpected secret: %+v", nextSecret)
	}

	cleared, err := normalizeCredentialAuthForUpdate(current, secret, json.RawMessage(`{"type":"env_oauth","expires_at":null,"scope":null}`))
	if err != nil {
		t.Fatalf("normalize clear: %v", err)
	}
	clearedAuth, err := decodeCredentialAuth(cleared.PublicAuth)
	if err != nil {
		t.Fatalf("decode cleared auth: %v", err)
	}
	clearedValue := clearedAuth.value.(*envOAuthCredentialAuth)
	if clearedValue.ExpiresAt != nil || clearedValue.Scope != nil {
		t.Fatalf("unexpected cleared auth: %+v", clearedValue)
	}
}

func TestNormalizeEnvOAuthCredentialForUpdateRejectsImmutableFields(t *testing.T) {
	t.Parallel()

	current := db.VaultCredential{
		AuthType:      "env_oauth",
		CredentialKey: "TOKEN",
		Auth: json.RawMessage(`{
			"type":"env_oauth",
			"provider":"builtin:feishu",
			"secret_name":"TOKEN",
			"placeholder":"oma_ph_testplaceholder",
			"networking":{"type":"unrestricted"},
			"injection_location":{"header":true,"body":false}
		}`),
	}
	for _, raw := range []string{
		`{"type":"env_oauth","provider":"builtin:feishu"}`,
		`{"type":"env_oauth","secret_name":"OTHER"}`,
	} {
		_, err := normalizeCredentialAuthForUpdate(current, []byte(`{"type":"env_oauth","access_token":"token"}`), json.RawMessage(raw))
		if err == nil || !strings.Contains(err.Error(), "immutable") {
			t.Fatalf("error = %v, want immutable", err)
		}
	}
}

func TestEnvOAuthInjectionLocationInput(t *testing.T) {
	for _, location := range []string{`{"header":false,"body":true}`, `{"header":true,"body":true}`, `{"header":false,"body":false}`, `{"body":true}`, `null`, `"body"`, `{"header":"true"}`, `{}`, `{"header":true,"body":false}`} {
		t.Run(location, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"env_oauth","provider":"builtin:feishu","secret_name":"TOKEN","access_token":"token","networking":{"type":"unrestricted"},"injection_location":` + location + `}`)
			state, err := normalizeCredentialAuthForCreate(raw)
			valid := location == `{}` || location == `{"header":true,"body":false}`
			if !valid {
				if err == nil || !strings.Contains(err.Error(), "injection_location") {
					t.Fatalf("创建未拒绝非法位置: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			base, baseErr := normalizeCredentialAuthForCreate(json.RawMessage(`{"type":"env_oauth","provider":"builtin:feishu","secret_name":"TOKEN","access_token":"old-token","networking":{"type":"unrestricted"}}`))
			if baseErr != nil {
				t.Fatal(baseErr)
			}
			current := db.VaultCredential{AuthType: "env_oauth", CredentialKey: "TOKEN", Auth: base.PublicAuth}
			updated, updateErr := normalizeCredentialAuthForUpdate(current, base.SecretPayload, json.RawMessage(`{"type":"env_oauth","injection_location":`+location+`}`))
			if !valid {
				if updateErr == nil || !strings.Contains(updateErr.Error(), "injection_location") {
					t.Fatalf("更新未拒绝非法位置: %v", updateErr)
				}
				return
			}
			if updateErr != nil {
				t.Fatal(updateErr)
			}
			for _, authJSON := range []json.RawMessage{state.PublicAuth, updated.PublicAuth} {
				public, err := decodeEnvOAuthCredentialAuth(authJSON)
				if err != nil || !public.InjectionLocation.Header || public.InjectionLocation.Body {
					t.Fatalf("位置=%+v 错误=%v", public, err)
				}
			}
		})
	}
}
