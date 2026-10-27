//go:build integration || e2e || e2b_integration

package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestVaultCredentialOptionalDisplayName(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("credential-name-bucket"))
	defer app.close()
	t.Run("invalid display name types and length", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"invalid credential name"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		for _, name := range []any{42, []string{"invalid"}, strings.Repeat("x", 256)} {
			body, _ := json.Marshal(map[string]any{"display_name": name, "auth": map[string]any{"type": "static_bearer", "token": "local-original-token", "mcp_server_url": "https://name.test/mcp"}})
			resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(string(body)), defaultTestKey, true)
			assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		}
	})
	for _, variant := range []string{"omitted", "null", "empty", "named"} {
		t.Run(variant, func(t *testing.T) {
			vault := createVault(t, app, `{"display_name":"optional credential name"}`)
			defer cleanupVaultRows(t, app, vault.ID)
			body := map[string]any{"auth": map[string]any{"type": "static_bearer", "token": "local-original-token", "mcp_server_url": "https://name.test/mcp"}}
			switch variant {
			case "null":
				body["display_name"] = nil
			case "empty":
				body["display_name"] = ""
			case "named":
				body["display_name"] = "provided name"
			}
			raw, _ := json.Marshal(body)
			created := createVaultCredential(t, app, vault.ID, string(raw))
			got := retrieveVaultCredential(t, app, vault.ID, created.ID)
			if got.ID != created.ID || (variant == "named" && got.DisplayName != "provided name") {
				t.Fatal("optional credential name changed identity or supplied name")
			}
			duplicate := `{"auth":{"type":"static_bearer","token":"local-replacement-token","mcp_server_url":"https://name.test/mcp"}}`
			resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(duplicate), defaultTestKey, true)
			assertVaultError(t, resp, http.StatusConflict, "conflict_error", "Credential key already exists")
			env, binding := readVaultCredentialEnvelope(t, app, created.ID)
			opened, err := app.vaultSecrets.Open(context.Background(), binding, env)
			if err != nil || !strings.Contains(string(opened), "local-original-token") || strings.Contains(string(opened), "local-replacement-token") {
				t.Fatal("duplicate creation changed original credential secret")
			}
		})
	}
}
