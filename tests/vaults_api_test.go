package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type vaultAPIResponse struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	DisplayName string          `json:"display_name"`
	Metadata    json.RawMessage `json:"metadata"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type vaultPageAPIResponse struct {
	Data     []vaultAPIResponse `json:"data"`
	NextPage *string            `json:"next_page"`
}

type vaultCredentialAPIResponse struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	VaultID     string          `json:"vault_id"`
	DisplayName string          `json:"display_name"`
	Auth        json.RawMessage `json:"auth"`
	Metadata    json.RawMessage `json:"metadata"`
	ArchivedAt  *string         `json:"archived_at"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

type vaultCredentialPageAPIResponse struct {
	Data     []vaultCredentialAPIResponse `json:"data"`
	NextPage *string                      `json:"next_page"`
}

type vaultCredentialValidationAPIResponse struct {
	Type            string          `json:"type"`
	CredentialID    string          `json:"credential_id"`
	VaultID         string          `json:"vault_id"`
	ValidatedAt     string          `json:"validated_at"`
	HasRefreshToken bool            `json:"has_refresh_token"`
	Status          string          `json:"status"`
	MCPProbe        json.RawMessage `json:"mcp_probe"`
	Refresh         struct {
		Status       string          `json:"status"`
		HTTPResponse json.RawMessage `json:"http_response"`
	} `json:"refresh"`
}

