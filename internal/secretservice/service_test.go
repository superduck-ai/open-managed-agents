package secretservice_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
	"github.com/superduck-ai/open-managed-agents/internal/testutil/kmsfake"
	"go.yaml.in/yaml/v3"
)

func TestNewRejectsInvalidProviderWithoutFallback(t *testing.T) {
	for _, mk := range []config.MasterKeyConfig{
		{Provider: "unknown_provider", Local: &config.LocalKeyConfig{Kek: base64.StdEncoding.EncodeToString(make([]byte, 32))}},
		{Provider: "aliyun_kms", Local: &config.LocalKeyConfig{Kek: base64.StdEncoding.EncodeToString(make([]byte, 32))}, AliyunKMS: &config.AliyunKMSConfig{Endpoint: "kms.example", KeyID: "key"}},
		{Provider: "aliyun_kms"},
	} {
		if svc, err := secretservice.New(mk); err == nil || svc != nil {
			t.Fatal("invalid provider accepted")
		}
	}
}

func TestHashicorpVaultDefersRemoteFailures(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("untrusted server received authenticated request")
		w.WriteHeader(500)
	}))
	defer server.Close()
	token := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(token, []byte("test-token"), 0600); err != nil {
		t.Fatal(err)
	}
	mk := config.MasterKeyConfig{Provider: "hashicorp_vault", HashicorpVault: &config.HashicorpVaultConfig{
		Address: server.URL, TransitMount: "transit", KeyName: "oma-dek", TokenFile: token,
	}}
	svc, err := secretservice.New(mk)
	if err != nil {
		t.Fatal(err)
	}
	binding := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "vault", CredentialExternalID: "credential"}
	if _, err := svc.Seal(t.Context(), binding, []byte("secret")); err == nil {
		t.Fatal("untrusted HTTPS accepted")
	}
}

func TestConfiguredKMSService(t *testing.T) {
	fake := kmsfake.New(t)
	data, err := os.ReadFile("../../config/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["vault"] = map[string]any{"master_key": map[string]any{"provider": "aliyun_kms", "aliyun_kms": map[string]any{"endpoint": fake.URL, "key_id": "acs:kms:cn-hangzhou:123:key/test-key", "access_key_id": "offline-id", "access_key_secret": "offline-secret"}}}
	data, err = yaml.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONFIG_FILE", file)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := secretservice.New(cfg.Vault.MasterKey, secretservice.WithKMSCA(fake.CA))
	if err != nil {
		t.Fatal(err)
	}
	if fake.Requests.Load() != 0 {
		t.Fatal("assembly contacted KMS")
	}
	binding := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "workspace", VaultExternalID: "vault", CredentialExternalID: "credential"}
	envelope, err := svc.Seal(t.Context(), binding, []byte("payload-stays-local"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := svc.Open(t.Context(), binding, envelope)
	if err != nil || string(opened) != "payload-stays-local" {
		t.Fatalf("configured KMS roundtrip: %v", err)
	}
	if envelope.KeyProvider != "aliyun_kms" || envelope.KeyVersion != 1 {
		t.Fatal("wrong persisted provider metadata")
	}
}

func TestLocalKeyRotation(t *testing.T) {
	for _, provider := range []string{"", "local"} {
		mk := config.MasterKeyConfig{Provider: provider, Local: &config.LocalKeyConfig{Kek: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))}}
		first, err := secretservice.New(mk)
		if err != nil {
			t.Fatal(err)
		}
		b := secrets.Binding{OrganizationUUID: "o", WorkspaceUUID: "w", VaultExternalID: "v", CredentialExternalID: "c"}
		env, err := first.Seal(t.Context(), b, []byte("legacy"))
		if err != nil {
			t.Fatal(err)
		}
		mk.Local.DecryptOnly = []config.DecryptOnlyKeyConfig{{Version: 1, Kek: mk.Local.Kek}}
		mk.Local.Version = 2
		mk.Local.Kek = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
		rotated, err := secretservice.New(mk)
		if err != nil {
			t.Fatal(err)
		}
		plain, err := rotated.Open(t.Context(), b, env)
		if err != nil || string(plain) != "legacy" {
			t.Fatalf("legacy rotation: %v", err)
		}
	}
}
