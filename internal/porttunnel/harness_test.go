package porttunnel

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

const (
	TestToken           = "good-token"
	TestCodeSessionID   = "cse_test"
	TestCodeSessionUUID = "6f1c3f7e-0d1b-4f53-9d39-1b1e4f0e9a10"
)

type FakeAuthenticator struct {
	Revoked atomic.Bool
}

func (a *FakeAuthenticator) AuthenticateTunnel(_ context.Context, token, codeSessionExternalID string) (Identity, error) {
	if token != TestToken || codeSessionExternalID != TestCodeSessionID || a.Revoked.Load() {
		return Identity{}, ErrUnauthorized
	}
	return Identity{OrganizationUUID: "org", WorkspaceUUID: "workspace", CodeSessionUUID: TestCodeSessionUUID, CodeSessionExternalID: TestCodeSessionID}, nil
}

type TunnelServer struct {
	URL      string
	Registry *Registry
	Auth     *FakeAuthenticator
	Handler  *Handler
}

type TunnelServerOptions struct {
	Limits          Limits
	ReadIdleTimeout time.Duration
	StallLimit      time.Duration
	Revalidate      time.Duration
	Fallback        http.Handler
	Owner           string
	Owners          OwnerStore
	Notifier        OnlineNotifier
}

func StartTunnelServer(t *testing.T, options TunnelServerOptions) *TunnelServer {
	t.Helper()
	if options.Limits.KeepaliveInterval == 0 {
		options.Limits.KeepaliveInterval = time.Hour
	}
	if options.Limits.MaxConcurrentRequests == 0 {
		options.Limits.MaxConcurrentRequests = 64
	}
	if options.Limits.MaxWebSockets == 0 {
		options.Limits.MaxWebSockets = 32
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := NewRegistry(cmp.Or(options.Owner, "test-owner"), options.Owners, options.Notifier, logger)
	authenticator := &FakeAuthenticator{}
	handler := NewHandler(authenticator, registry, options.Limits, logger)
	if options.ReadIdleTimeout > 0 {
		handler.readIdleTimeout = options.ReadIdleTimeout
	}
	if options.StallLimit > 0 {
		handler.stallLimit = options.StallLimit
	}
	if options.Revalidate > 0 {
		handler.revalidateEvery = options.Revalidate
	}
	mux := http.NewServeMux()
	mux.Handle(Path, handler)
	if options.Fallback != nil {
		mux.Handle("/", options.Fallback)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return &TunnelServer{URL: server.URL, Registry: registry, Auth: authenticator, Handler: handler}
}

func (s *TunnelServer) Connect(t *testing.T) *FakeClient {
	t.Helper()
	return s.ConnectWithOptions(t, FakeClientOptions{})
}

func (s *TunnelServer) ConnectWithOptions(t *testing.T, options FakeClientOptions) *FakeClient {
	t.Helper()
	client, _, err := DialFakeClientWithOptions(s.URL, TestToken, TestCodeSessionID, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	WaitFor(t, func() bool {
		_, err := s.Registry.Lookup(context.Background(), TestCodeSessionUUID)
		return err == nil
	})
	return client
}

func (s *TunnelServer) Tunnel(t *testing.T) Tunnel {
	t.Helper()
	tunnel, err := s.Registry.Lookup(context.Background(), TestCodeSessionUUID)
	if err != nil {
		t.Fatal(err)
	}
	return tunnel
}

func WaitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met before deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func LocalPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func StartTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	_, client := startRedis(t)
	return client
}

func StartTestNATS(t *testing.T) *nats.Conn {
	t.Helper()
	return startNATS(t)
}