func TestVaultsAPI(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-bucket"))
	defer app.close()

	t.Run("success missing beta header", func(t *testing.T) {
		resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults?beta=true", nil, defaultTestKey, false)
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("failure missing beta query", func(t *testing.T) {
		resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid json", func(t *testing.T) {
		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults?beta=true", strings.NewReader(`{"display_name":`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure invalid auth type", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault invalid auth"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(`{
			"display_name":"bad credential",
			"auth":{"type":"unknown"}
		}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure duplicate active credential key", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault duplicate key"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		createVaultCredential(t, app, vault.ID, staticBearerBody("dup", "https://mcp.example.com/sse", "token-one"))
		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(staticBearerBody("dup again", "https://mcp.example.com/sse", "token-two")), defaultTestKey, true)
		assertError(t, resp, http.StatusConflict, "conflict_error")
	})

	t.Run("failure credential limit", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault credential limit"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		for i := 0; i < 20; i++ {
			createVaultCredential(t, app, vault.ID, environmentVariableBody("env limit", "SECRET_LIMIT_"+string(rune('A'+i)), "value"))
		}
		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(environmentVariableBody("env limit overflow", "SECRET_LIMIT_OVERFLOW", "value")), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure not found", func(t *testing.T) {
		resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults/vlt_missing_test?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		vault := createVault(t, app, `{"display_name":"vault missing credential"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		resp = doVaultRequest(t, app, http.MethodGet, "/v1/vaults/"+vault.ID+"/credentials/vcrd_missing_test?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")
	})

	t.Run("failure archived vault is immutable", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault archived immutable"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		archiveVault(t, app, vault.ID)

		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"?beta=true", strings.NewReader(`{"display_name":"nope"}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")

		resp = doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(staticBearerBody("nope", "https://mcp.archived.example/sse", "secret")), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("failure cannot add oauth refresh after create", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault refresh immutable"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		credential := createVaultCredential(t, app, vault.ID, `{
			"display_name":"mcp oauth no refresh",
			"auth":{
				"type":"mcp_oauth",
				"mcp_server_url":"https://mcp.no-refresh.example/mcp",
				"access_token":"access-secret",
				"expires_at":"2099-12-31T23:59:59Z"
			}
		}`)
		resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials/"+credential.ID+"?beta=true", strings.NewReader(`{
			"auth":{"type":"mcp_oauth","refresh":{"refresh_token":"new-refresh-secret"}}
		}`), defaultTestKey, true)
		assertError(t, resp, http.StatusBadRequest, "invalid_request_error")
	})

	t.Run("success mcp oauth without expires_at", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault oauth optional expiry"}`)
		defer cleanupVaultRows(t, app, vault.ID)
		credential := createVaultCredential(t, app, vault.ID, `{
			"display_name":"mcp oauth no expiry",
			"auth":{
				"type":"mcp_oauth",
				"mcp_server_url":"https://mcp.no-expiry.example/mcp",
				"access_token":"access-secret"
			}
		}`)
		assertRawContains(t, credential.Auth, `"type":"mcp_oauth"`)
		assertRawContains(t, credential.Auth, `"mcp_server_url":"https://mcp.no-expiry.example/mcp"`)
		assertRawNotContains(t, credential.Auth, `"expires_at"`)
		assertRawNotContains(t, credential.Auth, "access-secret")
	})

	t.Run("success vault create list get update archive delete", func(t *testing.T) {
		first := createVault(t, app, `{"display_name":"vault first","metadata":{"external_user_id":"usr_first"}}`)
		defer cleanupVaultRows(t, app, first.ID)
		if first.Type != "vault" || first.DisplayName != "vault first" || first.ArchivedAt != nil {
			t.Fatalf("unexpected created vault: %+v", first)
		}
		assertRawContains(t, first.Metadata, `"external_user_id":"usr_first"`)

		time.Sleep(5 * time.Millisecond)
		second := createVault(t, app, `{"display_name":"vault second"}`)
		defer cleanupVaultRows(t, app, second.ID)

		retrieved := retrieveVault(t, app, first.ID)
		if retrieved.ID != first.ID {
			t.Fatalf("retrieve vault id = %s, want %s", retrieved.ID, first.ID)
		}
		updated := updateVault(t, app, first.ID, `{"display_name":"vault first updated","metadata":{"external_user_id":"","tier":"gold"}}`)
		if updated.DisplayName != "vault first updated" {
			t.Fatalf("updated display name = %s", updated.DisplayName)
		}
		assertRawContains(t, updated.Metadata, `"tier":"gold"`)
		assertRawNotContains(t, updated.Metadata, `"external_user_id"`)

		page1 := listVaults(t, app, "limit=1")
		if len(page1.Data) != 1 || page1.NextPage == nil {
			t.Fatalf("unexpected first vault page: %+v", page1)
		}
		page2 := listVaults(t, app, "limit=1&page="+url.QueryEscape(*page1.NextPage))
		if len(page2.Data) != 1 {
			t.Fatalf("unexpected second vault page: %+v", page2)
		}

		archived := archiveVault(t, app, first.ID)
		if archived.ArchivedAt == nil {
			t.Fatalf("archived vault archived_at = nil")
		}
		defaultPage := listVaults(t, app, "")
		if containsVault(defaultPage.Data, first.ID) {
			t.Fatalf("default vault list included archived vault: %+v", defaultPage.Data)
		}
		archivedPage := listVaults(t, app, "include_archived=true")
		if !containsVault(archivedPage.Data, first.ID) {
			t.Fatalf("include_archived vault list missing archived vault: %+v", archivedPage.Data)
		}
		deleteVault(t, app, second.ID)
	})

	t.Run("success credentials lifecycle", func(t *testing.T) {
		vault := createVault(t, app, `{"display_name":"vault credentials lifecycle"}`)
		defer cleanupVaultRows(t, app, vault.ID)

		mcp := createVaultCredential(t, app, vault.ID, `{
			"display_name":"mcp oauth",
			"metadata":{"provider":"slack"},
			"auth":{
				"type":"mcp_oauth",
				"mcp_server_url":"https://mcp.slack.example/mcp",
				"access_token":"access-secret",
				"expires_at":"2099-12-31T23:59:59Z",
				"refresh":{
					"token_endpoint":"https://slack.example/oauth/token",
					"client_id":"client-123",
					"scope":"channels:read",
					"resource":"https://slack.example",
					"refresh_token":"refresh-secret",
					"token_endpoint_auth":{"type":"client_secret_post","client_secret":"client-secret"}
				}
			}
		}`)
		if mcp.Type != "vault_credential" || mcp.VaultID != vault.ID {
			t.Fatalf("unexpected mcp credential: %+v", mcp)
		}
		assertRawContains(t, mcp.Auth, `"type":"mcp_oauth"`)
		assertRawContains(t, mcp.Auth, `"token_endpoint":"https://slack.example/oauth/token"`)
		assertRawNotContains(t, mcp.Auth, "access-secret")
		assertRawNotContains(t, mcp.Auth, "refresh-secret")
		assertRawNotContains(t, mcp.Auth, "client-secret")

		static := createVaultCredential(t, app, vault.ID, staticBearerBody("static bearer", "https://mcp.github.example/sse", "bearer-secret"))
		assertRawContains(t, static.Auth, `"type":"static_bearer"`)
		assertRawNotContains(t, static.Auth, "bearer-secret")

		envCred := createVaultCredential(t, app, vault.ID, `{
			"display_name":"env secret",
			"auth":{
				"type":"environment_variable",
				"secret_name":"NOTION_TOKEN",
				"secret_value":"env-secret",
				"networking":{"type":"limited","allowed_hosts":["api.notion.com","*.example.com"]}
			}
		}`)
		assertRawContains(t, envCred.Auth, `"secret_name":"NOTION_TOKEN"`)
		assertRawContains(t, envCred.Auth, `"allowed_hosts":["api.notion.com","*.example.com"]`)
		assertRawNotContains(t, envCred.Auth, "env-secret")

		retrieved := retrieveVaultCredential(t, app, vault.ID, mcp.ID)
		if retrieved.ID != mcp.ID {
			t.Fatalf("retrieve credential id = %s, want %s", retrieved.ID, mcp.ID)
		}
		updated := updateVaultCredential(t, app, vault.ID, mcp.ID, `{
			"display_name":"mcp oauth rotated",
			"metadata":{"provider":null,"rotated":"true"},
			"auth":{
				"type":"mcp_oauth",
				"access_token":"new-access-secret",
				"expires_at":"2100-01-01T00:00:00Z",
				"refresh":{"refresh_token":"new-refresh-secret","scope":"channels:read chat:write"}
			}
		}`)
		if updated.DisplayName != "mcp oauth rotated" {
			t.Fatalf("updated credential display name = %s", updated.DisplayName)
		}
		assertRawContains(t, updated.Metadata, `"rotated":"true"`)
		assertRawNotContains(t, updated.Auth, "new-access-secret")
		assertRawNotContains(t, updated.Auth, "new-refresh-secret")
		assertRawContains(t, updated.Auth, `"scope":"channels:read chat:write"`)

		validation := validateVaultCredential(t, app, vault.ID, mcp.ID)
		if validation.Type != "vault_credential_validation" || validation.CredentialID != mcp.ID || !validation.HasRefreshToken || validation.Refresh.Status != "connect_error" {
			t.Fatalf("unexpected validation response: %+v", validation)
		}

		credentialsPage1 := listVaultCredentials(t, app, vault.ID, "limit=2")
		if len(credentialsPage1.Data) != 2 || credentialsPage1.NextPage == nil {
			t.Fatalf("unexpected credentials first page: %+v", credentialsPage1)
		}
		credentialsPage2 := listVaultCredentials(t, app, vault.ID, "limit=2&page="+url.QueryEscape(*credentialsPage1.NextPage))
		if len(credentialsPage2.Data) != 1 {
			t.Fatalf("unexpected credentials second page: %+v", credentialsPage2)
		}

		archivedStatic := archiveVaultCredential(t, app, vault.ID, static.ID)
		if archivedStatic.ArchivedAt == nil {
			t.Fatalf("archived credential archived_at = nil")
		}
		replacement := createVaultCredential(t, app, vault.ID, staticBearerBody("static replacement", "https://mcp.github.example/sse", "replacement-secret"))
		if replacement.ID == "" {
			t.Fatalf("replacement credential has empty id")
		}
		archivedCredentialPage := listVaultCredentials(t, app, vault.ID, "include_archived=true")
		if !containsVaultCredential(archivedCredentialPage.Data, static.ID) {
			t.Fatalf("include_archived credential list missing archived credential: %+v", archivedCredentialPage.Data)
		}

		deleteVaultCredential(t, app, vault.ID, envCred.ID)
		resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults/"+vault.ID+"/credentials/"+envCred.ID+"?beta=true", nil, defaultTestKey, true)
		assertError(t, resp, http.StatusNotFound, "not_found_error")

		archivedVault := archiveVault(t, app, vault.ID)
		if archivedVault.ArchivedAt == nil {
			t.Fatalf("archive vault archived_at = nil")
		}
		pageAfterVaultArchive := listVaultCredentials(t, app, vault.ID, "include_archived=true")
		for _, credential := range pageAfterVaultArchive.Data {
			if credential.ArchivedAt == nil {
				t.Fatalf("vault archive did not archive credential: %+v", credential)
			}
		}
		assertVaultSecretsPurged(t, app, vault.ID)
	})
}

