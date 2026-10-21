//go:build integration || e2e || e2b_integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestStaticBearerCredentialURLImmutable(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("immutable-credential-bucket"))
	defer app.close()
	vault := createVault(t, app, `{"display_name":"immutable URL regression"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	credential := createVaultCredential(t, app, vault.ID, staticBearerBody("original name", "https://original.test/mcp", "local-original"))
	before := retrieveVaultCredential(t, app, vault.ID, credential.ID)
	envelope, binding := readVaultCredentialEnvelope(t, app, credential.ID)
	original, err := app.vaultSecrets.Open(context.Background(), binding, envelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"https://changed.test/mcp", "https://original.test/mcp", "", nil, 42} {
		raw, _ := json.Marshal(map[string]any{"display_name": "must not persist", "metadata": map[string]string{"rejected": "true"}, "auth": map[string]any{"type": "static_bearer", "mcp_server_url": value, "token": "local-replacement"}})
		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials/"+credential.ID+"?beta=true", strings.NewReader(string(raw)), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
		after := retrieveVaultCredential(t, app, vault.ID, credential.ID)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("rejected structural update changed public Credential")
		}
		var key string
		if err := app.pool.QueryRow(context.Background(), `select credential_key from vault_credentials where external_id=$1`, credential.ID).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key != "https://original.test/mcp" {
			t.Fatal("rejected structural update changed stored key")
		}
		envelope, binding = readVaultCredentialEnvelope(t, app, credential.ID)
		opened, err := app.vaultSecrets.Open(context.Background(), binding, envelope)
		if err != nil || !bytes.Equal(opened, original) {
			t.Fatal("rejected structural update changed secret")
		}
	}
	rotated := updateVaultCredential(t, app, vault.ID, credential.ID, `{"display_name":"rotated name","auth":{"type":"static_bearer","token":"local-replacement"}}`)
	if rotated.DisplayName != "rotated name" || !bytes.Equal(rotated.Auth, before.Auth) {
		t.Fatal("secret-only rotation changed structural configuration")
	}
	envelope, binding = readVaultCredentialEnvelope(t, app, credential.ID)
	opened, err := app.vaultSecrets.Open(context.Background(), binding, envelope)
	if err != nil || !strings.Contains(string(opened), "local-replacement") || strings.Contains(string(opened), "local-original") {
		t.Fatal("secret-only rotation failed")
	}
}
