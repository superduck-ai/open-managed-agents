package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	vaultsapi "github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestPlatformMCPOAuthReauthorizationPostgres(t *testing.T) {
	if os.Getenv("CONFIG_FILE") == "" {
		t.Skip("requires test database CONFIG_FILE")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	svcProvider, err := localkeys.New(localkeys.KeyMaterial{KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	svc := secrets.NewService(svcProvider)
	now := time.Now().UTC()
	vault, err := database.CreateVault(t.Context(), db.Vault{UUID: uuid.NewV4().String(), ExternalID: "vault_" + uuid.NewV4().String(), OrganizationUUID: uuid.NewV4().String(), WorkspaceUUID: uuid.NewV4().String(), DisplayName: "reauthorization test", Metadata: json.RawMessage(`{}`), CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DeleteVault(context.Background(), vault.WorkspaceUUID, vault.ExternalID); err != nil {
			t.Error(err)
		}
	}()
	server := &Server{db: database, vaultSecrets: svc, logger: slog.Default()}
	next := db.VaultCredential{UUID: uuid.NewV4().String(), ExternalID: "vcrd_" + uuid.NewV4().String(), OrganizationUUID: vault.OrganizationUUID, WorkspaceUUID: vault.WorkspaceUUID, VaultUUID: vault.UUID, VaultExternalID: vault.ExternalID, DisplayName: "OAuth", Metadata: json.RawMessage(`{}`), AuthType: "mcp_oauth", CredentialKey: "https://mcp.example/mcp", Auth: json.RawMessage(`{"type":"mcp_oauth","mcp_server_url":"https://mcp.example/mcp"}`), SecretPayload: json.RawMessage(`{"type":"mcp_oauth","access_token":"old-access"}`), CreatedAt: now, UpdatedAt: now}
	current, err := server.savePlatformMCPOAuthCredential(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	// Healthy grants remain protected by the existing duplicate check.
	next.SecretPayload = json.RawMessage(`{"type":"mcp_oauth","access_token":"new-access"}`)
	if _, err := server.savePlatformMCPOAuthCredential(t.Context(), next); !errors.Is(err, db.ErrDuplicate) {
		t.Fatalf("healthy credential overwritten: %v", err)
	}
	if !platformMCPVaultCredentialExists([]db.VaultCredential{current}, next.CredentialKey) {
		t.Fatal("healthy grant must block duplicate authorization")
	}
	// Wrong tenant, parent or envelope must not retire a grant.
	for _, mutate := range []func(*db.VaultCredential){
		func(c *db.VaultCredential) { c.WorkspaceUUID = uuid.NewV4().String() },
		func(c *db.VaultCredential) { c.VaultExternalID = "other-vault" },
		func(c *db.VaultCredential) {
			e := *c.SecretEnvelope
			e.WrappedDEK = []byte("different")
			c.SecretEnvelope = &e
		},
	} {
		wrong := current
		mutate(&wrong)
		if err := database.ClearVaultCredentialSecret(t.Context(), wrong); err != nil {
			t.Fatal(err)
		}
		persisted, err := database.GetVaultCredential(t.Context(), current.WorkspaceUUID, current.VaultExternalID, current.ExternalID)
		if err != nil || persisted.SecretEnvelope == nil {
			t.Fatalf("unrelated retirement changed secret: %v", err)
		}
	}
	// Rename changes version, not grant. Retirement still succeeds without KMS.
	renamed := current
	renamed.DisplayName = "renamed"
	renamed, err = database.UpdateVaultCredential(t.Context(), current.WorkspaceUUID, current.VaultExternalID, current.ExternalID, renamed)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.ClearVaultCredentialSecret(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	invalid, err := database.GetVaultCredential(t.Context(), current.WorkspaceUUID, current.VaultExternalID, current.ExternalID)
	if err != nil || invalid.SecretEnvelope != nil || invalid.ArchivedAt != nil || invalid.DisplayName != "renamed" || invalid.SecretVersion != renamed.SecretVersion+1 {
		t.Fatalf("invalid retirement state: %v", err)
	}
	if platformMCPVaultCredentialExists([]db.VaultCredential{invalid}, next.CredentialKey) {
		t.Fatal("invalid grant cannot start reauthorization")
	}
	next.SecretPayload = json.RawMessage(`{"type":"mcp_oauth","access_token":"new-access"}`)
	restored, err := server.savePlatformMCPOAuthCredential(t.Context(), next)
	if err != nil {
		t.Fatal(err)
	}
	if restored.ExternalID != current.ExternalID || restored.UUID != current.UUID {
		t.Fatal("reauthorization replaced resource identity")
	}
	// Delayed failure of the old refresh must not erase the new grant.
	if err := database.ClearVaultCredentialSecret(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	persisted, err := database.GetVaultCredential(t.Context(), current.WorkspaceUUID, current.VaultExternalID, current.ExternalID)
	if err != nil || persisted.SecretEnvelope == nil {
		t.Fatalf("new grant lost: %v", err)
	}
	testOAuthCallbackSealFailure(t, server, persisted)
	plain, err := svc.Open(t.Context(), secrets.Binding{OrganizationUUID: current.OrganizationUUID, WorkspaceUUID: current.WorkspaceUUID, VaultExternalID: current.VaultExternalID, CredentialExternalID: current.ExternalID}, *persisted.SecretEnvelope)
	defer clear(plain)
	if err != nil || !bytes.Contains(plain, []byte("new-access")) {
		t.Fatalf("reauthorized envelope unusable: %v", err)
	}
}

type callbackFailureProvider struct {
	secrets.KeyProvider
	fail bool
}

func (p *callbackFailureProvider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	if p.fail {
		return secrets.WrappedKey{}, errors.New("injected KMS wrapping failure")
	}
	return p.KeyProvider.WrapDEK(ctx, dek)
}

func testOAuthCallbackSealFailure(t *testing.T, server *Server, credential db.VaultCredential) {
	t.Helper()
	local, err := localkeys.New(localkeys.KeyMaterial{KEK: make([]byte, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &callbackFailureProvider{KeyProvider: local}
	server.vaultSecrets = secrets.NewService(provider)
	if err := server.db.ClearVaultCredentialSecret(t.Context(), credential); err != nil {
		t.Fatal(err)
	}
	var exchanges atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "callback-access", "refresh_token": "callback-refresh", "expires_in": 3600})
	}))
	defer upstream.Close()
	createFlow := func() db.MCPOAuthFlow {
		now := time.Now().UTC()
		flow := db.MCPOAuthFlow{UUID: uuid.NewV4().String(), ExternalID: uuid.NewV4().String(), OrganizationUUID: credential.OrganizationUUID, WorkspaceUUID: credential.WorkspaceUUID, VaultUUID: credential.VaultUUID, VaultExternalID: credential.VaultExternalID, UserUUID: uuid.NewV4().String(), MCPServerURL: credential.CredentialKey, RedirectURL: "http://localhost/oauth/callback", DisplayName: "reauthorize", AuthorizationEndpoint: upstream.URL, TokenEndpoint: upstream.URL, ClientID: "test-client", ClientCredentialSource: "sealed", TokenEndpointAuthMethod: "none", CodeChallengeMethod: "S256", Status: "pending", CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(time.Minute)}
		envelope, err := vaultsapi.SealMCPOAuthFlowSecrets(t.Context(), server.vaultSecrets, flow, "", "test-pkce-verifier")
		if err != nil {
			t.Fatal(err)
		}
		flow.SecretEnvelope = &envelope
		flow, err = server.db.CreateMCPOAuthFlow(t.Context(), flow)
		if err != nil {
			t.Fatal(err)
		}
		return flow
	}
	callback := func(flow db.MCPOAuthFlow) string {
		recorder := httptest.NewRecorder()
		server.handlePlatformMCPVaultAuthCallback(recorder, httptest.NewRequest(http.MethodGet, "/oauth/callback?state="+flow.ExternalID+"&code=test-code", nil))
		return recorder.Body.String()
	}
	failed := createFlow()
	provider.fail = true
	body := callback(failed)
	if !strings.Contains(body, platformMCPVaultAuthVerificationRequestFailed) || exchanges.Load() != 1 {
		t.Fatal("callback did not fail after successful token exchange")
	}
	stored, err := server.db.GetMCPOAuthFlow(t.Context(), failed.ExternalID)
	if err != nil || stored.Status != "failed" || stored.SecretEnvelope != nil {
		t.Fatalf("failed flow can be replayed: %v", err)
	}
	provider.fail = false
	_ = callback(failed)
	if exchanges.Load() != 1 {
		t.Fatal("failed callback replayed consumed authorization code")
	}
	next := createFlow()
	_ = callback(next)
	completed, err := server.db.GetMCPOAuthFlow(t.Context(), next.ExternalID)
	if err != nil || completed.Status != "completed" || completed.CredentialExternalID != credential.ExternalID || exchanges.Load() != 2 {
		t.Fatalf("new authorization did not recover original credential: %v", err)
	}
}