func TestVaultWebhooks(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.Webhook.EndpointURL = "https://webhook.example.com"
	cfg.Webhook.SigningKey = "whsec_c2VjcmV0Cg=="
	cfg.Webhook.EventTypes = []string{
		"vault.created",
		"vault.archived",
		"vault.deleted",
		"vault_credential.created",
		"vault_credential.archived",
		"vault_credential.deleted",
	}
	cfg.Webhook.WorkerEnabled = true
	app := newTestAppWithStore(t, &cfg, newFakeStore("vaults-webhooks-bucket"))
	defer app.close()
	clearWebhookState(t, app)
	defer clearWebhookState(t, app)

	vault := createVault(t, app, `{"display_name":"vault webhook lifecycle"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	if count := webhookJobCount(t, app, "vault.created", vault.ID); count != 1 {
		t.Fatalf("vault.created webhook jobs = %d, want 1", count)
	}

	credential := createVaultCredential(t, app, vault.ID, staticBearerBody("webhook direct archive", "https://mcp.webhook.example/sse", "secret"))
	if count := webhookJobCount(t, app, "vault_credential.created", credential.ID); count != 1 {
		t.Fatalf("vault_credential.created webhook jobs = %d, want 1", count)
	}
	if got := webhookJobDataField(t, app, "vault_credential.created", credential.ID, "vault_id"); got != vault.ID {
		t.Fatalf("created credential vault_id = %s, want %s", got, vault.ID)
	}

	archiveVaultCredential(t, app, vault.ID, credential.ID)
	if count := webhookJobCount(t, app, "vault_credential.archived", credential.ID); count != 1 {
		t.Fatalf("direct vault_credential.archived webhook jobs = %d, want 1", count)
	}
	if got := webhookJobDataField(t, app, "vault_credential.archived", credential.ID, "vault_id"); got != vault.ID {
		t.Fatalf("archived credential vault_id = %s, want %s", got, vault.ID)
	}

	activeCredential := createVaultCredential(t, app, vault.ID, staticBearerBody("webhook vault archive", "https://mcp.archive.example/sse", "secret"))
	archiveVault(t, app, vault.ID)
	if count := webhookJobCount(t, app, "vault.archived", vault.ID); count != 1 {
		t.Fatalf("vault.archived webhook jobs = %d, want 1", count)
	}
	if count := webhookJobCount(t, app, "vault_credential.archived", activeCredential.ID); count != 1 {
		t.Fatalf("vault archive credential webhook jobs = %d, want 1", count)
	}
	if count := webhookJobCount(t, app, "vault_credential.archived", credential.ID); count != 1 {
		t.Fatalf("already archived credential webhook jobs = %d, want no duplicate", count)
	}

	deleteVaultRecord := createVault(t, app, `{"display_name":"vault webhook delete"}`)
	defer cleanupVaultRows(t, app, deleteVaultRecord.ID)
	deleteCredential := createVaultCredential(t, app, deleteVaultRecord.ID, staticBearerBody("webhook delete", "https://mcp.delete.example/sse", "secret"))
	deleteVault(t, app, deleteVaultRecord.ID)
	if count := webhookJobCount(t, app, "vault.deleted", deleteVaultRecord.ID); count != 1 {
		t.Fatalf("vault.deleted webhook jobs = %d, want 1", count)
	}
	if count := webhookJobCount(t, app, "vault_credential.deleted", deleteCredential.ID); count != 1 {
		t.Fatalf("vault_credential.deleted webhook jobs = %d, want 1", count)
	}
	if got := webhookJobDataField(t, app, "vault_credential.deleted", deleteCredential.ID, "vault_id"); got != deleteVaultRecord.ID {
		t.Fatalf("deleted credential vault_id = %s, want %s", got, deleteVaultRecord.ID)
	}
}

func TestVaultsSchemaHasNoForeignKeys(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-schema-bucket"))
	defer app.close()

	var foreignKeyCount int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from pg_constraint con
		join pg_class cls on cls.oid = con.conrelid
		join pg_namespace ns on ns.oid = cls.relnamespace
		where con.contype = 'f'
			and ns.oid = current_schema()::regnamespace
			and cls.relname in ('vaults', 'vault_credentials')
	`).Scan(&foreignKeyCount); err != nil {
		t.Fatalf("count vaults foreign keys: %v", err)
	}
	if foreignKeyCount != 0 {
		t.Fatalf("vaults foreign key count = %d, want 0", foreignKeyCount)
	}
}

