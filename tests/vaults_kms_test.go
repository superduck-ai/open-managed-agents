package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
	"github.com/superduck-ai/open-managed-agents/internal/testutil/kmsfake"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
	"go.yaml.in/yaml/v3"
)

// Uses the production config loader, provider assembly, signed HTTPS SDK,
// PostgreSQL/API paths and the existing MITM entry point. No cloud account.
func TestVaultAliyunKMSLifecycle(t *testing.T) {
	fake := kmsfake.New(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Vault.MasterKey = config.MasterKeyConfig{Provider: "aliyun_kms", AliyunKMS: &config.AliyunKMSConfig{Endpoint: fake.URL, KeyID: "test-key", AccessKeyID: "offline-id", AccessKeySecret: "offline-secret"}}
	contents, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(t.TempDir(), "kms.yaml")
	if err := os.WriteFile(configFile, contents, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", configFile)
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	service, err := secretservice.New(cfg.Vault.MasterKey, secretservice.WithKMSCA(fake.CA))
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	app := newTestAppWithSecrets(t, &cfg, newFakeStore("kms-lifecycle"), logger, service)
	t.Cleanup(app.close)
	vault := createVault(t, app, `{"display_name":"KMS lifecycle"}`)
	defer cleanupVaultRows(t, app, vault.ID)
	credential := createVaultCredential(t, app, vault.ID, staticBearerBody("kms", "https://mcp.example.com/mcp", "kms-original-token"))
	before, binding := readVaultCredentialEnvelope(t, app, credential.ID)
	if before.KeyProvider != "aliyun_kms" || bytes.Contains(before.Ciphertext, []byte("kms-original-token")) {
		t.Fatal("credential not encrypted with KMS")
	}
	session := createKMSCodeSession(t, app, vault.ID)
	egress := vaults.NewMITMEgress(app.db, service, logger, vaults.NewInjector(app.db, service, logger))
	recorder := &kmsUpstream{}
	// Error-code variants are covered by provider tests; verify business effects once here.
	fake.Failure.Store("Forbidden.NoPermission")
	response := doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials?beta=true", strings.NewReader(staticBearerBody("must fail", "https://failed.example/mcp", "never-persist-this")), defaultTestKey, true)
	assertKMSHTTPFailure(t, response)
	response = doVaultRequest(t, app, http.MethodPost, "/v1/vaults/"+vault.ID+"/credentials/"+credential.ID+"?beta=true", strings.NewReader(`{"auth":{"type":"static_bearer","mcp_server_url":"https://mcp.example.com/mcp"}}`), defaultTestKey, true)
	assertKMSHTTPFailure(t, response)
	if err := kmsMITMRoundTrip(t, egress, session, recorder); !errors.Is(err, vaults.ErrInjectionRejected) {
		t.Fatalf("MITM outage error = %v", err)
	}
	if recorder.calls != 0 {
		t.Fatal("MITM forwarded request after KMS failure")
	}
	unchanged, _ := readVaultCredentialEnvelope(t, app, credential.ID)
	if !bytes.Equal(before.Ciphertext, unchanged.Ciphertext) || !bytes.Equal(before.WrappedDEK, unchanged.WrappedDEK) {
		t.Fatal("failed update changed envelope")
	}
	fake.Failure.Store("")
	if page := listVaultCredentials(t, app, vault.ID, ""); len(page.Data) != 1 {
		t.Fatal("failed create persisted a credential")
	}
	retrieved := retrieveVaultCredential(t, app, vault.ID, credential.ID)
	if bytes.Contains(retrieved.Auth, []byte("kms-original-token")) {
		t.Fatal("read API leaked plaintext")
	}
	plain, err := service.Open(t.Context(), binding, before)
	if err != nil || !bytes.Contains(plain, []byte("kms-original-token")) {
		t.Fatalf("database envelope open: %v", err)
	}
	clear(plain)
	if err := kmsMITMRoundTrip(t, egress, session, recorder); err != nil || recorder.authorization != "Bearer kms-original-token" {
		t.Fatalf("MITM injection: %v", err)
	}
	updateVaultCredential(t, app, vault.ID, credential.ID, `{"auth":{"type":"static_bearer","token":"kms-updated-token"}}`)
	after, _ := readVaultCredentialEnvelope(t, app, credential.ID)
	if bytes.Equal(before.WrappedDEK, after.WrappedDEK) {
		t.Fatal("update reused DEK")
	}
	if err := kmsMITMRoundTrip(t, egress, session, recorder); err != nil || recorder.authorization != "Bearer kms-updated-token" {
		t.Fatalf("updated MITM injection: %v", err)
	}
	// A partial auth update preserves the secret through Open/merge/Seal.
	updateVaultCredential(t, app, vault.ID, credential.ID, `{"display_name":"preserved","auth":{"type":"static_bearer","mcp_server_url":"https://mcp.example.com/mcp"}}`)
	// Actual persisted wrapped-key tampering must block injection too.
	if _, err := app.pool.Exec(t.Context(), `update vault_credentials set wrapped_dek = '\x00'::bytea where external_id = $1`, credential.ID); err != nil {
		t.Fatal(err)
	}
	previousCalls := recorder.calls
	if err := kmsMITMRoundTrip(t, egress, session, recorder); !errors.Is(err, vaults.ErrInjectionRejected) || recorder.calls != previousCalls {
		t.Fatal("tampered persisted key reached upstream")
	}
	// Archive is a deletion of secret material and must work during a KMS outage.
	fake.Failure.Store("Rejected.Disabled")
	archiveVaultCredential(t, app, vault.ID, credential.ID)
	if vaultCredentialHasEnvelope(t, app, credential.ID) {
		t.Fatal("archive retained encrypted secret")
	}
	fake.Failure.Store("")
	cascade := createVaultCredential(t, app, vault.ID, staticBearerBody("cascade", "https://cascade.example/mcp", "kms-cascade-token"))
	fake.Failure.Store("Rejected.Disabled")
	archiveVault(t, app, vault.ID)
	if vaultCredentialHasEnvelope(t, app, cascade.ID) {
		t.Fatal("vault archive retained encrypted secret")
	}
	for _, secret := range []string{"kms-original-token", "kms-updated-token", "never-persist-this", "offline-secret", "untrusted-response-containing-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("logs leaked a secret or raw KMS response")
		}
	}
}

func assertKMSHTTPFailure(t *testing.T, response *http.Response) {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode < 500 || bytes.Contains(body, []byte("untrusted-response")) || bytes.Contains(body, []byte("never-persist-this")) {
		t.Fatalf("KMS failure returned unsafe status %d", response.StatusCode)
	}
}

type kmsUpstream struct {
	calls         int
	authorization string
}

func (r *kmsUpstream) RoundTrip(request *http.Request) (*http.Response, error) {
	r.calls++
	r.authorization = request.Header.Get("Authorization")
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: request}, nil
}
func kmsMITMRoundTrip(t *testing.T, egress *vaults.MITMEgress, session vaults.EgressSession, recorder *kmsUpstream) error {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://mcp.example.com/mcp", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	transport, err := egress.Prepare(t.Context(), session, "mcp.example.com:443", request, recorder)
	if err != nil {
		return err
	}
	response, err := transport.RoundTrip(request)
	if response != nil {
		response.Body.Close()
	}
	return err
}
func createKMSCodeSession(t *testing.T, app *testApp, vaultID string) vaults.EgressSession {
	t.Helper()
	scope := getDefaultDBIDs(t, app.pool)
	input := filestoreSessionCreateInput(scope.OrganizationUUID, scope.WorkspaceUUID, scope.APIKeyUUID)
	input.Session.VaultIDs = []string{vaultID}
	session, _, _, _, err := app.db.CreateSession(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = app.db.DeleteSession(context.Background(), scope.WorkspaceUUID, session.ExternalID) })
	code, err := app.db.CreateCodeSession(t.Context(), db.CreateCodeSessionInput{
		ExternalID: "cse_kms_" + session.ExternalID, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID,
		SessionUUID: session.UUID, SessionExternalID: session.ExternalID, EnvironmentUUID: session.EnvironmentUUID, EnvironmentExternalID: session.EnvironmentExternalID,
		Status: "active", Model: "test", PermissionMode: "default", Metadata: json.RawMessage(`{}`), OAuthAccessTokenHash: auth.HashAPIKey(session.ExternalID), CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return vaults.EgressSession{CodeSessionExternalID: code.ExternalID, OrganizationUUID: scope.OrganizationUUID, WorkspaceUUID: scope.WorkspaceUUID}
}
