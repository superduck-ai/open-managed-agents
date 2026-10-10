package secretservice_test

import (
	"bytes"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
)

func TestNewRejectsInvalidProviderWithoutFallback(t *testing.T) {
	for _, mk := range []config.MasterKeyConfig{
		{Provider: "unknown_provider", Local: &config.LocalKeyConfig{Kek: base64.StdEncoding.EncodeToString(make([]byte, 32))}},
		{Provider: "aliyun_kms", Local: &config.LocalKeyConfig{Kek: "invalid-key"}, AliyunKMS: &config.AliyunKMSConfig{Endpoint: "kms.example", KeyID: "key"}},
		{Provider: "aliyun_kms"},
	} {
		if svc, err := secretservice.New(mk); err == nil || svc != nil {
			t.Fatal("invalid provider accepted")
		}
	}
}

func TestHashicorpVaultTLSConfiguration(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "inline-token" || r.URL.Path != "/v1/transit/encrypt/oma-dek" {
			t.Error("incorrect configured authentication or mount")
		}
		_, _ = io.WriteString(w, `{"data":{"ciphertext":"vault:v1:YQ=="}}`)
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
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	mk.HashicorpVault.TokenFile = ""
	mk.HashicorpVault.Token = "inline-token"
	mk.HashicorpVault.TransitMount = ""
	mk.HashicorpVault.CAFile = caFile
	svc, err = secretservice.New(mk)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Seal(t.Context(), binding, []byte("secret")); err != nil {
		t.Fatalf("configured token and CA did not reach provider: %v", err)
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
