package hashicorpvault

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func TestInvalidConfiguration(t *testing.T) {
	base := Config{Address: "https://vault.example.com", TransitMount: "transit", KeyName: "oma-dek", TokenFile: "/external/token"}
	for _, address := range []string{"", "http://vault.local", "http://vault.example.com", "http://10.0.0.1", "http://vault.evil.example", "ftp://localhost", "http://localhost/v1", "http://user:pass@vault", "https://user:pass@vault.local", "https://vault.local/v1", "https://vault.local?", "https://vault.local#x", "https:///", "https://vault.local/%2e%2e"} {
		cfg := base
		cfg.Address = address
		if p, err := New(cfg); err == nil || p != nil {
			t.Fatalf("accepted invalid address %q", address)
		}
	}
	for _, segment := range []string{"", ".", "..", "../keys", "name/other", "name?query", "%2f", "name#fragment", "trailing.", "non ascii界"} {
		cfg := base
		cfg.KeyName = segment
		if p, err := New(cfg); err == nil || p != nil {
			t.Fatalf("accepted key %q", segment)
		}
	}
	for _, mount := range []string{"/transit", "transit/", "transit//nested", "transit/../auth", "transit/%2e%2e"} {
		cfg := base
		cfg.TransitMount = mount
		if p, err := New(cfg); err == nil || p != nil {
			t.Fatalf("accepted mount %q", mount)
		}
	}
	base.TransitMount = "team/transit"
	if _, err := New(base); err != nil {
		t.Fatal(err)
	}
	base.Token = "inline-token"
	if _, err := New(base); err == nil {
		t.Fatal("accepted both token sources")
	}
	base.TokenFile = ""
	for _, token := range []string{"", " ", "header\r\ninjection", "space inside", strings.Repeat("x", maxResponseBytes+1)} {
		base.Token = token
		if _, err := New(base); err == nil {
			t.Fatal("accepted invalid inline token")
		}
	}
	base.Token = ""
	base.TokenFile = " "
	if _, err := New(base); err == nil {
		t.Fatal("accepted missing token file")
	}
}

