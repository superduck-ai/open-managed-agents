package porttunnel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

var runtimeTestKey = []byte(strings.Repeat("k", 32))

type runtimeHarness struct {
	redis *miniredis.Miniredis
	deps  Deps
}

func newRuntimeHarness(t *testing.T) runtimeHarness {
	t.Helper()
	instance, client := startRedis(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return runtimeHarness{redis: instance, deps: Deps{Redis: client, NATS: StartTestNATS(t), Logger: logger, ForwardLogger: logger}}
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func forwardingOptions(addr string) Options {
	return Options{
		AdvertiseAddr:       addr,
		ListenAddr:          addr,
		ForwardKey:          runtimeTestKey,
		MaxRequestBodyBytes: 1 << 20,
		ResponseIdleTimeout: time.Second,
	}
}

func connectRuntimeTunnel(t *testing.T, registry *Registry) *FakeClient {
	t.Helper()
	limits := Limits{KeepaliveInterval: time.Hour, MaxConcurrentRequests: 64, MaxWebSockets: 32}
	mux := http.NewServeMux()
	mux.Handle(Path, NewHandler(&FakeAuthenticator{}, registry, limits, slog.New(slog.NewTextHandler(io.Discard, nil))))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, _, err := DialFakeClient(server.URL, TestToken, TestCodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	WaitFor(t, func() bool {
		_, err := registry.Lookup(context.Background(), TestCodeSessionUUID)
		return err == nil
	})
	return client
}

func sendSignedForward(t *testing.T, addr, owner string) (*http.Response, error) {
	t.Helper()
	encoded, err := encodeForwardRequest(forwardRequest{Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	path := forwardHTTPPath(TestCodeSessionUUID, 4000)
	request, err := http.NewRequest(http.MethodPost, "http://"+addr+path, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set(forwardRequestHeader, encoded)
	request.Header.Set(forwardSignatureHeader, NewForwardSigner(runtimeTestKey).sign(http.MethodPost, path, owner, encoded))
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	return client.Do(request)
}

func TestStartFailsWhenForwardAddressIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = taken.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	_, err = Start(ctx, newRuntimeHarness(t).deps, forwardingOptions(taken.Addr().String()))
	if err == nil || !strings.Contains(err.Error(), "listen session port forwarding") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartWithoutForwardingDoesNotClaimOwner(t *testing.T) {
	harness := newRuntimeHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime, err := Start(ctx, harness.deps, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	if runtime.Router != Router(runtime.Registry) {
		t.Fatalf("router = %T, want local registry", runtime.Router)
	}
	connectRuntimeTunnel(t, runtime.Registry)
	if keys := harness.redis.Keys(); len(keys) != 0 {
		t.Fatalf("redis keys = %v, want none", keys)
	}
}

func TestStartWithForwardingClaimsOwnerAndServesSignedForwards(t *testing.T) {
	harness := newRuntimeHarness(t)
	addr := freeLocalAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime, err := Start(ctx, harness.deps, forwardingOptions(addr))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runtime.Close)
	if _, ok := runtime.Router.(*ClusterRouter); !ok {
		t.Fatalf("router = %T, want cluster router", runtime.Router)
	}
	owner := runtime.Registry.Owner()
	if !strings.HasPrefix(owner, addr+"#") {
		t.Fatalf("owner = %q", owner)
	}
	connectRuntimeTunnel(t, runtime.Registry)
	WaitFor(t, func() bool {
		got, _ := harness.redis.Get(ownerKey(TestCodeSessionUUID))
		return got == owner
	})
	response, err := sendSignedForward(t, addr, owner)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		t.Fatalf("signed forward rejected: status=%d", response.StatusCode)
	}
}

func TestRuntimeKeepsTunnelsUntilClose(t *testing.T) {
	harness := newRuntimeHarness(t)
	addr := freeLocalAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	runtime, err := Start(ctx, harness.deps, forwardingOptions(addr))
	if err != nil {
		t.Fatal(err)
	}
	client := connectRuntimeTunnel(t, runtime.Registry)
	WaitFor(t, func() bool { return harness.redis.Exists(ownerKey(TestCodeSessionUUID)) })
	cancel()
	time.Sleep(50 * time.Millisecond)
	if _, err := runtime.Registry.Lookup(context.Background(), TestCodeSessionUUID); err != nil {
		t.Fatalf("tunnel closed before Close: %v", err)
	}
	runtime.Close()
	select {
	case <-client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("tunnel client was not closed")
	}
	if _, err := runtime.Registry.Lookup(context.Background(), TestCodeSessionUUID); !errors.Is(err, ErrNoTunnel) {
		t.Fatalf("lookup after Close err = %v", err)
	}
	if harness.redis.Exists(ownerKey(TestCodeSessionUUID)) {
		t.Fatal("owner record survived Close")
	}
	if _, err := sendSignedForward(t, addr, runtime.Registry.Owner()); !errors.Is(err, syscall.ECONNREFUSED) {
		t.Fatalf("forward after Close err = %v, want connection refused", err)
	}
}
