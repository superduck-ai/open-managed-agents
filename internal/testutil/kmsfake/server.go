// Package kmsfake provides an offline HTTPS KMS protocol fixture for tests.
package kmsfake

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

// Server speaks the official Encrypt/Decrypt protocol with a local test KEK.
type Server struct {
	URL      string
	CA       string       // PEM trust bundle, scoped to the client under test
	Failure  atomic.Value // string: KMS error code or empty
	Requests atomic.Int64
	local    *localkeys.Provider
}

func New(t *testing.T) *Server {
	t.Helper()
	local, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: bytes.Repeat([]byte{91}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &Server{local: local}
	fake.Failure.Store("")
	server := httptest.NewTLSServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(server.Close)
	fake.CA = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	fake.URL = server.URL
	return fake
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
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

func (*Server) fail(w http.ResponseWriter, code string) {
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"Code": code, "Message": "untrusted-response-containing-secret", "RequestId": "offline-test"})
}
