package aliyunkms

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kms "github.com/alibabacloud-go/kms-20160120/v4/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"
	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

type fakeClient struct {
	encrypt func(context.Context, *kms.EncryptRequest) (*kms.EncryptResponse, error)
	decrypt func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error)
	calls   int
}

func (f *fakeClient) EncryptWithContext(ctx context.Context, r *kms.EncryptRequest, _ *dara.RuntimeOptions) (*kms.EncryptResponse, error) {
	f.calls++
	return f.encrypt(ctx, r)
}
func (f *fakeClient) DecryptWithContext(ctx context.Context, r *kms.DecryptRequest, _ *dara.RuntimeOptions) (*kms.DecryptResponse, error) {
	f.calls++
	return f.decrypt(ctx, r)
}

func TestProviderFailsClosed(t *testing.T) {
	for _, code := range []string{"Forbidden.NoPermission", "Rejected.Disabled", "Forbidden.KeyNotFound", "InvalidCiphertext", "unknown-secret-code"} {
		t.Run(code, func(t *testing.T) {
			failure := &dara.SDKError{Code: dara.String(code), Message: dara.String("raw-secret-access-key-dek"), Data: dara.String("raw-secret-response")}
			fake := &fakeClient{encrypt: func(context.Context, *kms.EncryptRequest) (*kms.EncryptResponse, error) { return nil, failure }, decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) { return nil, failure }}
			provider := &Provider{client: fake, keyID: "test-key", canonicalKeyID: "test-key"}
			svc := secrets.NewService(provider)
			binding := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "v", CredentialExternalID: "c"}
			env, err := svc.Seal(t.Context(), binding, []byte("business-secret"))
			if err == nil || len(env.Ciphertext) != 0 || strings.Contains(fmt.Sprintf("%+v", err), "raw-secret") || errors.Unwrap(err) == failure {
				t.Fatalf("seal did not fail safely: %v", err)
			}
			plain, err := svc.Open(t.Context(), binding, secrets.Envelope{FormatVersion: 1, KeyProvider: providerName, KeyVersion: 1, WrappedDEK: []byte("opaque")})
			if err == nil || plain != nil || strings.Contains(err.Error(), "raw-secret") {
				t.Fatalf("open did not fail safely: %v", err)
			}
		})
	}
}

