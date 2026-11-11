//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	"log/slog"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"uuid"
)

type targetProvider struct {
	secrets.KeyProvider
	calls  int
	failAt int
}

func (p *targetProvider) Name() string { return "hashicorp_vault" }
func (p *targetProvider) WrapDEK(ctx context.Context, dek []byte) (secrets.WrappedKey, error) {
	p.calls++
	if p.calls == p.failAt {
		return secrets.WrappedKey{}, errors.New("injected failure")
	}
	return p.KeyProvider.WrapDEK(ctx, dek)
}

func TestMigrationAllStoragePostgres(t *testing.T) {
	for _, externalID := range []string{"poap_fixture", "poap_FridayBuiltinOAuthClient"} {
		t.Run(externalID, func(t *testing.T) { testMigrationAllStoragePostgres(t, externalID) })
	}
}

func testMigrationAllStoragePostgres(t *testing.T, platformExternalID string) {
	t.Helper()
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不代表通过")
	}
	cfg := config.Config{Database: config.DatabaseConfig{URL: address}}
	database, err := db.OpenExisting(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	schema := "migration_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err = database.SQLDB().ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := database.SQLDB().ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	u, err := url.Parse(cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	cfg.Database.URL = u.String()
	isolated, err := db.OpenExisting(t.Context(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer isolated.Close()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := isolated.SQLDB().ExecContext(t.Context(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	org, workspace, tunnel := uuid.NewV4().String(), uuid.NewV4().String(), uuid.NewV4().String()
	oldKey, _ := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{1}, 32)}, nil)
	newKey, _ := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{2}, 32)}, nil)
	target := &targetProvider{KeyProvider: newKey}
	service := secrets.NewService(target, oldKey)
	binding := secrets.Binding{OrganizationUUID: org, WorkspaceUUID: workspace, VaultExternalID: "vault", CredentialExternalID: "credential"}
	original, err := secrets.NewService(oldKey).Seal(t.Context(), binding, []byte("fixture-secret"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(original)
	stored := fmt.Sprintf(`{"envelope":%s,"extra":"preserved"}`, encoded)
	exec(`CREATE TABLE mcp_tunnels (uuid uuid PRIMARY KEY, organization_uuid uuid, workspace_uuid uuid)`)
	exec(`INSERT INTO mcp_tunnels VALUES ($1,$2,$3)`, tunnel, org, workspace)
	oauthEnvelopes := make(map[string]secrets.Envelope)
	oauthBindings := make(map[string]secrets.Binding)
	for _, kind := range db.SecretMigrationKinds() {
		if kind == "platform_oauth_providers" || kind == "env_oauth_flows" {
			externalID := platformExternalID
			binding := platformoauth.ProviderClientIDBinding(externalID)
			columns, values := "", ""
			args := []any{uuid.NewV4().String(), externalID}
			if kind == "env_oauth_flows" {
				binding = platformoauth.FlowClientIDBinding(db.EnvOAuthFlow{OrganizationUUID: org, WorkspaceUUID: workspace, ExternalID: externalID})
				columns = ",organization_uuid,workspace_uuid"
				values = ",$4,$5"
			}
			envelope, err := secrets.NewService(oldKey).Seal(t.Context(), binding, []byte("fixture-secret"))
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(envelope)
			args = append(args, string(encoded))
			extra := ""
			if kind == "platform_oauth_providers" {
				extra = ",client_secret_envelope jsonb"
			}
			schemaScope := ""
			if kind == "env_oauth_flows" {
				schemaScope = ",organization_uuid uuid,workspace_uuid uuid,code_verifier_envelope jsonb"
				args = append(args, org, workspace)
			}
			exec("CREATE TABLE " + kind + " (uuid uuid PRIMARY KEY, external_id text, version bigint DEFAULT 1, updated_at timestamptz, client_id_envelope jsonb" + extra + schemaScope + ")")
			exec("INSERT INTO "+kind+" (uuid,external_id,client_id_envelope"+columns+") VALUES ($1,$2,$3"+values+")", args...)
			if kind == "platform_oauth_providers" {
				secret, err := secrets.NewService(oldKey).Seal(t.Context(), platformoauth.ProviderSecretBinding(externalID), []byte("fixture-secret"))
				if err != nil {
					t.Fatal(err)
				}
				secretJSON, _ := json.Marshal(secret)
				exec("UPDATE "+kind+" SET client_secret_envelope=$1", string(secretJSON))
			}
			oauthEnvelopes[kind], oauthBindings[kind] = envelope, binding
			continue
		}
		scope := "organization_uuid uuid, workspace_uuid uuid,"
		if kind == "mcp_tunnel_token_versions" {
			scope = "tunnel_uuid uuid,"
		}
		timestamps := " version bigint DEFAULT 1, updated_at timestamptz,"
		if kind == "channel_workspace_credentials" {
			timestamps = ""
		}
		data := "ciphertext bytea, nonce bytea, wrapped_dek bytea, format_version integer, key_provider text, key_version bigint"
		if kind == "session_resources" {
			data = "secret_payload jsonb"
		}
		if kind == "deployments" {
			data = "resource_secrets jsonb"
		}
		if kind == "channel_workspace_credentials" {
			data = "envelope jsonb"
		}
		exec("CREATE TABLE " + kind + " (uuid uuid PRIMARY KEY," + scope + timestamps + data + ")")
		switch kind {
		case "channel_workspace_credentials":
			exec("INSERT INTO "+kind+" (uuid,organization_uuid,workspace_uuid,envelope) VALUES ($1,$2,$3,$4)", uuid.NewV4().String(), org, workspace, string(encoded))
		case "session_resources", "deployments":
			column, document := "secret_payload", stored
			if kind == "deployments" {
				column = "resource_secrets"
				document = `{"0":` + stored + `,"1":null,"2":` + stored + `,"3":{}}`
			}
			exec("INSERT INTO "+kind+" (uuid,organization_uuid,workspace_uuid,"+column+") VALUES ($1,$2,$3,$4)", uuid.NewV4().String(), org, workspace, document)
		case "mcp_tunnel_token_versions":
			exec("INSERT INTO "+kind+" (uuid,tunnel_uuid,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)", uuid.NewV4().String(), tunnel, original.Ciphertext, original.Nonce, original.WrappedDEK, original.FormatVersion, original.KeyProvider, original.KeyVersion)
		default:
			count := 1
			if kind == "vault_credentials" {
				count = 105
			}
			exec("INSERT INTO "+kind+" (uuid,organization_uuid,workspace_uuid,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version) SELECT gen_random_uuid(),$1,$2,$3,$4,$5,$6,$7,$8 FROM generate_series(1,$9)", org, workspace, original.Ciphertext, original.Nonce, original.WrappedDEK, original.FormatVersion, original.KeyProvider, original.KeyVersion, count)
		}
	}
	for i, fixture := range []struct{ kind, column, raw string }{
		{"session_resources", "secret_payload", `{}`},
		{"session_resources", "secret_payload", `null`},
		{"deployments", "resource_secrets", `[]`},
		{"deployments", "resource_secrets", `{}`},
	} {
		exec("INSERT INTO "+fixture.kind+" (uuid,organization_uuid,workspace_uuid,"+fixture.column+") VALUES ($1,$2,$3,$4)",
			fmt.Sprintf("ffffffff-ffff-ffff-ffff-%012d", i+1), org, workspace, fixture.raw)
	}
	exec("INSERT INTO session_resources (uuid,organization_uuid,workspace_uuid,secret_payload) VALUES ($1,$2,$3,$4)",
		"ffffffff-ffff-ffff-ffff-000000000005", org, workspace, `{"authorization_token":"test-secret","extra":"preserved"}`)
	legacyEnvelope := strings.NewReplacer(`"ciphertext"`, `"Ciphertext"`, `"nonce"`, `"Nonce"`, `"wrapped_dek"`, `"WrappedDEK"`, `"format_version"`, `"FormatVersion"`, `"key_provider"`, `"KeyProvider"`, `"key_version"`, `"KeyVersion"`).Replace(stored)
	exec("INSERT INTO session_resources (uuid,organization_uuid,workspace_uuid,secret_payload) VALUES ($1,$2,$3,$4)",
		"ffffffff-ffff-ffff-ffff-000000000006", org, workspace, legacyEnvelope)
	exec("INSERT INTO vault_credentials (uuid,organization_uuid,workspace_uuid) VALUES ($1,$2,$3)", "ffffffff-ffff-ffff-ffff-ffffffffffff", org, workspace)
	badUUIDs := []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"}
	exec("INSERT INTO vault_credentials (uuid,organization_uuid,workspace_uuid,ciphertext,nonce,wrapped_dek,format_version,key_provider,key_version) VALUES ($1,$2,$3,$4,$5,$6,1,'local',1)", badUUIDs[0], org, workspace, original.Ciphertext, original.Nonce, []byte("invalid-wrapped"))
	exec("INSERT INTO vault_credentials (uuid,organization_uuid,workspace_uuid,nonce) VALUES ($1,$2,$3,$4)", badUUIDs[1], org, workspace, original.Nonce)
	var output bytes.Buffer
	if err = migrate(t.Context(), isolated, service, "local", "hashicorp_vault", 0, false, &output, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "3 records failed") {
		t.Fatalf("preview failed to report invalid records: %v", err)
	}
	if target.calls != 0 || !regexp.MustCompile(`(?m)^Total\s+114\s+4\s+3$`).MatchString(output.String()) || !strings.Contains(output.String(), "missing key_provider") || !strings.Contains(output.String(), "legacy plaintext") || strings.Contains(output.String(), "test-secret") {
		t.Fatalf("preview counts or safe failure report incorrect: %s", output.String())
	}
	if strings.Count(output.String(), "FAILED ") != 3 || !strings.Contains(output.String(), "Failure reasons:") || !strings.Contains(output.String(), "FAILED vault_credentials "+badUUIDs[0]) || strings.Index(output.String(), "Total") > strings.Index(output.String(), "Failed records:") {
		t.Fatal("failed records must follow the summary with safe record identifiers")
	}
	target.failAt = 2
	output.Reset()
	if err = migrate(t.Context(), isolated, service, "local", "hashicorp_vault", 0, true, &output, slog.New(slog.DiscardHandler)); err == nil || !errors.Is(err, secrets.ErrMigrationTarget) || !regexp.MustCompile(`(?m)^Total \(partial\)\s+1\s+0\s+3$`).MatchString(output.String()) || strings.Count(output.String(), "FAILED ") != 3 {
		t.Fatal("provider failure lost completed progress")
	}
	rows, err := isolated.ListSecretMigrationPage(t.Context(), "vault_credentials", "")
	if err != nil {
		t.Fatal(err)
	}
	var first, second secrets.Envelope
	_ = json.Unmarshal(rows[2].Document, &first)
	_ = json.Unmarshal(rows[3].Document, &second)
	if first.KeyProvider != "hashicorp_vault" || second.KeyProvider != "local" {
		t.Fatal("failure overwrote the failed record or lost completed progress")
	}
	target.failAt = 0
	output.Reset()
	if err = migrate(t.Context(), isolated, service, "local", "hashicorp_vault", 0, true, &output, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "3 records failed") {
		t.Fatalf("apply failed to report invalid rows: %v", err)
	}
	if !regexp.MustCompile(`(?m)^Total\s+113\s+5\s+3$`).MatchString(output.String()) {
		t.Fatal("apply summary counted failed writes")
	}
	var converted json.RawMessage
	if err := isolated.SQLDB().QueryRowContext(t.Context(), "SELECT secret_payload->'envelope' FROM session_resources WHERE uuid='ffffffff-ffff-ffff-ffff-000000000006'").Scan(&converted); err != nil {
		t.Fatal(err)
	}
	var convertedEnvelope secrets.Envelope
	if err := json.Unmarshal(converted, &convertedEnvelope); err != nil {
		t.Fatal(err)
	}
	plain, err := secrets.NewService(target).Open(t.Context(), binding, convertedEnvelope)
	if err != nil || string(plain) != "fixture-secret" || !bytes.Equal(convertedEnvelope.Ciphertext, original.Ciphertext) {
		t.Fatal("legacy envelope did not migrate correctly")
	}
	clear(plain)
	calls := target.calls
	if calls != 117 {
		t.Fatalf("wrap attempts=%d; want 113 envelopes and one failure", calls)
	}
	output.Reset()
	if err = migrate(t.Context(), isolated, service, "local", "hashicorp_vault", 0, true, &output, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "3 records failed") || target.calls != calls || !regexp.MustCompile(`(?m)^Total\s+0\s+118\s+3$`).MatchString(output.String()) {
		t.Fatalf("rerun is not idempotent: %v", err)
	}
	var unchanged bool
	if err := isolated.SQLDB().QueryRowContext(t.Context(), "SELECT secret_payload=$1::jsonb FROM session_resources WHERE uuid=$2", `{"authorization_token":"test-secret","extra":"preserved"}`, "ffffffff-ffff-ffff-ffff-000000000005").Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("legacy plaintext was modified")
	}
	for _, id := range badUUIDs {
		exec("DELETE FROM vault_credentials WHERE uuid=$1", id)
	}
	exec("DELETE FROM session_resources WHERE uuid=$1", "ffffffff-ffff-ffff-ffff-000000000005")
	output.Reset()
	if err := migrate(t.Context(), isolated, service, "local", "hashicorp_vault", 0, true, &output, slog.New(slog.DiscardHandler)); err != nil || !strings.Contains(output.String(), "Nothing to migrate") || strings.Contains(output.String(), "Failed records:") || target.calls != calls {
		t.Fatal("zero-match run changed data")
	}
	for _, kind := range db.SecretMigrationKinds() {
		rows, err := isolated.ListSecretMigrationPage(t.Context(), kind, "")
		if err != nil || len(rows) == 0 {
			t.Fatalf("%s: %v", kind, err)
		}
		original := original
		binding := binding
		raw := rows[0].Document
		if kind == "platform_oauth_providers" || kind == "env_oauth_flows" {
			var document oauthMigrationDocument
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatal(err)
			}
			raw, _ = json.Marshal(document.ClientIDEnvelope)
			original, binding = oauthEnvelopes[kind], oauthBindings[kind]
		}
		if kind == "deployments" {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			raw = fields["0"]
		}
		if kind == "session_resources" || kind == "deployments" {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			if string(fields["extra"]) != `"preserved"` {
				t.Fatal("lost unknown JSON field")
			}
			raw = fields["envelope"]
		}
		var result secrets.Envelope
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(result.Ciphertext, original.Ciphertext) || !bytes.Equal(result.Nonce, original.Nonce) {
			t.Fatal("payload changed")
		}
		plain, openErr := secrets.NewService(target).Open(t.Context(), binding, result)
		clear(plain)
		if openErr != nil {
			t.Fatalf("%s no longer readable: %v", kind, openErr)
		}
		wrong := rows[0]
		wrong.WorkspaceUUID = uuid.NewV4().String()
		if err = isolated.SaveMigratedSecret(t.Context(), kind, wrong, rows[0].Document); !errors.Is(err, db.ErrVersionConflict) {
			t.Fatalf("%s tenant CAS: %v", kind, err)
		}
		changed, _, err := migrateDocument(t.Context(), service, kind, rows[0], "hashicorp_vault", 0, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = isolated.SaveMigratedSecret(t.Context(), kind, rows[0], changed); err != nil {
			t.Fatal(err)
		}
		if err = isolated.SaveMigratedSecret(t.Context(), kind, rows[0], rows[0].Document); !errors.Is(err, db.ErrVersionConflict) {
			t.Fatalf("%s stale CAS: %v", kind, err)
		}
	}
}
