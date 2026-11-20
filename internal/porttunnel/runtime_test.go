package porttunnel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"
)

func runtimeTestDeps(t *testing.T) Deps {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return Deps{Redis: StartTestRedis(t), NATS: StartTestNATS(t), Logger: logger, ForwardLogger: logger}
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

func sendSignedForward(t *testing.T, addr, owner string, signer *ForwardSigner) (*http.Response, error) {
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
	request.Header.Set(forwardSignatureHeader, signer.sign(http.MethodPost, path, owner, encoded))
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
	_, err = Start(ctx, runtimeTestDeps(t), Options{
		AdvertiseAddr: taken.Addr().String(),
		ListenAddr:    taken.Addr().String(),
		ForwardKey:    []byte(strings.Repeat("k", 32)),
	})
	if err == nil || !strings.Contains(err.Error(), "listen session port forwarding") {
		t.Fatalf("err = %v", err)
	}
}

func TestStartWithoutForwardingRoutesThroughLocalRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime, err := Start(ctx, runtimeTestDeps(t), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Router != Router(runtime.Registry) {
		t.Fatalf("router = %T, want local registry", runtime.Router)
	}
	if owner := runtime.Registry.Owner(); !strings.HasPrefix(owner, "#") || len(owner) == 1 {
		t.Fatalf("owner = %q", owner)
	}
}

func TestStartServesSignedForwardsUntilContextEnds(t *testing.T) {
	addr := freeLocalAddr(t)
	key := []byte(strings.Repeat("k", 32))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	runtime, err := Start(ctx, runtimeTestDeps(t), Options{
		AdvertiseAddr:       addr,
		ListenAddr:          addr,
		ForwardKey:          key,
		MaxRequestBodyBytes: 1 << 20,
		ResponseIdleTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.Router.(*ClusterRouter); !ok {
		t.Fatalf("router = %T, want cluster router", runtime.Router)
	}
	owner := runtime.Registry.Owner()
	if !strings.HasPrefix(owner, addr+"#") {
		t.Fatalf("owner = %q", owner)
	}
	response, err := sendSignedForward(t, addr, owner, NewForwardSigner(key))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound || !strings.Contains(string(body), forwardNoTunnel) {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		response, err := sendSignedForward(t, addr, owner, NewForwardSigner(key))
		if errors.Is(err, syscall.ECONNREFUSED) {
			return
		}
		if response != nil {
			response.Body.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("forwarding still reachable after shutdown: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
