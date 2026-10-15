package tests

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
	"github.com/superduck-ai/open-managed-agents/internal/secretservice"
	"go.yaml.in/yaml/v3"
)

func TestConfiguredKMSService(t *testing.T) {
	fake := newFakeKMSServer(t)
	data, err := os.ReadFile("../config/config.example.yaml")
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

type fakeKMSServer struct {
	URL      string
	CA       string
	Failure  atomic.Value
	Requests atomic.Int64
	local    *localkeys.Provider
}

func newFakeKMSServer(t *testing.T) *fakeKMSServer {
	t.Helper()
	local, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: bytes.Repeat([]byte{91}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeKMSServer{local: local}
	fake.Failure.Store("")
	server := httptest.NewTLSServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.CA = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	fake.URL = server.URL
	return fake
}

func (s *fakeKMSServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.Requests.Add(1)
	w.Header().Set("Content-Type", "application/json")
	if code := s.Failure.Load().(string); code != "" {
		s.fail(w, code)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.fail(w, "InvalidParameter")
		return
	}
	var encryptionContext struct {
		Purpose string `json:"purpose"`
		KeyID   string `json:"key_id"`
	}
	if json.Unmarshal([]byte(r.Form.Get("EncryptionContext")), &encryptionContext) != nil || encryptionContext.Purpose != "oma-dek-v1" || encryptionContext.KeyID != "test-key" {
		s.fail(w, "InvalidParameter")
		return
	}
	response := map[string]string{"KeyId": "test-key", "KeyVersionId": "opaque-cloud-version", "RequestId": "offline-test"}
	action := r.Form.Get("Action")
	if action == "" {
		action = r.Header.Get("x-acs-action")
	}
	switch action {
	case "Encrypt":
		dek, err := base64.StdEncoding.DecodeString(r.Form.Get("Plaintext"))
		defer clear(dek)
		if err != nil || len(dek) != 32 {
			s.fail(w, "InvalidParameter")
			return
		}
		wrapped, err := s.local.WrapDEK(r.Context(), dek)
		if err != nil {
			s.fail(w, "InternalFailure")
			return
		}
		response["CiphertextBlob"] = base64.StdEncoding.EncodeToString(wrapped.Ciphertext)
	case "Decrypt":
		encrypted, err := base64.StdEncoding.DecodeString(r.Form.Get("CiphertextBlob"))
		if err != nil {
			s.fail(w, "InvalidCiphertext")
			return
		}
		dek, err := s.local.UnwrapDEK(r.Context(), secrets.WrappedKey{Ciphertext: encrypted, KeyVersion: 1})
		defer clear(dek)
		if err != nil {
			s.fail(w, "InvalidCiphertext")
			return
		}
		response["Plaintext"] = base64.StdEncoding.EncodeToString(dek)
	default:
		s.fail(w, "InvalidParameter")
		return
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (*fakeKMSServer) fail(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"Code": code, "Message": "untrusted-response-containing-secret", "RequestId": "offline-test"})
}