func doVaultRequest(t *testing.T, app *testApp, method, path string, body io.Reader, key string, betaHeader bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, app.baseURL+path, body)
	if err != nil {
		t.Fatalf("new vault request: %v", err)
	}
	if key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	if betaHeader {
		req.Header.Set("anthropic-beta", "managed-agents-2026-04-01")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.client.Do(req)
	if err != nil {
		t.Fatalf("do vault request: %v", err)
	}
	return resp
}

func createVault(t *testing.T, app *testApp, body string) vaultAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create vault status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var vault vaultAPIResponse
	decodeJSON(t, resp.Body, &vault)
	if vault.ID == "" {
		t.Fatalf("create vault returned empty id: %+v", vault)
	}
	return vault
}

func retrieveVault(t *testing.T, app *testApp, vaultID string) vaultAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults/"+vaultID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve vault status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var vault vaultAPIResponse
	decodeJSON(t, resp.Body, &vault)
	return vault
}

func updateVault(t *testing.T, app *testApp, vaultID, body string) vaultAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vaultID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update vault status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var vault vaultAPIResponse
	decodeJSON(t, resp.Body, &vault)
	return vault
}

func archiveVault(t *testing.T, app *testApp, vaultID string) vaultAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vaultID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive vault status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var vault vaultAPIResponse
	decodeJSON(t, resp.Body, &vault)
	return vault
}

