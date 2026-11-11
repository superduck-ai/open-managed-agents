//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/pressly/goose/v3"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/platformoauth"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func migrationDatabase(t *testing.T) (*db.DB, *secrets.Service, string) {
	t.Helper()
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL，跳过不代表通过")
	}
	admin, err := db.OpenExisting(t.Context(), config.Config{Database: config.DatabaseConfig{URL: address}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	schema := "oauth_delta_" + strings.ReplaceAll(uuid.NewV4().String(), "-", "")
	if _, err := admin.SQLDB().ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	scopedURL := parsed.String()
	database, err := db.OpenExisting(t.Context(), config.Config{Database: config.DatabaseConfig{URL: scopedURL}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		database.Close()
		_, _ = admin.SQLDB().ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	provider, err := goose.NewProvider(goose.DialectPostgres, database.SQLDB(), os.DirFS("../../internal/db/migrations"), goose.WithDisableGlobalRegistry(true), goose.WithLogger(goose.NopLogger()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(t.Context(), 118); err != nil {
		t.Fatal(err)
	}
	key, err := localkeys.New(localkeys.KeyMaterial{KEK: bytes.Repeat([]byte{9}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return database, secrets.NewService(key), scopedURL
}

func seedLegacyProvider(t *testing.T, database *db.DB, service *secrets.Service, externalID string) string {
	t.Helper()
	envelope, err := platformoauth.SealClientField(t.Context(), service, platformoauth.ProviderSecretBinding(externalID), "synthetic-provider-secret")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var providerUUID string
	if err := database.SQLDB().QueryRowContext(t.Context(), `INSERT INTO platform_oauth_providers
        (external_id,name,authorize_endpoint,token_endpoint,client_id,client_secret_envelope,created_by_platform_admin_uuid)
        VALUES ($1,'Feishu','https://auth.test/authorize','https://auth.test/token','synthetic-provider-client',$2,gen_random_uuid()) RETURNING uuid`, externalID, string(encoded)).Scan(&providerUUID); err != nil {
		t.Fatal(err)
	}
	return providerUUID
}

func seedLegacyFlows(t *testing.T, database *db.DB) {
	t.Helper()
	for _, status := range []string{"pending", "completed", "failed", "expired"} {
		if _, err := database.SQLDB().ExecContext(t.Context(), `INSERT INTO env_oauth_flows (
            external_id,organization_uuid,workspace_uuid,vault_uuid,vault_external_id,provider_key,secret_name,
            redirect_url,authorize_endpoint,token_endpoint,client_id,token_endpoint_auth_method,token_endpoint_encoding,
            networking_type,status,expires_at) VALUES ($1,gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),
            'vlt_fixture','custom:poap_legacy','TOKEN','https://app.test/callback','https://auth.test/authorize',
            'https://auth.test/token','synthetic-flow-client','client_secret_post','form','unrestricted',$1,now())`, status); err != nil {
			t.Fatal(err)
		}
	}
}

func migrateFixture(t *testing.T, database *db.DB, service *secrets.Service, legacy legacyClient, apply bool) string {
	t.Helper()
	var output bytes.Buffer
	execute := func() error {
		return migrate(t.Context(), database, service, legacy, legacyClient{}, apply, &output, slog.New(slog.DiscardHandler))
	}
	var err error
	if apply {
		err = database.WithOAuthMaintenanceLock(t.Context(), execute)
	} else {
		err = execute()
	}
	if err != nil {
		t.Fatalf("迁移失败：%v，输出：%s", err, output.String())
	}
	if strings.Contains(output.String(), "synthetic-") {
		t.Fatal("迁移输出泄漏敏感值")
	}
	return output.String()
}

func TestOAuthProviderMigrationBlocksIncompletePostgres(t *testing.T) {
	database, service, _ := migrationDatabase(t)
	seedLegacyProvider(t, database, service, "poap_legacy")
	seedLegacyFlows(t, database)
	migrateFixture(t, database, service, legacyClient{}, false)
	if version, err := database.OAuthMigrationVersion(t.Context()); err != nil || version != 118 {
		t.Fatal("预检修改了数据库")
	}
	if err := database.WithOAuthMaintenanceLock(t.Context(), func() error { return database.MigrateOAuthSchemaTo(t.Context(), 119) }); err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateOAuthSchemaTo(t.Context(), 120); err == nil {
		t.Fatal("未回填数据越过收口迁移")
	}
	var value string
	if err := database.SQLDB().QueryRowContext(t.Context(), "SELECT client_id FROM platform_oauth_providers WHERE builtin_key IS NULL").Scan(&value); err != nil || value != "synthetic-provider-client" {
		t.Fatal("失败迁移丢失旧数据")
	}
	rows, err := database.ListOAuthClientMigrationPage(t.Context(), "platform_oauth_providers", "", true, true)
	if err != nil || len(rows) != 1 {
		t.Fatal(err)
	}
	envelope, err := platformoauth.SealClientField(t.Context(), service, platformoauth.ProviderClientIDBinding(rows[0].ExternalID), rows[0].LegacyClientID)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.BackfillOAuthClientID(t.Context(), "platform_oauth_providers", rows[0], *envelope); err != nil {
		t.Fatal(err)
	}
	if err := database.BackfillOAuthClientID(t.Context(), "platform_oauth_providers", rows[0], *envelope); !errors.Is(err, db.ErrVersionConflict) {
		t.Fatal("过期回填未拒绝")
	}
	migrateFixture(t, database, service, legacyClient{ClientID: "synthetic-feishu-id", ClientSecret: "synthetic-feishu-secret"}, true)
	for _, kind := range []string{"platform_oauth_providers", "env_oauth_flows"} {
		rows, err := database.ListOAuthClientMigrationPage(t.Context(), kind, "", false, true)
		if err != nil {
			t.Fatal(err)
		}
		expected := 1
		if kind == "env_oauth_flows" {
			expected = 4
		}
		if len(rows) != expected {
			t.Fatal("历史状态未完整回填")
		}
		for _, row := range rows {
			if row.ClientIDEnvelope == nil {
				t.Fatal("缺少信封")
			}
		}
		var count int
		if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='client_id'`, kind).Scan(&count); err != nil || count != 0 {
			t.Fatal("明文列未删除")
		}
	}
	var exported string
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT jsonb_agg(to_jsonb(p))::text FROM platform_oauth_providers p`).Scan(&exported); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported, "synthetic-") {
		t.Fatal("数据库导出含客户端明文")
	}
	var foreignKeys int
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace=current_schema()::regnamespace`).Scan(&foreignKeys); err != nil || foreignKeys != 0 {
		t.Fatal("出现外键")
	}
}

func TestOAuthProviderMigrationReservedIDPostgres(t *testing.T) {
	database, service, _ := migrationDatabase(t)
	seedLegacyProvider(t, database, service, "poap_FeishuBuiltinOAuthClient")
	if err := database.MigrateOAuthSchemaTo(t.Context(), 119); err == nil {
		t.Fatal("保留 external_id 冲突被覆盖")
	}
	if version, _ := database.OAuthMigrationVersion(t.Context()); version != 118 {
		t.Fatal("冲突迁移未回滚")
	}
}

func TestOAuthProviderMigrationEmptyAndReentrantPostgres(t *testing.T) {
	database, service, address := migrationDatabase(t)
	migrateFixture(t, database, service, legacyClient{}, true)
	builtin, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil || builtin.ClientIDEnvelope != nil || !builtin.Protected {
		t.Fatal("空库 seed 错误")
	}
	legacy := legacyClient{ClientID: "synthetic-feishu-id", ClientSecret: "synthetic-feishu-secret"}
	migrateFixture(t, database, service, legacy, true)
	configured, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil {
		t.Fatal(err)
	}
	migrateFixture(t, database, service, legacyClient{ClientID: "different", ClientSecret: "different"}, true)
	current, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "feishu")
	if err != nil || current.Version != configured.Version || current.UUID != builtin.UUID || !bytes.Equal(current.ClientIDEnvelope.Ciphertext, configured.ClientIDEnvelope.Ciphertext) {
		t.Fatal("重入覆盖已有配置")
	}
	reopened, err := db.OpenExisting(t.Context(), config.Config{Database: config.DatabaseConfig{URL: address}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	snapshot, err := platformoauth.NewResolver(reopened, service, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:feishu")
	if err != nil || snapshot.ClientID != legacy.ClientID || snapshot.ClientSecret != legacy.ClientSecret {
		t.Fatal("数据库重连后不可解密")
	}
	for _, raw := range []string{`{}`, `"text"`, `{"ciphertext":null,"nonce":"a","wrapped_dek":"a","format_version":1,"key_provider":"local","key_version":1}`} {
		if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE platform_oauth_providers SET client_id_envelope=$1 WHERE builtin_key='feishu'`, raw); err == nil {
			t.Fatal("无效信封被接受")
		}
	}
	if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE platform_oauth_providers SET client_secret_envelope=NULL WHERE builtin_key='feishu'`); err == nil {
		t.Fatal("半配置被接受")
	}
	if err := database.MigrateOAuthSchemaTo(t.Context(), 119); err == nil {
		t.Fatal("允许降级")
	}
}

func TestOAuthProviderMigrationBadEnvelopeResumesPostgres(t *testing.T) {
	database, service, _ := migrationDatabase(t)
	providerUUID := seedLegacyProvider(t, database, service, "poap_legacy")
	if err := database.MigrateOAuthSchemaTo(t.Context(), 119); err != nil {
		t.Fatal(err)
	}
	var original string
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT client_secret_envelope FROM platform_oauth_providers WHERE uuid=$1`, providerUUID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE platform_oauth_providers SET client_secret_envelope='{}' WHERE uuid=$1`, providerUUID); err != nil {
		t.Fatal(err)
	}
	if err := migrate(t.Context(), database, service, legacyClient{}, legacyClient{}, true, io.Discard, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("坏信封未失败关闭")
	}
	if version, _ := database.OAuthMigrationVersion(t.Context()); version != 119 {
		t.Fatal("失败后删除明文列")
	}
	if _, err := database.SQLDB().ExecContext(t.Context(), `UPDATE platform_oauth_providers SET client_secret_envelope=$1 WHERE uuid=$2`, original, providerUUID); err != nil {
		t.Fatal(err)
	}
	migrateFixture(t, database, service, legacyClient{}, true)
	var pending sql.NullString
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT client_id_envelope FROM platform_oauth_providers WHERE uuid=$1`, providerUUID).Scan(&pending); err != nil || !pending.Valid {
		t.Fatal("续跑未完成")
	}
}

func TestOAuthProviderFridayReservedIDPostgres(t *testing.T) {
	database, service, _ := migrationDatabase(t)
	seedLegacyProvider(t, database, service, "poap_FridayBuiltinOAuthClient")
	if err := migrate(t.Context(), database, service, legacyClient{}, legacyClient{}, true, io.Discard, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("Friday 保留 ID 冲突被覆盖")
	}
	if version, err := database.OAuthMigrationVersion(t.Context()); err != nil || version != 120 {
		t.Fatal("Friday 失败迁移未保留已加密旧记录")
	}
}

func TestOAuthProviderMigrationAppliesOnHeadSchemaPostgres(t *testing.T) {
	database, service, _ := migrationDatabase(t)
	migrateFixture(t, database, service, legacyClient{}, true)
	if _, err := database.SQLDB().ExecContext(t.Context(), `ALTER TABLE env_oauth_flows
        DROP CONSTRAINT env_oauth_flows_status_check,
        ADD CONSTRAINT env_oauth_flows_status_check CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'expired'))`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQLDB().ExecContext(t.Context(), `INSERT INTO goose_db_version (version_id, is_applied, tstamp) VALUES (123, true, now())`); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	execute := func() error {
		return migrate(t.Context(), database, service, legacyClient{}, legacyClient{}, true, &output, slog.New(slog.DiscardHandler))
	}
	if err := database.WithOAuthMaintenanceLock(t.Context(), execute); err != nil {
		t.Fatalf("已到 00123 的库重跑失败：%v，输出：%s", err, output.String())
	}
	if !strings.Contains(output.String(), "schema_version=123") {
		t.Fatalf("未报告当前 schema 版本：%s", output.String())
	}
}

func TestOAuthProviderFridayImportPostgres(t *testing.T) {
	database, service, _ := migrationDatabase(t)
	migrateFixture(t, database, service, legacyClient{}, true)
	friday, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "friday")
	if err != nil || friday.ClientIDEnvelope != nil || friday.ClientSecretEnvelope != nil {
		t.Fatal("Friday 不允许未配置", err)
	}
	var output bytes.Buffer
	client := legacyClient{ClientID: "synthetic-friday-id", ClientSecret: "synthetic-friday-secret"}
	execute := func() error {
		return migrate(t.Context(), database, service, legacyClient{}, client, true, &output, slog.New(slog.DiscardHandler))
	}
	if err := database.WithOAuthMaintenanceLock(t.Context(), execute); err != nil {
		t.Fatal(err)
	}
	configured, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "friday")
	if err != nil || configured.UUID != friday.UUID || configured.Version != friday.Version+1 {
		t.Fatal("Friday 导入身份/版本错误", err)
	}
	if err := database.WithOAuthMaintenanceLock(t.Context(), execute); err != nil {
		t.Fatal(err)
	}
	current, err := database.GetPlatformOAuthProviderByBuiltinKey(t.Context(), "friday")
	if err != nil || current.Version != configured.Version {
		t.Fatal("Friday 重入覆盖配置")
	}
	snapshot, err := platformoauth.NewResolver(database, service, nil).ResolveEnvOAuthProvider(t.Context(), "builtin:friday")
	if err != nil || snapshot.ClientID != client.ClientID || snapshot.ClientSecret != client.ClientSecret || snapshot.Scope != "read" {
		t.Fatal("Friday 密文不可解析", err)
	}
	var exported string
	if err := database.SQLDB().QueryRowContext(t.Context(), `SELECT to_jsonb(p)::text FROM platform_oauth_providers p WHERE builtin_key='friday'`).Scan(&exported); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(exported, "synthetic-") || strings.Contains(output.String(), "synthetic-") {
		t.Fatal("Friday 导入泄漏明文")
	}
	for _, query := range []string{
		`UPDATE platform_oauth_providers SET client_id_envelope=NULL WHERE builtin_key='friday'`,
		`UPDATE platform_oauth_providers SET builtin_key='feishu' WHERE builtin_key='friday'`,
		`UPDATE platform_oauth_providers SET protected=false WHERE builtin_key='friday'`,
	} {
		if _, err := database.SQLDB().ExecContext(t.Context(), query); err == nil {
			t.Fatal("Friday 约束被绕过")
		}
	}
	if version, err := database.OAuthMigrationVersion(t.Context()); err != nil || version != 122 {
		t.Fatal("未升级到 00122")
	}
	output.Reset()
	if err := migrate(t.Context(), database, service, legacyClient{}, client, false, &output, slog.New(slog.DiscardHandler)); err != nil || !strings.Contains(output.String(), "success=0 pending=0 skipped=2 failed=0") {
		t.Fatal("Friday 只读预检未统计两个内置行", err)
	}

}
