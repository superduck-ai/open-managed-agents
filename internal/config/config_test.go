package config

import (
	"os"
	"testing"
)

func TestLoadDatabaseAutoMigrateDefaultDevelopment(t *testing.T) {
	prepareLoadTest(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.AppEnv != "development" {
		t.Fatalf("AppEnv = %q, want development", cfg.AppEnv)
	}
	if !cfg.DatabaseAutoMigrate {
		t.Fatal("DatabaseAutoMigrate = false, want true")
	}
}

func TestLoadDatabaseAutoMigrateProductionDefault(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("APP_ENV", "production")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DatabaseAutoMigrate {
		t.Fatal("DatabaseAutoMigrate = true, want false")
	}
}

func TestLoadDatabaseAutoMigrateProdDefault(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("APP_ENV", "prod")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DatabaseAutoMigrate {
		t.Fatal("DatabaseAutoMigrate = true, want false")
	}
}

func TestLoadDatabaseAutoMigrateOverride(t *testing.T) {
	t.Run("production enabled explicitly", func(t *testing.T) {
		prepareLoadTest(t)
		t.Setenv("APP_ENV", "production")
		t.Setenv("DB_AUTO_MIGRATE", "true")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.DatabaseAutoMigrate {
			t.Fatal("DatabaseAutoMigrate = false, want true")
		}
	})

	t.Run("development disabled explicitly", func(t *testing.T) {
		prepareLoadTest(t)
		t.Setenv("APP_ENV", "development")
		t.Setenv("DB_AUTO_MIGRATE", "false")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.DatabaseAutoMigrate {
			t.Fatal("DatabaseAutoMigrate = true, want false")
		}
	})
}

func TestLoadMCPDiscoveryEnabledInProductionWithoutIdentitySecret(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("MCP_DISCOVERY_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.MCPDiscoveryEnabled {
		t.Fatal("MCPDiscoveryEnabled = false, want true")
	}
}

func TestLoadCodeSessionOTLPFileLogDefaults(t *testing.T) {
	t.Run("development enabled", func(t *testing.T) {
		prepareLoadTest(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if !cfg.CodeSessionOTLPFileLogEnabled {
			t.Fatal("CodeSessionOTLPFileLogEnabled = false, want true")
		}
		if cfg.CodeSessionOTLPLogRoot != "logs" {
			t.Fatalf("CodeSessionOTLPLogRoot = %q, want logs", cfg.CodeSessionOTLPLogRoot)
		}
		if cfg.CodeSessionOTLPLogBodyPreviewBytes != 256*1024 {
			t.Fatalf("CodeSessionOTLPLogBodyPreviewBytes = %d, want %d", cfg.CodeSessionOTLPLogBodyPreviewBytes, 256*1024)
		}
	})

	t.Run("production disabled", func(t *testing.T) {
		prepareLoadTest(t)
		t.Setenv("APP_ENV", "production")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load config: %v", err)
		}
		if cfg.CodeSessionOTLPFileLogEnabled {
			t.Fatal("CodeSessionOTLPFileLogEnabled = true, want false")
		}
	})
}

func TestLoadCodeSessionOTLPFileLogOverrides(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("CODE_SESSION_OTLP_FILE_LOG_ENABLED", "true")
	t.Setenv("CODE_SESSION_OTLP_LOG_ROOT", "/tmp/custom-otlp")
	t.Setenv("CODE_SESSION_OTLP_LOG_BODY_PREVIEW_BYTES", "1024")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.CodeSessionOTLPFileLogEnabled {
		t.Fatal("CodeSessionOTLPFileLogEnabled = false, want true")
	}
	if cfg.CodeSessionOTLPLogRoot != "/tmp/custom-otlp" {
		t.Fatalf("CodeSessionOTLPLogRoot = %q, want /tmp/custom-otlp", cfg.CodeSessionOTLPLogRoot)
	}
	if cfg.CodeSessionOTLPLogBodyPreviewBytes != 1024 {
		t.Fatalf("CodeSessionOTLPLogBodyPreviewBytes = %d, want 1024", cfg.CodeSessionOTLPLogBodyPreviewBytes)
	}
}

func prepareLoadTest(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
	for _, key := range []string{
		"APP_ENV",
		"DB_AUTO_MIGRATE",
		"CODE_SESSION_OTLP_FILE_LOG_ENABLED",
		"CODE_SESSION_OTLP_LOG_ROOT",
		"CODE_SESSION_OTLP_LOG_BODY_PREVIEW_BYTES",
		"MCP_DISCOVERY_ENABLED",
		"DATABASE_URL",
		"S3_ENDPOINT",
		"S3_BUCKET",
		"S3_ACCESS_KEY_ID",
		"S3_SECRET_ACCESS_KEY",
	} {
		unsetEnv(t, key)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	oldValue, hadValue := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv(key, oldValue)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