func deleteVault(t *testing.T, app *testApp, vaultID string) {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodDelete, "/v1/vaults/"+vaultID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete vault status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listVaults(t *testing.T, app *testApp, query string) vaultPageAPIResponse {
	t.Helper()
	path := "/v1/vaults?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doVaultRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list vaults status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page vaultPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func createVaultCredential(t *testing.T, app *testApp, vaultID, body string) vaultCredentialAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create vault credential status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var credential vaultCredentialAPIResponse
	decodeJSON(t, resp.Body, &credential)
	if credential.ID == "" {
		t.Fatalf("create vault credential returned empty id: %+v", credential)
	}
	return credential
}

func retrieveVaultCredential(t *testing.T, app *testApp, vaultID, credentialID string) vaultCredentialAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodGet, "/v1/vaults/"+vaultID+"/credentials/"+credentialID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("retrieve vault credential status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var credential vaultCredentialAPIResponse
	decodeJSON(t, resp.Body, &credential)
	return credential
}

func updateVaultCredential(t *testing.T, app *testApp, vaultID, credentialID, body string) vaultCredentialAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials/"+credentialID+"?beta=true", strings.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update vault credential status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var credential vaultCredentialAPIResponse
	decodeJSON(t, resp.Body, &credential)
	return credential
}

