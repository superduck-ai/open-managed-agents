package porttunnel

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const forwardTestOwner = "10.0.0.2:38090#instance-b"

func forwardTestServer(t *testing.T, signer *ForwardSigner) *httptest.Server {
	t.Helper()
	registry := NewRegistry(forwardTestOwner, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(NewForwardServer(registry, signer, 1<<20, time.Second, nil).Handler())
	t.Cleanup(server.Close)
	return server
}

func signedForwardRequest(t *testing.T, server *httptest.Server, signer *ForwardSigner, method, signedPath, sentPath, owner string, request forwardRequest) *http.Request {
	t.Helper()
	encoded, err := encodeForwardRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	forward, err := http.NewRequest(method, server.URL+sentPath, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	forward.Header.Set(forwardRequestHeader, encoded)
	forward.Header.Set(forwardSignatureHeader, signer.sign(method, signedPath, owner, encoded))
	return forward
}

func TestForwardServerRejectsInvalidSignatures(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	signer := NewForwardSigner(key)
	server := forwardTestServer(t, signer)
	path := forwardHTTPPath(TestCodeSessionUUID, 4000)
	request := forwardRequest{Method: http.MethodGet, Path: "/"}
	past := NewForwardSigner(key)
	past.now = func() time.Time { return time.Now().Add(-forwardSignatureSkew - time.Second) }
	future := NewForwardSigner(key)
	future.now = func() time.Time { return time.Now().Add(forwardSignatureSkew + time.Second) }
	for name, build := range map[string]func() *http.Request{
		"missing": func() *http.Request {
			forward := signedForwardRequest(t, server, signer, http.MethodPost, path, path, forwardTestOwner, request)
			forward.Header.Del(forwardSignatureHeader)
			return forward
		},
		"forged key": func() *http.Request {
			return signedForwardRequest(t, server, NewForwardSigner([]byte(strings.Repeat("x", 32))), http.MethodPost, path, path, forwardTestOwner, request)
		},
		"expired": func() *http.Request {
			return signedForwardRequest(t, server, past, http.MethodPost, path, path, forwardTestOwner, request)
		},
		"future": func() *http.Request {
			return signedForwardRequest(t, server, future, http.MethodPost, path, path, forwardTestOwner, request)
		},
		"other owner": func() *http.Request {
			return signedForwardRequest(t, server, signer, http.MethodPost, path, path, "10.0.0.2:38090#restarted", request)
		},
		"other port": func() *http.Request {
			return signedForwardRequest(t, server, signer, http.MethodPost, path, forwardHTTPPath(TestCodeSessionUUID, 5000), forwardTestOwner, request)
		},
		"other session": func() *http.Request {
			return signedForwardRequest(t, server, signer, http.MethodPost, path, forwardHTTPPath("00000000-0000-4000-8000-000000000000", 4000), forwardTestOwner, request)
		},
		"tampered request": func() *http.Request {
			forward := signedForwardRequest(t, server, signer, http.MethodPost, path, path, forwardTestOwner, request)
			tampered, err := encodeForwardRequest(forwardRequest{Method: http.MethodDelete, Path: "/admin"})
			if err != nil {
				t.Fatal(err)
			}
			forward.Header.Set(forwardRequestHeader, tampered)
			return forward
		},
		"malformed": func() *http.Request {
			forward := signedForwardRequest(t, server, signer, http.MethodPost, path, path, forwardTestOwner, request)
			forward.Header.Set(forwardSignatureHeader, "v1.not-a-time.sig")
			return forward
		},
	} {
		t.Run(name, func(t *testing.T) {
			response, err := http.DefaultClient.Do(build())
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), forwardUnauthorized) {
				t.Fatalf("status=%d body=%s", response.StatusCode, body)
			}
		})
	}
}

func TestForwardServerAcceptsValidSignatureAndReportsMissingTunnel(t *testing.T) {
	signer := NewForwardSigner([]byte(strings.Repeat("k", 32)))
	server := forwardTestServer(t, signer)
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		path := forwardHTTPPath(TestCodeSessionUUID, 4000)
		if method == http.MethodGet {
			path = forwardWebSocketPath(TestCodeSessionUUID, 4000)
		}
		response, err := http.DefaultClient.Do(signedForwardRequest(t, server, signer, method, path, path, forwardTestOwner, forwardRequest{Method: http.MethodGet, Path: "/"}))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound || !strings.Contains(string(body), forwardNoTunnel) {
			t.Fatalf("%s status=%d body=%s", method, response.StatusCode, body)
		}
	}
}

func TestForwardClientBypassesProxiesAndOutlastsOwnerHeaderTimeout(t *testing.T) {
	client := NewForwardClient(NewForwardSigner([]byte(strings.Repeat("k", 32))))
	transport, ok := client.http.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil || client.dialer.Proxy != nil {
		t.Fatal("forward client must not use environment proxies")
	}
	if transport.ResponseHeaderTimeout <= responseHeaderTimeout || client.dialer.HandshakeTimeout <= websocketOpenTimeout {
		t.Fatalf("forward header timeouts %s/%s must exceed owner waits %s/%s", transport.ResponseHeaderTimeout, client.dialer.HandshakeTimeout, responseHeaderTimeout, websocketOpenTimeout)
	}
	if ForwardDialTimeout > 2*time.Second {
		t.Fatalf("dial timeout %s", ForwardDialTimeout)
	}
}

func TestForwardFailureCodesRoundTrip(t *testing.T) {
	for _, err := range []error{ErrTooManyRequests, ErrTooManyWebSockets, &UpstreamError{Message: "response header timeout"}, context.DeadlineExceeded} {
		_, failure := forwardFailureFor(err)
		mapped := failure.err()
		if mapped.Error() != err.Error() {
			t.Fatalf("%v mapped to %v", err, mapped)
		}
	}
	if _, failure := forwardFailureFor(ErrNoTunnel); failure.err() != ErrTunnelGone {
		t.Fatalf("no tunnel mapped to %v", failure.err())
	}
	if !IsHeaderTimeout((forwardFailure{Code: forwardUpstream, Message: "response header timeout"}).err()) {
		t.Fatal("header timeout lost across forward hop")
	}
}

func TestClusterRouterWaitOnlineReturnsWhenOwnerAlreadyElsewhere(t *testing.T) {
	_, redisClient := startRedis(t)
	owners := NewRedisOwnerStore(redisClient)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	local := NewRegistry("10.0.0.1:38090#instance-a", owners, NewNATSOnlineNotifier(startNATS(t)), logger)
	router := NewClusterRouter(local, owners, NewForwardClient(NewForwardSigner([]byte(strings.Repeat("k", 32)))), logger)
	if err := owners.Claim(context.Background(), TestCodeSessionUUID, forwardTestOwner); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := router.WaitOnline(ctx, TestCodeSessionUUID); err != nil {
		t.Fatalf("WaitOnline = %v", err)
	}
	tunnel, err := router.Lookup(context.Background(), TestCodeSessionUUID)
	if err != nil {
		t.Fatal(err)
	}
	if remote, ok := tunnel.(*remoteTunnel); !ok || remote.address != "10.0.0.2:38090" {
		t.Fatalf("lookup = %#v", tunnel)
	}
	if err := owners.Claim(context.Background(), TestCodeSessionUUID, local.Owner()); err != nil {
		t.Fatal(err)
	}
	if _, err := router.Lookup(context.Background(), TestCodeSessionUUID); err != ErrNoTunnel {
		t.Fatalf("stale self owner lookup = %v", err)
	}
}
