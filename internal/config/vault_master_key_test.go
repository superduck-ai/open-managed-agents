package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestValidateMasterKey(t *testing.T) {
	aliyun := &AliyunKMSConfig{Endpoint: "kms.example", KeyID: "key"}
	vault := &HashicorpVaultConfig{Address: "https://vault.example", TransitMount: "transit", KeyName: "oma", TokenFile: "/run/secrets/token"}
	for _, tc := range []struct {
		name   string
		config MasterKeyConfig
		want   string
	}{
		{"unknown provider", MasterKeyConfig{Provider: "unknown"}, "unsupported vault.master_key.provider"},
		{"missing aliyun config", MasterKeyConfig{Provider: "aliyun_kms"}, "endpoint and key_id are required"},
		{"partial access key", MasterKeyConfig{Provider: "aliyun_kms", AliyunKMS: &AliyunKMSConfig{Endpoint: "kms.example", KeyID: "key", AccessKeyID: "partial"}}, "access_key_id and access_key_secret must be configured together"},
		{"local with aliyun", MasterKeyConfig{Provider: "aliyun_kms", AliyunKMS: aliyun, Local: &LocalKeyConfig{}}, "local.kek or kek_file is required"},
		{"missing vault config", MasterKeyConfig{Provider: "hashicorp_vault"}, "address and key_name are required"},
		{"local with vault", MasterKeyConfig{Provider: "hashicorp_vault", HashicorpVault: vault, Local: &LocalKeyConfig{}}, "local.kek or kek_file is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateMasterKey(tc.config); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		clear func(*HashicorpVaultConfig)
	}{
		{"address", func(v *HashicorpVaultConfig) { v.Address = " " }},
		{"key_name", func(v *HashicorpVaultConfig) { v.KeyName = " " }},
	} {
		t.Run("missing vault "+tc.name, func(t *testing.T) {
			v := *vault
			tc.clear(&v)
			if err := ValidateMasterKey(MasterKeyConfig{Provider: "hashicorp_vault", HashicorpVault: &v}); err == nil || !strings.Contains(err.Error(), "address and key_name are required") {
				t.Fatalf("missing %s error = %v", tc.name, err)
			}
		})
	}
}

func TestRemoteProviderYAMLRoundTrip(t *testing.T) {
	// Older remote configurations could contain empty flat local fields.
	for _, tc := range []struct{ provider, fields string }{
		{"aliyun_kms", "aliyun_kms: {endpoint: kms.example, key_id: 'acs:kms:cn-hangzhou:123:key/key-example'}"},
		{"hashicorp_vault", "hashicorp_vault: {address: 'https://vault.example', transit_mount: transit, key_name: oma, token_file: /run/secrets/token}"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			cfg, err := loadMasterKeyTestYAML(t, "{provider: "+tc.provider+", kek: '', version: 1, "+tc.fields+"}")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Vault.MasterKey.EffectiveProvider() != tc.provider || cfg.Vault.MasterKey.Local != nil {
				t.Fatal("legacy remote configuration was not normalized")
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "roundtrip.yaml")
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(configFileEnv, file)
			if _, err := Load(); err != nil {
				t.Fatalf("serialized provider configuration is not reloadable: %v", err)
			}
		})
	}
}

func TestHashicorpVaultYAML(t *testing.T) {
	const fields = "address: 'https://vault.example', transit_mount: transit, key_name: oma, token_file: tokens/vault-token, ca_file: certs/vault.pem"
	for _, forbidden := range []string{"skip_tls_verify", "auth_method"} {
		t.Run(forbidden, func(t *testing.T) {
			_, err := loadMasterKeyTestYAML(t, "{provider: hashicorp_vault, hashicorp_vault: {"+fields+", "+forbidden+": unsupported}}")
			if err == nil || !strings.Contains(err.Error(), "field "+forbidden+" not found") {
				t.Fatalf("error = %v, want unknown field %s", err, forbidden)
			}
		})
	}
	cfg, err := loadMasterKeyTestYAML(t, "{provider: hashicorp_vault, hashicorp_vault: {"+fields+"}}")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(os.Getenv(configFileEnv)), "tokens/vault-token")
	if cfg.Vault.MasterKey.HashicorpVault.TokenFile != want {
		t.Fatal("relative token path not resolved")
	}
	if cfg.Vault.MasterKey.HashicorpVault.CAFile != filepath.Join(filepath.Dir(os.Getenv(configFileEnv)), "certs/vault.pem") {
		t.Fatal("relative CA path not resolved")
	}
	for _, auth := range []string{"", ", token: inline-token, token_file: tokens/vault-token"} {
		_, err := loadMasterKeyTestYAML(t, "{provider: hashicorp_vault, hashicorp_vault: {address: 'https://vault.example', key_name: oma"+auth+"}}")
		if err == nil || !strings.Contains(err.Error(), "configure exactly one of token or token_file") {
			t.Fatalf("ambiguous or absent authentication accepted: %v", err)
		}
	}
	cfg, err = loadMasterKeyTestYAML(t, "{provider: hashicorp_vault, hashicorp_vault: {address: 'https://vault.example', key_name: oma, token: inline-token}}")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Vault.MasterKey.HashicorpVault.Token != "inline-token" {
		t.Fatal("inline token not preserved")
	}
}

