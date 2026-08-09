package tests

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/ids"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"

	"github.com/google/uuid"
)

func TestVaultCredentialEncryptedAtRest(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-encryption-bucket"))
	defer app.close()
	vault := createVault(t, app, `{"display_name":"vault encryption at rest"}`)
	defer cleanupVaultRows(t, app, vault.ID)

	assertVaultCredentialsHaveNoSecretPayloadColumn(t, app)

	created := createVaultCredential(t, app, vault.ID, staticBearerBody("encrypted", "https://mcp.roundtrip.example/sse", "round-trip-secret"))
	if strings.Contains(string(created.Auth), "round-trip-secret") {
		t.Fatalf("API response leaked the secret: %s", created.Auth)
	}

	env, binding := readVaultCredentialEnvelope(t, app, created.ID)
	if len(env.Ciphertext) == 0 || len(env.Nonce) == 0 || len(env.WrappedDEK) == 0 {
		t.Fatalf("envelope columns not populated: %+v", env)
	}
	opened, err := app.vaultSecrets.Open(context.Background(), binding, env)
	if err != nil || !strings.Contains(string(opened), "round-trip-secret") {
		t.Fatalf("open sealed credential: %v plaintext=%q", err, opened)
	}
}

func TestVaultCredentialArchiveClearsEnvelope(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-archive-bucket"))
	defer app.close()
	vault := createVault(t, app, `{"display_name":"vault archive envelope"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	created := createVaultCredential(t, app, vault.ID, staticBearerBody("archive envelope", "https://mcp.archive-env.example/sse", "archive-secret"))
	archiveVaultCredential(t, app, vault.ID, created.ID)
	if vaultCredentialHasEnvelope(t, app, created.ID) {
		t.Fatal("archived credential still carries envelope columns")
	}
}

func TestVaultCredentialUpdateSecretVersionCAS(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-cas-bucket"))
	defer app.close()
	ctx := context.Background()
	vault := createVault(t, app, `{"display_name":"vault cas"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	created := createVaultCredential(t, app, vault.ID, staticBearerBody("cas credential", "https://mcp.cas.example/sse", "cas-secret"))

	workspaceUUID := defaultWorkspaceUUID(t, app)
	current, err := app.db.GetVaultCredential(ctx, workspaceUUID, vault.ID, created.ID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	stale := current
	stale.DisplayName = "cas credential updated"
	updated, err := app.db.UpdateVaultCredential(ctx, workspaceUUID, vault.ID, created.ID, stale)
	if err != nil || updated.SecretVersion != 1 {
		t.Fatalf("first update = (%+v, %v)", updated, err)
	}
	stale.DisplayName = "cas credential stale"
	if _, err := app.db.UpdateVaultCredential(ctx, workspaceUUID, vault.ID, created.ID, stale); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatalf("stale-version update error = %v, want ErrVersionConflict", err)
	}
}

func TestVaultCredentialUpdateMissingEnvelopeBehavior(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-missing-envelope-bucket"))
	defer app.close()
	vault := createVault(t, app, `{"display_name":"vault missing envelope"}`)
	defer cleanupVaultRows(t, app, vault.ID)

	credentialID := insertEnvelopeLessCredential(t, app, vault.ID, "missing-envelope-key")
	omitBody, _ := json.Marshal(map[string]any{"auth": map[string]any{"type": "static_bearer"}})
	omitResp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials/"+credentialID+"?beta=true", bytes.NewReader(omitBody), defaultTestKey, true)
	defer omitResp.Body.Close()
	if omitResp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing-envelope update status = %d, want 400: %s", omitResp.StatusCode, readAll(t, omitResp.Body))
	}

	resealBody, _ := json.Marshal(map[string]any{
		"auth": map[string]any{"type": "static_bearer", "token": "replacement-token"},
	})
	resealResp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials/"+credentialID+"?beta=true", bytes.NewReader(resealBody), defaultTestKey, true)
	defer resealResp.Body.Close()
	if resealResp.StatusCode != http.StatusOK || !vaultCredentialHasEnvelope(t, app, credentialID) {
		t.Fatalf("missing-envelope reseal status = %d hasEnvelope=%v", resealResp.StatusCode, vaultCredentialHasEnvelope(t, app, credentialID))
	}
}

func TestVaultCredentialUpdateOpenFailureReturns5xx(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-tamper-open-bucket"))
	defer app.close()
	vault := createVault(t, app, `{"display_name":"vault tamper open"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	created := createVaultCredential(t, app, vault.ID, staticBearerBody("tamper open", "https://mcp.tamper.example/sse", "tamper-secret"))
	tamperVaultCredentialCiphertext(t, app, created.ID)

	body, _ := json.Marshal(map[string]any{"auth": map[string]any{"type": "static_bearer"}})
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials/"+created.ID+"?beta=true", bytes.NewReader(body), defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode < 500 {
		t.Fatalf("tampered-envelope update status = %d, want 5xx: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func TestVaultCredentialBackfillEndpointGone(t *testing.T) {
	app := newTestAppWithStore(t, nil, newFakeStore("vaults-backfill-gone-bucket"))
	defer app.close()
	resp := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/backfill_secrets?beta=true", nil, defaultTestKey, true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("backfill endpoint status = %d, want 404: %s", resp.StatusCode, readAll(t, resp.Body))
	}
}

func defaultWorkspaceUUID(t *testing.T, app *testApp) string {
	t.Helper()
	var id string
	if err := app.pool.QueryRow(context.Background(), `select uuid::text from workspaces where external_id = 'workspace_default'`).Scan(&id); err != nil {
		t.Fatalf("load default workspace uuid: %v", err)
	}
	return id
}

func assertVaultCredentialsHaveNoSecretPayloadColumn(t *testing.T, app *testApp) {
	t.Helper()
	var exists bool
	if err := app.pool.QueryRow(context.Background(), `
		select exists (
			select 1 from information_schema.columns
			where table_schema = 'public' and table_name = 'vault_credentials' and column_name = 'secret_payload'
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("check secret_payload column: %v", err)
	}
	if exists {
		t.Fatal("vault_credentials.secret_payload still exists")
	}
}

func vaultCredentialHasEnvelope(t *testing.T, app *testApp, credentialID string) bool {
	t.Helper()
	var present bool
	if err := app.pool.QueryRow(context.Background(), `select ciphertext is not null from vault_credentials where external_id = $1`, credentialID).Scan(&present); err != nil {
		t.Fatalf("check credential envelope: %v", err)
	}
	return present
}

func readVaultCredentialEnvelope(t *testing.T, app *testApp, credentialID string) (secrets.Envelope, secrets.Binding) {
	t.Helper()
	var (
		orgUUID, wsUUID, vaultExt, credExt string
		ciphertext, nonce, wrappedDEK      []byte
		formatVersion                      sql.NullInt32
		keyProvider                        sql.NullString
		keyVersion                         sql.NullInt64
	)
	err := app.pool.QueryRow(context.Background(), `
		select organization_uuid::text, workspace_uuid::text, vault_external_id, external_id,
			ciphertext, nonce, wrapped_dek, format_version, key_provider, key_version
		from vault_credentials where external_id = $1
	`, credentialID).Scan(&orgUUID, &wsUUID, &vaultExt, &credExt, &ciphertext, &nonce, &wrappedDEK, &formatVersion, &keyProvider, &keyVersion)
	if err != nil {
		t.Fatalf("read credential envelope: %v", err)
	}
	return secrets.Envelope{
			Ciphertext: ciphertext, Nonce: nonce, WrappedDEK: wrappedDEK,
			FormatVersion: int(formatVersion.Int32), KeyProvider: keyProvider.String, KeyVersion: keyVersion.Int64,
		}, secrets.Binding{
			OrganizationUUID: orgUUID, WorkspaceUUID: wsUUID,
			VaultExternalID: vaultExt, CredentialExternalID: credExt,
		}
}

func insertEnvelopeLessCredential(t *testing.T, app *testApp, vaultID, credentialKey string) string {
	t.Helper()
	var orgUUID, wsUUID, vaultUUID string
	if err := app.pool.QueryRow(context.Background(), `
		select organization_uuid::text, workspace_uuid::text, uuid::text from vaults where external_id = $1
	`, vaultID).Scan(&orgUUID, &wsUUID, &vaultUUID); err != nil {
		t.Fatalf("load vault for envelope-less insert: %v", err)
	}
	credentialID, err := ids.New("vcrd_")
	if err != nil {
		t.Fatalf("generate envelope-less credential id: %v", err)
	}
	_, err = app.pool.Exec(context.Background(), `
		insert into vault_credentials (
			uuid, external_id, organization_uuid, workspace_uuid, vault_uuid, vault_external_id,
			created_by_api_key_uuid, display_name, metadata, auth_type, credential_key,
			auth, created_at, updated_at
		) values ($1, $2, $3::uuid, $4::uuid, $5::uuid, $6, NULL, 'missing envelope', '{}'::jsonb, 'static_bearer', $7,
			'{"type":"static_bearer","mcp_server_url":"https://mcp.missing-env.example/sse"}'::jsonb, now(), now())
	`, uuid.NewString(), credentialID, orgUUID, wsUUID, vaultUUID, vaultID, credentialKey)
	if err != nil {
		t.Fatalf("insert envelope-less credential: %v", err)
	}
	return credentialID
}

func tamperVaultCredentialCiphertext(t *testing.T, app *testApp, credentialID string) {
	t.Helper()
	tag, err := app.pool.Exec(context.Background(), `
		update vault_credentials
		set ciphertext = set_byte(ciphertext, 0, (get_byte(ciphertext, 0) # 1))
		where external_id = $1 and ciphertext is not null
	`, credentialID)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("tamper credential ciphertext: %v rows=%d", err, tag.RowsAffected())
	}
}
