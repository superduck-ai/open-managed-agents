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
	"uuid"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	"github.com/superduck-ai/open-managed-agents/internal/vaults"
)

func TestOAuthLocalVersionRotationPostgres(t *testing.T) {
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
	source, _ := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: oldBytes}, nil)
	target, _ := localkeys.New(localkeys.KeyMaterial{Version: 2, KEK: bytes.Repeat([]byte{2}, 32)}, []localkeys.KeyMaterial{{Version: 1, KEK: oldBytes}})
	oldService, service := secrets.NewService(source), secrets.NewService(target)
	provider, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
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
	stale := rows[0]
	next, _, err := migrateDocument(t.Context(), service, "platform_oauth_providers", stale, "local", 1, true)
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
	current, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil || current.Version != provider.Version+1 || current.ClientIDEnvelope.KeyVersion != 2 || current.ClientSecretEnvelope.KeyVersion != 2 || !bytes.Equal(current.ClientIDEnvelope.Ciphertext, provider.ClientIDEnvelope.Ciphertext) {
		t.Fatal("双信封版本轮换失败")
	}
	resolver := platformoauth.NewResolver(database, service, slog.New(slog.DiscardHandler))
	snapshot, err := resolver.ResolveEnvOAuthProvider(t.Context(), "builtin:feishu")
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
	if _, err := vaults.ExchangeOAuthTokenEndpoint(t.Context(), upstream.Client(), vaults.OAuthTokenEndpointExchange{TokenEndpoint: upstream.URL, ClientID: snapshot.ClientID, ClientSecret: snapshot.ClientSecret, TokenEndpointAuthMethod: "client_secret_post", TokenEndpointEncoding: "json", Form: url.Values{"client_id": {snapshot.ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {"fixture-refresh"}}}); err != nil {
		t.Fatal(err)
	}
}