func TestLocalMasterKeyYAML(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"mixed empty legacy", "{local: {kek: key}, kek: ''}", "local cannot be combined"},
		{"mixed legacy version", "{local: {kek: key}, version: 0}", "local cannot be combined"},
		{"legacy version with remote", "{provider: aliyun_kms, version: 2, aliyun_kms: {endpoint: kms.example, key_id: key}}", "local.kek or kek_file is required"},
		{"unknown local field", "{local: {typo: key}}", "field typo not found"},
		{"null local", "{local: null}", "must not be null"},
		{"null legacy", "{kek: null}", "must not be null"},
		{"missing local", "{provider: local}", "local is required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadMasterKeyTestYAML(t, tc.body)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
	const keys = "kek_file: keys/current, version: 2, decrypt_only: [{version: 1, kek_file: keys/previous}]"
	for _, tc := range []struct{ name, body string }{
		{"legacy without provider", "{" + keys + "}"},
		{"nested with empty provider", "{provider: '', local: {" + keys + "}}"},
		{"nested with explicit provider", "{provider: local, local: {" + keys + "}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadMasterKeyTestYAML(t, tc.body)
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Dir(os.Getenv(configFileEnv))
			want := &LocalKeyConfig{KekFile: filepath.Join(dir, "keys/current"), Version: 2, DecryptOnly: []DecryptOnlyKeyConfig{{Version: 1, KekFile: filepath.Join(dir, "keys/previous")}}}
			if cfg.Vault.MasterKey.EffectiveProvider() != "local" || !reflect.DeepEqual(cfg.Vault.MasterKey.Local, want) {
				t.Fatal("local keys or paths not preserved")
			}
			data, err := yaml.Marshal(cfg.Vault.MasterKey)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := yaml.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["local"]; !ok || len(fields) != 2 {
				t.Fatal("serialization must contain only provider and local")
			}
		})
	}
}

func loadMasterKeyTestYAML(t *testing.T, body string) (Config, error) {
	t.Helper()
	prepareLoadTest(t)
	prefix, _, _ := strings.Cut(requiredConfigTestYAML, "vault:")
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigTestContents(t, path, prefix+"vault:\n  master_key: "+body+"\n")
	t.Setenv(configFileEnv, path)
	return Load()
}

func TestMultipleProviderConfiguration(t *testing.T) {
	const fields = `local: {kek_file: keys/current}, aliyun_kms: {endpoint: kms.example, key_id: key}, hashicorp_vault: {address: 'https://vault.example', key_name: oma, token: offline-token}`
	for _, selection := range []string{"", "provider: local, ", "provider: aliyun_kms, ", "provider: hashicorp_vault, "} {
		cfg, err := loadMasterKeyTestYAML(t, "{"+selection+fields+"}")
		if err != nil {
			t.Fatal(err)
		}
		mk := cfg.Vault.MasterKey
		if mk.Local == nil || mk.AliyunKMS == nil || mk.HashicorpVault == nil {
			t.Fatal("historical provider dropped")
		}
		if !filepath.IsAbs(mk.Local.KekFile) {
			t.Fatal("historical key path not resolved")
		}
		encoded, err := yaml.Marshal(mk)
		if err != nil {
			t.Fatal(err)
		}
		var roundtrip MasterKeyConfig
		if err := yaml.Unmarshal(encoded, &roundtrip); err != nil {
			t.Fatal(err)
		}
		again, err := yaml.Marshal(roundtrip)
		if err != nil || string(encoded) != string(again) {
			t.Fatalf("mixed configuration roundtrip: %v", err)
		}
		if err := ValidateMasterKey(roundtrip); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := loadMasterKeyTestYAML(t, `{provider: aliyun_kms, kek_file: keys/legacy, aliyun_kms: {endpoint: kms.example, key_id: key}}`)
	if err != nil || legacy.Vault.MasterKey.Local == nil {
		t.Fatalf("legacy local reader: %v", err)
	}
	for _, fields := range []string{
		`local: {kek_file: keys/current}, aliyun_kms: {key_id: missing-endpoint}`,
		`local: {kek_file: keys/current}, hashicorp_vault: {address: 'https://vault.example', key_name: oma}`,
	} {
		if _, err := loadMasterKeyTestYAML(t, "{provider: local, "+fields+"}"); err == nil {
			t.Fatal("invalid read provider was ignored")
		}
	}
}