func archiveVaultCredential(t *testing.T, app *testApp, vaultID, credentialID string) vaultCredentialAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials/"+credentialID+"/archive?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("archive vault credential status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var credential vaultCredentialAPIResponse
	decodeJSON(t, resp.Body, &credential)
	return credential
}

func deleteVaultCredential(t *testing.T, app *testApp, vaultID, credentialID string) {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodDelete, "/v1/vaults/"+vaultID+"/credentials/"+credentialID+"?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete vault credential status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func listVaultCredentials(t *testing.T, app *testApp, vaultID, query string) vaultCredentialPageAPIResponse {
	t.Helper()
	path := "/v1/vaults/" + vaultID + "/credentials?beta=true"
	if query != "" {
		path += "&" + query
	}
	resp := doVaultRequest(t, app, http.MethodGet, path, nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list vault credentials status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var page vaultCredentialPageAPIResponse
	decodeJSON(t, resp.Body, &page)
	return page
}

func validateVaultCredential(t *testing.T, app *testApp, vaultID, credentialID string) vaultCredentialValidationAPIResponse {
	t.Helper()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vaultID+"/credentials/"+credentialID+"/mcp_oauth_validate?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("validate vault credential status = %d, want 200: %s", resp.StatusCode, readAll(t, resp.Body))
	}
	var validation vaultCredentialValidationAPIResponse
	decodeJSON(t, resp.Body, &validation)
	return validation
}

func containsVault(vaults []vaultAPIResponse, id string) bool {
	for _, vault := range vaults {
		if vault.ID == id {
			return true
		}
	}
	return false
}

func containsVaultCredential(credentials []vaultCredentialAPIResponse, id string) bool {
	for _, credential := range credentials {
		if credential.ID == id {
			return true
		}
	}
	return false
}

func webhookJobDataField(t *testing.T, app *testApp, eventType, resourceID, field string) string {
	t.Helper()
	var value string
	if err := app.db.Pool.QueryRow(context.Background(), `
		select jsonb_extract_path_text(payload, 'event', 'data', $3)
		from jobs
		where type = 'webhook_delivery'
			and payload->>'event_type' = $1
			and payload->'event'->'data'->>'id' = $2
		order by created_at desc, id desc
		limit 1
	`, eventType, resourceID, field).Scan(&value); err != nil {
		t.Fatalf("load webhook job data field: %v", err)
	}
	return value
}

func cleanupVaultRows(t *testing.T, app *testApp, vaultID string) {
	t.Helper()
	if _, err := app.db.Pool.Exec(context.Background(), `delete from vault_credentials where vault_external_id = $1`, vaultID); err != nil {
		t.Fatalf("cleanup vault credentials: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `delete from vaults where external_id = $1`, vaultID); err != nil {
		t.Fatalf("cleanup vault: %v", err)
	}
}

func assertVaultSecretsPurged(t *testing.T, app *testApp, vaultID string) {
	t.Helper()
	var count int
	if err := app.db.Pool.QueryRow(context.Background(), `
		select count(*)
		from vault_credentials
		where vault_external_id = $1
			and secret_payload is not null
	`, vaultID).Scan(&count); err != nil {
		t.Fatalf("count vault credential secrets: %v", err)
	}
	if count != 0 {
		t.Fatalf("vault %s has %d credential secret payloads, want 0", vaultID, count)
	}
}

func staticBearerBody(displayName, serverURL, token string) string {
	body, _ := json.Marshal(map[string]any{
		"display_name": displayName,
		"auth": map[string]any{
			"type":           "static_bearer",
			"mcp_server_url": serverURL,
			"token":          token,
		},
	})
	return string(body)
}

func environmentVariableBody(displayName, secretName, secretValue string) string {
	body, _ := json.Marshal(map[string]any{
		"display_name": displayName,
		"auth": map[string]any{
			"type":         "environment_variable",
			"secret_name":  secretName,
			"secret_value": secretValue,
		},
	})
	return string(body)
}