func TestProviderRejectsMalformedResponses(t *testing.T) {
	for _, tc := range []struct{ name, key, plain string }{
		{"wrong key", "other-key", base64.StdEncoding.EncodeToString(make([]byte, 32))},
		{"short DEK", "test-key", base64.StdEncoding.EncodeToString(make([]byte, 16))},
		{"bad base64", "test-key", "secret%%%"},
		{"empty DEK", "test-key", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := &kms.DecryptResponse{Body: &kms.DecryptResponseBody{KeyId: &tc.key, Plaintext: &tc.plain}}
			provider := &Provider{canonicalKeyID: "test-key", client: &fakeClient{decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) { return response, nil }}}
			dek, err := provider.UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")})
			if err == nil || dek != nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
	for _, response := range []*kms.EncryptResponse{nil, {}, {Body: &kms.EncryptResponseBody{KeyId: dara.String("other-key")}}, {Body: &kms.EncryptResponseBody{KeyId: dara.String("test-key"), CiphertextBlob: dara.String("%%%")}}} {
		provider := &Provider{canonicalKeyID: "test-key", client: &fakeClient{encrypt: func(context.Context, *kms.EncryptRequest) (*kms.EncryptResponse, error) { return response, nil }}}
		wrapped, err := provider.WrapDEK(t.Context(), make([]byte, 32))
		if err == nil || len(wrapped.Ciphertext) != 0 {
			t.Fatal("invalid encrypt response accepted")
		}
	}
}

func TestProviderCancellationAndInvalidInputs(t *testing.T) {
	fake := &fakeClient{encrypt: func(ctx context.Context, _ *kms.EncryptRequest) (*kms.EncryptResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	p := &Provider{client: fake, keyID: "test-key"}
	if _, err := p.WrapDEK(t.Context(), make([]byte, 16)); err == nil {
		t.Fatal("accepted non-DEK")
	}
	for _, w := range []secrets.WrappedKey{{KeyVersion: 2, Ciphertext: []byte("x")}, {KeyVersion: 1}} {
		if _, err := p.UnwrapDEK(t.Context(), w); err == nil {
			t.Fatal("accepted invalid wrapped key")
		}
	}
	if fake.calls != 0 {
		t.Fatal("invalid input reached KMS")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Millisecond)
	defer cancel()
	if _, err := p.WrapDEK(ctx, make([]byte, 32)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestConfigurationRejections(t *testing.T) {
	t.Setenv("DEBUG", "")
	for _, endpoint := range []string{"", "http://kms.example", "https://user:password@kms.example", "https://kms.example/path", "https://kms.example?secret=1", "https://kms.example#x"} {
		if _, err := New(Config{Endpoint: endpoint, KeyID: "key"}); err == nil {
			t.Fatalf("accepted invalid endpoint %q", endpoint)
		}
	}
	for _, id := range []string{"", "alias/foo", "acs:kms::account:key/id", "acs:kms:region:account:key/", "acs:ram:region:account:key/id"} {
		if _, err := canonicalKeyID(id); err == nil {
			t.Fatal("accepted invalid key identifier")
		}
	}
	if _, err := New(Config{Endpoint: "kms.example", KeyID: "key", AccessKeyID: "partial"}); err == nil {
		t.Fatal("accepted partial credentials")
	}
	t.Setenv("DEBUG", "dara")
	if _, err := New(Config{Endpoint: "kms.example", KeyID: "key"}); err == nil {
		t.Fatal("accepted unsafe SDK wire logging")
	}
}

func TestEndpointHost(t *testing.T) {
	for _, endpoint := range []string{"https://kms.example//", "https://kms.example/path/", "https://kms.example/?query=1", "https://kms.example/#fragment", "https://kms.example/?"} {
		if _, err := endpointHost(endpoint); err == nil {
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	for _, endpoint := range []string{"kms.example", "kms.example/", "https://kms.example", "https://kms.example/", "https://kms.example:8443/"} {
		want := "kms.example"
		if strings.Contains(endpoint, ":8443") {
			want += ":8443"
		}
		if got, err := endpointHost(endpoint); err != nil || got != want {
			t.Errorf("endpointHost(%q) = %q, %v; want %q", endpoint, got, err, want)
		}
	}
}

func TestEnvelopeRoundTripAndTamper(t *testing.T) {
	local, err := localkeys.New(localkeys.KeyMaterial{Version: 1, KEK: bytes.Repeat([]byte{7}, 32)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeClient{}
	fake.encrypt = func(ctx context.Context, r *kms.EncryptRequest) (*kms.EncryptResponse, error) {
		dek, err := base64.StdEncoding.DecodeString(dara.StringValue(r.Plaintext))
		if err != nil {
			t.Fatal(err)
		}
		defer clear(dek)
		if len(dek) != 32 || r.EncryptionContext["purpose"] != "oma-dek-v1" {
			t.Fatal("KMS received a business payload or missing purpose")
		}
		wrapped, err := local.WrapDEK(ctx, dek)
		if err != nil {
			return nil, err
		}
		return &kms.EncryptResponse{Body: &kms.EncryptResponseBody{KeyId: dara.String("key"), CiphertextBlob: dara.String(base64.StdEncoding.EncodeToString(wrapped.Ciphertext))}}, nil
	}
	fake.decrypt = func(ctx context.Context, r *kms.DecryptRequest) (*kms.DecryptResponse, error) {
		wrapped, err := base64.StdEncoding.DecodeString(dara.StringValue(r.CiphertextBlob))
		if err != nil {
			return nil, err
		}
		dek, err := local.UnwrapDEK(ctx, secrets.WrappedKey{Ciphertext: wrapped, KeyVersion: 1})
		defer clear(dek)
		if err != nil {
			return nil, err
		}
		return &kms.DecryptResponse{Body: &kms.DecryptResponseBody{KeyId: dara.String("key"), Plaintext: dara.String(base64.StdEncoding.EncodeToString(dek))}}, nil
	}
	p := &Provider{client: fake, keyID: "acs:kms:region:account:key/key", canonicalKeyID: "key"}
	svc := secrets.NewService(p)
	binding := secrets.Binding{OrganizationUUID: "org", WorkspaceUUID: "ws", VaultExternalID: "vault", CredentialExternalID: "credential"}
	env, err := svc.Seal(t.Context(), binding, []byte("business-secret"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := svc.Open(t.Context(), binding, env)
	if err != nil || string(opened) != "business-secret" {
		t.Fatalf("roundtrip: %v", err)
	}
	for _, mutate := range []func(*secrets.Envelope){func(e *secrets.Envelope) { e.Ciphertext[0] ^= 1 }, func(e *secrets.Envelope) { e.WrappedDEK[0] ^= 1 }, func(e *secrets.Envelope) { e.KeyProvider = "local" }} {
		tampered := env
		tampered.Ciphertext = bytes.Clone(env.Ciphertext)
		tampered.WrappedDEK = bytes.Clone(env.WrappedDEK)
		mutate(&tampered)
		if opened, err := svc.Open(t.Context(), binding, tampered); err == nil || opened != nil {
			t.Fatal("tampered envelope opened")
		}
	}
	binding.WorkspaceUUID = "other-workspace"
	if opened, err := svc.Open(t.Context(), binding, env); err == nil || opened != nil {
		t.Fatal("tenant swap accepted")
	}
}

func TestHTTPClientRejectsRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("DEK followed redirect") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, source.URL, strings.NewReader("sensitive"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := newHTTPClient("")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Call(request, http.DefaultTransport.(*http.Transport).Clone())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatal("redirect was followed")
	}
}

func TestWorkloadIdentitySelection(t *testing.T) {
	for _, key := range []string{"ALIBABA_CLOUD_OIDC_TOKEN_FILE", "ALIBABA_CLOUD_OIDC_PROVIDER_ARN", "ALIBABA_CLOUD_ROLE_ARN", "ALIBABA_CLOUD_ECS_METADATA_DISABLED"} {
		t.Setenv(key, "")
	}
	// This constructs providers only; no cloud metadata/STS calls occur.
	credential, err := newCredential(Config{})
	if err != nil || dara.StringValue(credential.GetType()) != "ecs_ram_role" {
		t.Fatalf("ECS provider: %v", err)
	}
	t.Setenv("ALIBABA_CLOUD_ROLE_ARN", "acs:ram::123:role/test")
	if _, err := newCredential(Config{}); err == nil {
		t.Fatal("partial OIDC identity fell back to ECS")
	}
	t.Setenv("ALIBABA_CLOUD_OIDC_PROVIDER_ARN", "acs:ram::123:oidc-provider/test")
	t.Setenv("ALIBABA_CLOUD_OIDC_TOKEN_FILE", "/not-read-during-construction")
	credential, err = newCredential(Config{})
	if err != nil || dara.StringValue(credential.GetType()) != "oidc_role_arn" {
		t.Fatalf("ACK provider: %v", err)
	}
	for _, token := range []string{"", "test-sts-token"} {
		credential, err = newCredential(Config{AccessKeyID: "test-ak", AccessKeySecret: "test-sk", SecurityToken: token})
		if err != nil {
			t.Fatal(err)
		}
		value, err := credential.GetCredential()
		if err != nil || dara.StringValue(value.AccessKeyId) != "test-ak" || dara.StringValue(value.SecurityToken) != token {
			t.Fatal("explicit credential selection failed")
		}
	}
}

func TestInvokeStopsWithoutRetry(t *testing.T) {
	transient := &url.Error{Op: "Post", URL: "https://kms.example", Err: &net.DNSError{Err: "no such host", Name: "kms.example"}}
	for _, tc := range []struct {
		name    string
		failure error
		calls   int
	}{
		{"service rejection", &dara.SDKError{Code: dara.String("Forbidden.NoPermission"), StatusCode: dara.Int(403)}, 1},
		{"certificate", &url.Error{Op: "Post", URL: "https://kms.example", Err: x509UnknownAuthority()}, 1},
		{"attempts exhausted", transient, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeClient{decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) { return nil, tc.failure }}
			p := retryingProvider(fake, 0)
			_, err := p.UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")})
			var failure *KMSError
			if !errors.As(err, &failure) || failure.Attempts != tc.calls || fake.calls != tc.calls {
				t.Fatalf("calls = %d, err = %v", fake.calls, err)
			}
		})
	}
	t.Run("attempt timeout", func(t *testing.T) {
		fake := &fakeClient{decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) {
			return nil, &url.Error{Op: "Post", URL: "https://kms.example", Err: timeoutError{}}
		}}
		if _, err := retryingProvider(fake, 0).UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")}); !errors.Is(err, context.DeadlineExceeded) || fake.calls != 1 {
			t.Fatalf("calls = %d, err = %v", fake.calls, err)
		}
	})
	t.Run("unexpected key", func(t *testing.T) {
		fake := &fakeClient{decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) {
			return &kms.DecryptResponse{Body: &kms.DecryptResponseBody{KeyId: dara.String("other-key")}}, nil
		}}
		if _, err := retryingProvider(fake, 0).UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")}); err == nil || fake.calls != 1 {
			t.Fatalf("calls = %d, err = %v", fake.calls, err)
		}
	})
	t.Run("budget too short", func(t *testing.T) {
		fake := &fakeClient{decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) { return nil, transient }}
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		if _, err := retryingProvider(fake, 0).UnwrapDEK(ctx, secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")}); err == nil || fake.calls != 1 {
			t.Fatalf("calls = %d, err = %v", fake.calls, err)
		}
	})
	t.Run("parent cancelled during backoff", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		fake := &fakeClient{decrypt: func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) {
			time.AfterFunc(20*time.Millisecond, cancel)
			return nil, transient
		}}
		_, err := retryingProvider(fake, time.Second).UnwrapDEK(ctx, secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")})
		if !errors.Is(err, context.Canceled) || fake.calls != 1 {
			t.Fatalf("calls = %d, err = %v", fake.calls, err)
		}
	})
	t.Run("budget exhausted", func(t *testing.T) {
		fake := &fakeClient{decrypt: func(ctx context.Context, _ *kms.DecryptRequest) (*kms.DecryptResponse, error) {
			<-ctx.Done()
			return nil, transient
		}}
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()
		if _, err := retryingProvider(fake, 0).UnwrapDEK(ctx, secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")}); !errors.Is(err, context.DeadlineExceeded) || fake.calls != 1 {
			t.Fatalf("calls = %d, err = %v", fake.calls, err)
		}
	})
}

func TestInvokeRetriesTransientFailure(t *testing.T) {
	failures := []error{
		&url.Error{Op: "Post", URL: "https://kms.example", Err: &net.DNSError{Err: "no such host", Name: "kms.example"}},
		&dara.SDKError{Code: dara.String("Rejected.Throttling"), StatusCode: dara.Int(400)},
	}
	dek := bytes.Repeat([]byte{9}, 32)
	decrypts := &fakeClient{}
	decrypts.decrypt = func(context.Context, *kms.DecryptRequest) (*kms.DecryptResponse, error) {
		if decrypts.calls <= len(failures) {
			return nil, failures[decrypts.calls-1]
		}
		return &kms.DecryptResponse{Body: &kms.DecryptResponseBody{KeyId: dara.String("test-key"), Plaintext: dara.String(base64.StdEncoding.EncodeToString(dek))}}, nil
	}
	got, err := retryingProvider(decrypts, 0).UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")})
	if err != nil || !bytes.Equal(got, dek) || decrypts.calls != 3 {
		t.Fatalf("calls = %d, err = %v", decrypts.calls, err)
	}
	encrypts := &fakeClient{}
	encrypts.encrypt = func(context.Context, *kms.EncryptRequest) (*kms.EncryptResponse, error) {
		if encrypts.calls == 1 {
			return &kms.EncryptResponse{Body: &kms.EncryptResponseBody{KeyId: dara.String("test-key"), CiphertextBlob: dara.String(base64.StdEncoding.EncodeToString([]byte("first")))}}, io.ErrUnexpectedEOF
		}
		return &kms.EncryptResponse{Body: &kms.EncryptResponseBody{KeyId: dara.String("test-key"), CiphertextBlob: dara.String(base64.StdEncoding.EncodeToString([]byte("second")))}}, nil
	}
	wrapped, err := retryingProvider(encrypts, 0).WrapDEK(t.Context(), dek)
	if err != nil || string(wrapped.Ciphertext) != "second" || encrypts.calls != 2 {
		t.Fatalf("calls = %d, err = %v", encrypts.calls, err)
	}
}

func TestProviderRetriesResponseTruncatedByTLSServer(t *testing.T) {
	dek := bytes.Repeat([]byte{5}, 32)
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"KeyId":`))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"KeyId": "test-key", "Plaintext": base64.StdEncoding.EncodeToString(dek), "RequestId": "request-1"})
	}))
	defer server.Close()
	t.Setenv("DEBUG", "")
	provider, err := New(Config{Endpoint: server.Listener.Addr().String(), KeyID: "test-key", AccessKeyID: "offline", AccessKeySecret: "offline", CA: serverCA(server)})
	if err != nil {
		t.Fatal(err)
	}
	provider.retryDelays = nil
	_, err = provider.UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")})
	var failure *KMSError
	if !errors.As(err, &failure) || failure.Code != "NetworkError.ConnectionReset" {
		t.Fatalf("truncated response classified as %v", err)
	}
	provider.retryDelays = []time.Duration{0}
	requests.Store(0)
	got, err := provider.UnwrapDEK(t.Context(), secrets.WrappedKey{KeyVersion: 1, Ciphertext: []byte("opaque")})
	if err != nil || !bytes.Equal(got, dek) || requests.Load() != 2 {
		t.Fatalf("requests = %d, err = %v", requests.Load(), err)
	}
}

func TestHTTPClientRejectsUntrustedCA(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("request reached untrusted server") }))
	defer server.Close()
	client, err := newHTTPClient("")
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("sensitive"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Call(request, nil)
	if err == nil {
		response.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
	if failure, retryable := classifyFailure(err); failure.Code != "NetworkError.TLSCertificate" || retryable {
		t.Fatalf("classified as %q, %t", failure.Code, retryable)
	}
}

func TestNewRejectsInvalidCA(t *testing.T) {
	t.Setenv("DEBUG", "")
	if _, err := New(Config{Endpoint: "kms.example", KeyID: "key", AccessKeyID: "offline", AccessKeySecret: "offline", CA: "not a certificate"}); err == nil {
		t.Fatal("accepted invalid CA")
	}
}

func TestHTTPClientReusesConnection(t *testing.T) {
	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	defer server.Close()
	client, err := newHTTPClient(serverCA(server))
	if err != nil {
		t.Fatal(err)
	}
	if client.client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("KMS transport must not use a proxy")
	}
	for range 2 {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, strings.NewReader("payload"))
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Call(request, http.DefaultTransport.(*http.Transport).Clone())
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
	}
	if connections.Load() != 1 {
		t.Fatalf("connections = %d", connections.Load())
	}
}

func retryingProvider(fake *fakeClient, delay time.Duration) *Provider {
	return &Provider{client: fake, keyID: "test-key", canonicalKeyID: "test-key", retryDelays: []time.Duration{delay, delay}}
}

func serverCA(server *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}

func x509UnknownAuthority() error {
	return &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}
}
