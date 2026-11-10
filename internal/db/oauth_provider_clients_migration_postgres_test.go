//go:build integration

package db

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestPlatformOAuthProviderClientConstraintMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL；跳过不能视为通过")
	}
	ctx := t.Context()
	database := newIsolatedSessionResourceMigrationTestDatabase(t, ctx, address)
	provider := newMigrationTestProvider(t, database)
	if _, err := provider.UpTo(ctx, 120); err != nil {
		t.Fatal(err)
	}
	const envelope = `{"ciphertext":"Yw==","nonce":"bg==","wrapped_dek":"aw==","format_version":1,"key_provider":"local","key_version":1}`
	const insert = `INSERT INTO platform_oauth_providers (
		external_id, name, authorize_endpoint, token_endpoint, created_by_platform_admin_uuid,
		client_id_envelope, client_secret_envelope
	) VALUES ($1, $1, 'https://provider.test/authorize', 'https://provider.test/token',
		'11111111-1111-4111-8111-111111111111', $2, $3)`
	for _, fixture := range []struct {
		id       string
		envelope *string
	}{
		{"poap_legacy_invalid", nil},
		{"poap_legacy_valid", new(envelope)},
	} {
		if _, err := database.ExecContext(ctx, insert, fixture.id, fixture.envelope, fixture.envelope); err != nil {
			t.Fatal(err)
		}
	}
	_, err := provider.UpTo(ctx, 121)
	assertOAuthClientConstraintViolation(t, err, "platform_oauth_providers_clients_check")
	version, err := provider.GetDBVersion(ctx)
	if err != nil || version != 120 {
		t.Fatalf("失败迁移未保留版本 120：版本=%d，错误=%v", version, err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE platform_oauth_providers SET client_id_envelope=$1, client_secret_envelope=$1 WHERE external_id='poap_legacy_invalid'`, envelope); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 121); err != nil {
		t.Fatal(err)
	}
	var preserved int
	if err := database.QueryRowContext(ctx, `SELECT count(*) FROM platform_oauth_providers WHERE external_id IN ('poap_legacy_valid','poap_legacy_invalid') AND client_id_envelope=$1 AND client_secret_envelope=$1 AND version=1`, envelope).Scan(&preserved); err != nil || preserved != 2 {
		t.Fatalf("迁移未保留既有凭据：行数=%d，错误=%v", preserved, err)
	}
	for _, state := range []struct {
		name       string
		clientID   *string
		secret     *string
		configured bool
	}{
		{"双空", nil, nil, false},
		{"缺少ID", nil, new(envelope), false},
		{"缺少secret", new(envelope), nil, false},
		{"双有效", new(envelope), new(envelope), true},
	} {
		for _, key := range []string{"custom", "feishu", "friday"} {
			for _, operation := range []string{"插入", "更新"} {
				t.Run(key+"/"+state.name+"/"+operation, func(t *testing.T) {
					tx, err := database.BeginTx(t.Context(), nil)
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = tx.Rollback() }()
					if key == "custom" && operation == "插入" {
						_, err = tx.ExecContext(t.Context(), insert, "poap_constraint_probe", state.clientID, state.secret)
					} else if key != "custom" && operation == "插入" {
						_, err = tx.ExecContext(t.Context(), `WITH removed AS (
							DELETE FROM platform_oauth_providers WHERE builtin_key=$1 RETURNING *
						) INSERT INTO platform_oauth_providers (
							external_id, builtin_key, protected, name, authorize_endpoint, token_endpoint,
							client_id_envelope, client_secret_envelope
						) SELECT external_id, builtin_key, protected, name, authorize_endpoint, token_endpoint, $2, $3 FROM removed`, key, state.clientID, state.secret)
					} else {
						_, err = tx.ExecContext(t.Context(), `UPDATE platform_oauth_providers SET client_id_envelope=$2, client_secret_envelope=$3 WHERE (builtin_key=$1 OR ($1='custom' AND external_id='poap_legacy_valid'))`, key, state.clientID, state.secret)
					}
					allowed := state.configured || (key != "custom" && state.clientID == nil && state.secret == nil)
					if !allowed {
						assertOAuthClientConstraintViolation(t, err, "platform_oauth_providers_clients_check")
					} else if err != nil {
						t.Fatalf("合法凭据状态被拒绝：%v", err)
					}
				})
			}
		}
	}
	if _, err := database.ExecContext(ctx, `UPDATE platform_oauth_providers SET client_id_envelope=$1, client_secret_envelope=$1 WHERE builtin_key='feishu'`, envelope); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []struct {
		name       string
		set        string
		constraint string
	}{
		{"空对象ID", `client_id_envelope='{}'`, "platform_oauth_providers_client_id_envelope_check"},
		{"JSON空secret", `client_secret_envelope='null'`, "platform_oauth_providers_client_secret_envelope_check"},
		{"内置保护", `protected=false`, "platform_oauth_providers_builtin_check"},
		{"内置身份", `builtin_key='unknown'`, "platform_oauth_providers_builtin_check"},
		{"保留ID", `external_id='poap_reserved_probe'`, "platform_oauth_providers_builtin_check"},
		{"版本", `version=0`, "platform_oauth_providers_version_check"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			_, err := database.ExecContext(t.Context(), "UPDATE platform_oauth_providers SET "+invalid.set+" WHERE builtin_key='feishu'")
			assertOAuthClientConstraintViolation(t, err, invalid.constraint)
		})
	}
}

func assertOAuthClientConstraintViolation(t *testing.T, err error, constraint string) {
	t.Helper()
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "23514" || pgErr.ConstraintName != constraint {
		t.Fatalf("期望 CHECK %s 拒绝，实际错误=%v", constraint, err)
	}
}