func TestLocalHTTP(t *testing.T) {
	for _, address := range []string{"http://localhost:18200", "http://127.0.0.1:18200", "http://[::1]:18200", "http://vault:8200"} {
		p, err := New(Config{Address: address, TransitMount: "transit", KeyName: "key", TokenFile: "/external/token"})
		if err != nil {
			t.Fatal(err)
		}
		if p.client.Transport.(*http.Transport).Proxy != nil {
			t.Fatal("local HTTP may expose tokens to an environment proxy")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "local-token" || r.URL.Path != "/v1/transit/encrypt/key" {
			t.Error("incorrect local HTTP request")
		}
		_, _ = io.WriteString(w, `{"data":{"ciphertext":"vault:v1:YQ=="}}`)
	}))
	defer server.Close()
	token := filepath.Join(t.TempDir(), "token")
	writeToken(t, token, "local-token")
	p, err := New(Config{Address: server.URL, TransitMount: "transit", KeyName: "key", TokenFile: token})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPFailuresAreSafeAndNotRetried(t *testing.T) {
	for _, status := range []int{301, 307, 400, 403, 404, 429, 500, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			p, _ := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "https://must-not-follow.invalid")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"errors":["leaked-token-and-DEK"]}`)
			})
			_, err := p.WrapDEK(t.Context(), make([]byte, 32))
			var failure *RequestError
			if !errors.As(err, &failure) || failure.StatusCode != status || failure.Code != "HTTPError" {
				t.Fatalf("unexpected error: %v", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("calls=%d", calls.Load())
			}
			var logs bytes.Buffer
			slog.New(slog.NewJSONHandler(&logs, nil)).Error("operation failed", "error", err)
			if strings.Contains(logs.String(), "leaked") || !strings.Contains(logs.String(), `"provider":"hashicorp_vault"`) {
				t.Fatal("unsafe or unstructured error")
			}
		})
	}
}

func TestMalformedResponses(t *testing.T) {
	for _, payload := range []string{``, `{`, `null`, `{}`, `{"data":null}`, `{"data":{"ciphertext":""}}`, `{"data":{"ciphertext":"vault:v0:YQ=="}}`, `{"data":{"ciphertext":"vault:v1:!"}}`, `{"data":{"ciphertext":123}}`, `{"data":{"ciphertext":"vault:v1:YQ=="}} {}`, strings.Repeat("x", maxResponseBytes+1)} {
		p, _ := testProvider(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, payload) })
		if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
	for _, plaintext := range []string{"", "!", base64.StdEncoding.EncodeToString(make([]byte, 16)), base64.StdEncoding.EncodeToString(make([]byte, 33)), base64.StdEncoding.EncodeToString(make([]byte, 32)) + "!"} {
		p, _ := testProvider(t, func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"plaintext": plaintext}})
		})
		if dek, err := p.UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("vault:v2:YQ==")}); err == nil || dek != nil {
			t.Fatal("invalid DEK accepted")
		}
	}
}

func TestTokenFailureAndInvalidInputDoNotSendRequests(t *testing.T) {
	var calls atomic.Int32
	p, tokenFile := testProvider(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) })
	for _, token := range []string{"", " \n", "header\r\ninjection", "space inside", strings.Repeat("x", maxResponseBytes+1)} {
		writeToken(t, tokenFile, token)
		if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	if err := os.Remove(tokenFile); err != nil {
		t.Fatal(err)
	}
	if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
		t.Fatal("missing token accepted")
	}
	p.tokenFile = t.TempDir()
	if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
		t.Fatal("directory token accepted")
	}
	p.tokenFile = tokenFile
	writeToken(t, tokenFile, "valid")
	if _, err := p.WrapDEK(t.Context(), make([]byte, 16)); err == nil {
		t.Fatal("AES-128 DEK accepted")
	}
	for _, wrapped := range []secrets.WrappedKey{{KeyVersion: 2, Ciphertext: []byte("vault:v1:YQ==")}, {KeyVersion: 1, Ciphertext: []byte("not-vault")}} {
		if _, err := p.UnwrapDEK(t.Context(), wrapped); err == nil {
			t.Fatal("bad metadata accepted")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.WrapDEK(ctx, make([]byte, 32)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid input sent %d requests", calls.Load())
	}
}

func TestTLSCancellationAndTruncatedResponse(t *testing.T) {
	p, _ := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if _, err := p.WrapDEK(ctx, make([]byte, 32)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
	// A fresh production client must reject the test server's untrusted CA.
	q, err := New(Config{Address: strings.TrimSuffix(p.baseURL, "/v1/team/transit"), TransitMount: "transit", KeyName: "key", TokenFile: p.tokenFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	r, _ := testProvider(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, `{"data":`)
	})
	if _, err := r.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
		t.Fatal("truncated response accepted")
	}
}

func TestEnvelopeRoundTripAndTokenRotation(t *testing.T) {
	local, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: bytes.Repeat([]byte{3}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wantToken atomic.Value
	wantToken.Store("test-token")
	var calls atomic.Int32
	p, tokenFile := testProvider(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("X-Vault-Token") != wantToken.Load().(string) || r.Header.Get("X-Vault-Request") != "true" {
			t.Error("wrong method or headers")
			w.WriteHeader(403)
			return
		}
		var input transitRequest
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if input.AssociatedData != base64.StdEncoding.EncodeToString([]byte("oma-dek-v1")) {
			t.Error("missing purpose AAD")
		}
		data := transitData{}
		switch r.URL.Path {
		case "/v1/team/transit/encrypt/oma-dek":
			dek, err := base64.StdEncoding.DecodeString(input.Plaintext)
			if err != nil || len(dek) != 32 {
				t.Error("business payload reached Transit")
				w.WriteHeader(400)
				return
			}
			wrapped, err := local.WrapDEK(r.Context(), dek)
			clear(dek)
			if err != nil {
				t.Error(err)
				return
			}
			data.Ciphertext = "vault:v7:" + base64.StdEncoding.EncodeToString(wrapped.Ciphertext)
		case "/v1/team/transit/decrypt/oma-dek":
			blob, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(input.Ciphertext, "vault:v7:"))
			if err != nil {
				w.WriteHeader(400)
				return
			}
			dek, err := local.UnwrapDEK(r.Context(), secrets.WrappedKey{Ciphertext: blob, KeyVersion: 1})
			if err != nil {
				w.WriteHeader(400)
				return
			}
			data.Plaintext = base64.StdEncoding.EncodeToString(dek)
			clear(dek)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
			return
		}
		_ = json.NewEncoder(w).Encode(struct {
			Data transitData `json:"data"`
		}{data})
	})
	svc := secrets.NewService(p)
	binding := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "vault", CredentialExternalID: "credential"}
	envelope, err := svc.Seal(t.Context(), binding, []byte("business-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if envelope.KeyProvider != "hashicorp_vault" || envelope.KeyVersion != 1 || !strings.HasPrefix(string(envelope.WrappedDEK), "vault:v7:") {
		t.Fatal("incorrect persisted metadata")
	}
	// Replace the token file atomically; no provider recreation or SetToken.
	writeToken(t, tokenFile+".next", " next-token\n")
	if err := os.Rename(tokenFile+".next", tokenFile); err != nil {
		t.Fatal(err)
	}
	wantToken.Store("next-token")
	plain, err := svc.Open(t.Context(), binding, envelope)
	if err != nil || string(plain) != "business-secret" {
		t.Fatalf("roundtrip: %v", err)
	}
	clear(plain)
	wrongBinding := binding
	wrongBinding.WorkspaceUUID = "other"
	if _, err := svc.Open(t.Context(), wrongBinding, envelope); err == nil {
		t.Fatal("cross-workspace envelope accepted")
	}
	envelope.Ciphertext[0] ^= 1
	if _, err := svc.Open(t.Context(), binding, envelope); err == nil {
		t.Fatal("tampering accepted")
	}
	count := calls.Load()
	envelope.KeyProvider = "local"
	if _, err := svc.Open(t.Context(), binding, envelope); !errors.Is(err, secrets.ErrKeyProviderMismatch) || count != calls.Load() {
		t.Fatal("provider mismatch not rejected locally")
	}
}

func testProvider(t *testing.T, handler http.HandlerFunc) (*Provider, string) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = nil
	server.StartTLS()
	t.Cleanup(server.Close)
	tokenFile := filepath.Join(t.TempDir(), "token")
	writeToken(t, tokenFile, "test-token")
	p, err := New(Config{Address: server.URL, TransitMount: "team/transit", KeyName: "oma-dek", TokenFile: tokenFile})
	if err != nil {
		t.Fatal(err)
	}
	p.client.Transport = server.Client().Transport
	t.Cleanup(p.client.CloseIdleConnections)
	return p, tokenFile
}

func writeToken(t *testing.T, path, token string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCustomCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "inline-token" || r.URL.Path != "/v1/transit/encrypt/key" {
			t.Error("incorrect token or default mount")
		}
		_, _ = io.WriteString(w, `{"data":{"ciphertext":"vault:v1:YQ=="}}`)
	}))
	defer server.Close()
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	cfg := Config{Address: server.URL, KeyName: "key", Token: " inline-token\n", CAFile: caFile}
	if _, err := New(cfg); err == nil {
		t.Fatal("missing CA file accepted")
	}
	writeToken(t, caFile, "not-a-certificate")
	if _, err := New(cfg); err == nil || strings.Contains(err.Error(), "not-a-certificate") {
		t.Fatal("invalid CA accepted or contents leaked")
	}
	writeToken(t, caFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
	cfg.Address = strings.Replace(server.URL, "127.0.0.1", "localhost", 1)
	p, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.client.CloseIdleConnections)
	if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err == nil {
		t.Fatal("CA trust bypassed hostname validation")
	}
	cfg.Address = server.URL
	p, err = New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.client.CloseIdleConnections)
	if _, err := p.WrapDEK(t.Context(), make([]byte, 32)); err != nil {
		t.Fatalf("configured CA was not trusted: %v", err)
	}
}
