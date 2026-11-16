//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestOAuthCrossProviderMigrationPostgres(t *testing.T) {
	const builtinKey = "feishu"
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不代表通过")
	}
	admin, err := db.OpenExisting(t.Context(), config.Config{Database: config.DatabaseConfig{URL: address}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "oauth_rotation_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.SQLDB().ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.SQLDB().ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") }()
	parsed, _ := url.Parse(address)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := db.OpenExisting(t.Context(), config.Config{Database: config.DatabaseConfig{URL: parsed.String()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	oldBytes := bytes.Repeat([]byte{1}, 32)
	source, _ := localkeys.New(localkeys.KeyMaterial{KEK: oldBytes}, nil)
	target, _ := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{2}, 32)}, nil)
	oldService, service := secrets.NewService(source), secrets.NewService(&targetProvider{KeyProvider: target}, source)
	provider, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), builtinKey)
	if err != nil {
		t.Fatal(err)
	}
	provider.ClientIDEnvelope, err = platformoauth.SealClientField(t.Context(), oldService, platformoauth.ProviderClientIDBinding(provider.ExternalID), "rotation-client")
	if err != nil {
		t.Fatal(err)
	}
	provider.ClientSecretEnvelope, err = platformoauth.SealClientField(t.Context(), oldService, platformoauth.ProviderSecretBinding(provider.ExternalID), "rotation-secret")
	if err != nil {
		t.Fatal(err)
	}
	provider, err = database.UpdatePlatformOAuthProvider(t.Context(), provider)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.ListSecretMigrationPage(t.Context(), "platform_oauth_providers", "")
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	var stale db.SecretMigrationRecord
	for _, row := range rows {
		if row.UUID == provider.UUID {
			stale = row
		}
	}
	next, _, err := migrateDocument(t.Context(), service, "platform_oauth_providers", stale, "local", true)
	if err != nil {
		t.Fatal(err)
	}
	wrong := stale
	wrong.WorkspaceUUID = "other"
	if err := database.SaveMigratedSecret(t.Context(), "platform_oauth_providers", wrong, next); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("固定平台 scope 未校验")
	}
	if err := database.SaveMigratedSecret(t.Context(), "platform_oauth_providers", stale, next); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveMigratedSecret(t.Context(), "platform_oauth_providers", stale, next); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("旧双信封 CAS 未拒绝")
	}
	current, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), builtinKey)
	if err != nil || current.Version != provider.Version+1 || current.ClientIDEnvelope.KeyProvider != "hashicorp_vault" || current.ClientSecretEnvelope.KeyProvider != "hashicorp_vault" || !bytes.Equal(current.ClientIDEnvelope.Ciphertext, provider.ClientIDEnvelope.Ciphertext) {
		t.Fatal("双信封跨提供方迁移失败")
	}
	testCustomVerifierRotation(t, database, oldService, service)
	resolver := platformoauth.NewResolver(database, service, slog.New(slog.DiscardHandler))
	snapshot, err := resolver.ResolveEnvOAuthProvider(t.Context(), "builtin:"+builtinKey)
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			GrantType    string `json:"grant_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.ClientID != "rotation-client" || input.ClientSecret != "rotation-secret" || input.GrantType != "refresh_token" {
			t.Error("轮换后客户端无法用于刷新")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "new-access", "expires_in": 3600})
	}))
	defer upstream.Close()
	if _, err := vaults.ExchangeOAuthTokenEndpoint(t.Context(), upstream.Client(), vaults.OAuthTokenEndpointExchange{TokenEndpoint: upstream.URL, ClientID: snapshot.ClientID, ClientSecret: snapshot.ClientSecret, TokenEndpointAuthMethod: snapshot.TokenEndpointAuthMethod, TokenEndpointEncoding: snapshot.TokenEndpointEncoding, Form: url.Values{"client_id": {snapshot.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {"fixture-refresh"}}}); err != nil {
		t.Fatal(err)
	}
}

func testCustomVerifierRotation(t *testing.T, database *db.DB, original, target *secrets.Service) {
	t.Helper()
	flow := db.EnvOAuthFlow{UUID: uuid.NewV4().String(), ExternalID: uuid.NewV4().String(), OrganizationUUID: uuid.NewV4().String(), WorkspaceUUID: uuid.NewV4().String(), VaultUUID: uuid.NewV4().String(), VaultExternalID: "vlt_custom", ProviderKey: "builtin:feishu", SecretName: "CUSTOM_TOKEN", RedirectURL: "https://app.test/callback", AuthorizeEndpoint: "https://custom.test/authorize", TokenEndpoint: "https://custom.test/token", Scope: "read", TokenEndpointAuthMethod: "client_secret_basic", TokenEndpointEncoding: "form", NetworkingType: "unrestricted", Status: "pending", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Minute)}
	envelope, err := platformoauth.SealClientField(t.Context(), original, platformoauth.FlowCodeVerifierBinding(flow), "synthetic-verifier")
	if err != nil {
		t.Fatal(err)
	}
	flow.CodeVerifierEnvelope = envelope
	if _, err := database.CreateEnvOAuthFlow(t.Context(), flow); err != nil {
		t.Fatal(err)
	}
	rows, err := database.ListSecretMigrationPage(t.Context(), "env_oauth_flows", "")
	if err != nil || len(rows) != 1 {
		t.Fatal("Custom verifier 没有进入轮换扫描", err)
	}
	next, count, err := migrateOAuthDocument(t.Context(), target, "env_oauth_flows", rows[0], "local", true)
	if err != nil || count != 1 {
		t.Fatal("Custom verifier 轮换失败", err)
	}
	if err := database.SaveMigratedSecret(t.Context(), "env_oauth_flows", rows[0], next); err != nil {
		t.Fatal(err)
	}
	if err := database.SaveMigratedSecret(t.Context(), "env_oauth_flows", rows[0], next); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("Custom verifier 轮换未校验 CAS")
	}
	stored, err := database.GetEnvOAuthFlow(t.Context(), flow.ExternalID)
	if err != nil || stored.ClientIDEnvelope != nil || stored.CodeVerifierEnvelope.KeyProvider != "hashicorp_vault" {
		t.Fatal("Custom verifier 扫描/NULL 错误", err)
	}
	verifier, err := platformoauth.OpenClientField(t.Context(), target, platformoauth.FlowCodeVerifierBinding(stored), stored.CodeVerifierEnvelope)
	if err != nil || verifier != "synthetic-verifier" {
		t.Fatal("Custom verifier 轮换不可解密", err)
	}
}
