package config

import (
	"os"
	"path/filepath"
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

func TestLoadCodeSessionUpstreamProxySSRFProtectionOverride(t *testing.T) {
	prepareLoadTest(t)

	// 默认必须保持保护开启，避免新部署在未配置时意外获得私网访问能力。
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.CodeSessionUpstreamProxyDisableSSRFProtection {
		t.Fatal("CodeSessionUpstreamProxyDisableSSRFProtection = true, want false by default")
	}

	// 只有运维显式设置危险开关时才允许本地 fake-IP/TUN 排障模式。
	t.Setenv("CODE_SESSION_UPSTREAM_PROXY_DISABLE_SSRF_PROTECTION", "true")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("load config with SSRF override: %v", err)
	}
	if !cfg.CodeSessionUpstreamProxyDisableSSRFProtection {
		t.Fatal("CodeSessionUpstreamProxyDisableSSRFProtection = false, want true")
	}
}

func TestLoadCodeSessionUpstreamProxyMITMRejectsInvalidCAKey(t *testing.T) {
	t.Run("MITM enabled without private key", func(t *testing.T) {
		prepareLoadTest(t)
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED", "true")

		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want missing stable CA private key error")
		}
	})

	t.Run("private key does not exist", func(t *testing.T) {
		prepareLoadTest(t)
		directory := t.TempDir()
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED", "true")
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE", filepath.Join(directory, "missing-key.pem"))

		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want missing stable CA private key file error")
		}
	})

	t.Run("private key is not a regular file", func(t *testing.T) {
		prepareLoadTest(t)
		directory := t.TempDir()
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED", "true")
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE", directory)

		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want non-regular stable CA private key error")
		}
	})
}

func TestLoadCodeSessionUpstreamProxyMITMConfiguration(t *testing.T) {
	t.Run("MITM disabled ignores dormant private key path", func(t *testing.T) {
		prepareLoadTest(t)
		missingKeyFile := filepath.Join(t.TempDir(), "missing-key.pem")
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE", missingKeyFile)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.CodeSessionUpstreamProxyMITMEnabled || cfg.CodeSessionUpstreamProxyCAKeyFile != missingKeyFile {
			t.Fatalf("unexpected dormant MITM config: enabled=%t key=%q", cfg.CodeSessionUpstreamProxyMITMEnabled, cfg.CodeSessionUpstreamProxyCAKeyFile)
		}
	})

	t.Run("stable private key exists", func(t *testing.T) {
		prepareLoadTest(t)
		keyFile := writeConfigTestFile(t, filepath.Join(t.TempDir(), "ca-key.pem"))
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED", "true")
		t.Setenv("CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE", keyFile)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if !cfg.CodeSessionUpstreamProxyMITMEnabled || cfg.CodeSessionUpstreamProxyCAKeyFile != keyFile {
			t.Fatalf("unexpected MITM config: enabled=%t key=%q", cfg.CodeSessionUpstreamProxyMITMEnabled, cfg.CodeSessionUpstreamProxyCAKeyFile)
		}
	})
}

func writeConfigTestFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("write config fixture %q: %v", path, err)
	}
	return path
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
		"CODE_SESSION_UPSTREAM_PROXY_MITM_ENABLED",
		"CODE_SESSION_UPSTREAM_PROXY_CA_KEY_FILE",
		"CODE_SESSION_UPSTREAM_PROXY_DISABLE_SSRF_PROTECTION",
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
