//go:build integration

package db

import (
	"os"
	"testing"
)

func TestOAuthProviderAvailabilityMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL；跳过不能视为通过")
	}
	for _, previous := range []string{"", "CUSTOM_ACCESS_TOKEN"} {
		t.Run(previous, func(t *testing.T) {
			database := newIsolatedSessionResourceMigrationTestDatabase(t, t.Context(), address)
			provider := newMigrationTestProvider(t, database)
			if _, err := provider.UpTo(t.Context(), 127); err != nil {
				t.Fatal(err)
			}
			if _, err := database.ExecContext(t.Context(), `UPDATE platform_oauth_providers SET default_secret_name=$1 WHERE builtin_key='feishu'`, previous); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.UpTo(t.Context(), 128); err != nil {
				t.Fatal(err)
			}
			var enabled bool
			var name string
			if err := database.QueryRowContext(t.Context(), `SELECT enabled, default_secret_name FROM platform_oauth_providers WHERE builtin_key='feishu'`).Scan(&enabled, &name); err != nil {
				t.Fatal(err)
			}
			if !enabled || name != previous {
				t.Fatalf("启用状态或自定义变量名错误：启用=%v，变量名=%q", enabled, name)
			}
			if _, err := database.ExecContext(t.Context(), `UPDATE platform_oauth_providers SET enabled=NULL WHERE builtin_key='feishu'`); err == nil {
				t.Fatal("启用状态接受 NULL")
			}
		})
	}
}

func TestOAuthProviderArchiveMigrationPostgres(t *testing.T) {
	address := os.Getenv("TEST_MIGRATION_DATABASE_URL")
	if address == "" {
		t.Skip("需要隔离 PostgreSQL；跳过不能视为通过")
	}
	database := newIsolatedSessionResourceMigrationTestDatabase(t, t.Context(), address)
	provider := newMigrationTestProvider(t, database)
	if _, err := provider.UpTo(t.Context(), 129); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `UPDATE platform_oauth_providers SET archived=NULL WHERE builtin_key='feishu'`); err == nil {
		t.Fatal("归档状态接受 NULL")
	}
	var archived bool
	if err := database.QueryRowContext(t.Context(), `SELECT archived FROM platform_oauth_providers WHERE builtin_key='feishu'`).Scan(&archived); err != nil || archived {
		t.Fatal("已有客户端未默认保持未归档", err)
	}
}
