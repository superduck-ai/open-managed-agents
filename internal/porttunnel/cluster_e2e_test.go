package porttunnel_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/portgateway"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

const (
	clusterBigBytes         = 8 << 20
	clusterLargeHeaderBytes = 780 << 10
)

type clusterInstance struct {
	name     string
	public   *porttunnel.TunnelServer
	forward  *httptest.Server
	owner    string
	router   *porttunnel.ClusterRouter
	sandbox  *e2eSandboxes
	settings *sessionports.Settings
	hits     atomic.Int64
}

type clusterApp struct {
	server    *httptest.Server
	release   chan struct{}
	cancelled chan struct{}
	closes    chan int
}

type cluster struct {
	a, b  *clusterInstance
	redis *redis.Client
	app   *clusterApp
	host  string
}

func startCluster(t *testing.T, limits porttunnel.Limits) *cluster {
	t.Helper()
	redisClient := porttunnel.StartTestRedis(t)
	connection := porttunnel.StartTestNATS(t)
	codec, err := sessionports.NewSlugCodec(bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ports := config.SessionPortsConfig{
		DomainSuffix: "preview.localhost", URLScheme: "http", WakeTimeout: 2 * time.Second,
		ResponseIdleTimeout: 5 * time.Second, MaxRequestBodyBytes: 1 << 20, StripCookieNames: []string{"sessionKey"},
	}
	settings := sessionports.NewWithCodec(ports, 0, codec)
	app := startClusterApp(t)
	c := &cluster{redis: redisClient, app: app}
	c.a = startClusterInstance(t, "a", settings, ports, redisClient, connection, limits)
	c.b = startClusterInstance(t, "b", settings, ports, redisClient, connection, limits)
	address, err := settings.URL(e2eSessionUUID, porttunnel.LocalPort(t, app.server))
	if err != nil {
		t.Fatal(err)
	}
	c.host = strings.TrimSuffix(strings.TrimPrefix(address, "http://"), "/")
	return c
}

func startClusterInstance(t *testing.T, name string, settings *sessionports.Settings, ports config.SessionPortsConfig, redisClient *redis.Client, connection *nats.Conn, limits porttunnel.Limits) *clusterInstance {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	instance := &clusterInstance{name: name, sandbox: &e2eSandboxes{}, settings: settings}
	instance.forward = httptest.NewUnstartedServer(http.NotFoundHandler())
	t.Cleanup(instance.forward.Close)
	instance.owner = instance.forward.Listener.Addr().String() + "#" + name
	owners := porttunnel.NewRedisOwnerStore(redisClient)
	signer := porttunnel.NewForwardSigner(settings.ForwardKey())
	var gateway *portgateway.Gateway
	instance.public = porttunnel.StartTunnelServer(t, porttunnel.TunnelServerOptions{
		Limits:   limits,
		Owner:    instance.owner,
		Owners:   owners,
		Notifier: porttunnel.NewNATSOnlineNotifier(connection),
		Fallback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gateway.Middleware(http.NotFoundHandler()).ServeHTTP(w, r)
		}),
	})
	forwardServer := porttunnel.NewForwardServer(instance.public.Registry, signer, ports.MaxRequestBodyBytes, ports.ResponseIdleTimeout, logger)
	instance.forward.Config = forwardServer.HTTPServer()
	forwardHandler := instance.forward.Config.Handler
	instance.forward.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		instance.hits.Add(1)
		forwardHandler.ServeHTTP(w, r)
	})
	instance.forward.Start()
	instance.router = porttunnel.NewClusterRouter(instance.public.Registry, owners, porttunnel.NewForwardClient(signer), logger)
	gateway = portgateway.New(portgateway.Deps{
		Settings: settings, Config: ports, SandboxTimeout: 30 * time.Second,
		Store: &e2eStore{}, Router: instance.router, Sandboxes: instance.sandbox, Logger: logger,
	})
	return instance
}

func startClusterApp(t *testing.T) *clusterApp {
	t.Helper()
	app := &clusterApp{release: make(chan struct{}), cancelled: make(chan struct{}, 1), closes: make(chan int, 4)}
	upgrader := websocket.Upgrader{Subprotocols: []string{"echo.v1"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Cookie", r.Header.Get("Cookie"))
		w.Header().Set("X-Seen-Forwarded-Host", r.Header.Get("X-Forwarded-Host"))
		w.Header().Set("Connection", "X-Hop")
		w.Header().Set("X-Hop", "drop-me")
		http.SetCookie(w, &http.Cookie{Name: "app", Value: "1", Path: "/"})
		w.WriteHeader(http.StatusCreated)
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte(r.Method+":"), body...))
	})
	mux.HandleFunc("/large-headers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Large-Bytes", strconv.Itoa(len(r.Header.Get("X-Large"))))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("0123456789abcdef"), 4096)
		for written := 0; written < clusterBigBytes; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-app.release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "data: second\n\n")
	})
	mux.HandleFunc("/hold", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "held\n")
		w.(http.Flusher).Flush()
		select {
		case <-app.release:
		case <-r.Context().Done():
			app.cancelled <- struct{}{}
		}
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		for {
			kind, data, err := socket.ReadMessage()
			if err != nil {
				var closeErr *websocket.CloseError
				if errors.As(err, &closeErr) {
					app.closes <- closeErr.Code
				}
				return
			}
			if err := socket.WriteMessage(kind, append([]byte("echo:"), data...)); err != nil {
				return
			}
		}
	})
	app.server = httptest.NewServer(mux)
	t.Cleanup(app.server.Close)
	return app
}

func (c *cluster) request(t *testing.T, ctx context.Context, instance *clusterInstance, method, path string, body io.Reader) (*http.Response, error) {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, method, instance.public.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = c.host
	request.Header.Set("Cookie", "sessionKey=secret; theme=dark")
	return http.DefaultClient.Do(request)
}

func (c *cluster) connectOwner(t *testing.T, options porttunnel.FakeClientOptions) {
	t.Helper()
	c.b.public.ConnectWithOptions(t, options)
	porttunnel.WaitFor(t, func() bool { return c.owner(t) == c.b.owner })
}

func (c *cluster) dialer(instance *clusterInstance) *websocket.Dialer {
	address := strings.TrimPrefix(instance.public.URL, "http://")
	return &websocket.Dialer{
		Subprotocols:     []string{"echo.v1"},
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
}

func (c *cluster) owner(t *testing.T) string {
	t.Helper()
	owner, err := c.redis.Get(context.Background(), "oma:session_port:owner:"+porttunnel.TestCodeSessionUUID).Result()
	if errors.Is(err, redis.Nil) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func assertNoInternalAddress(t *testing.T, c *cluster, response *http.Response, body []byte) {
	t.Helper()
	for _, instance := range []*clusterInstance{c.a, c.b} {
		address := strings.TrimPrefix(instance.forward.URL, "http://")
		if bytes.Contains(body, []byte(address)) {
			t.Fatalf("response body leaks internal address %s: %s", address, body)
		}
		for name, values := range response.Header {
			for _, value := range values {
				if strings.Contains(value, address) {
					t.Fatalf("response header %s leaks internal address %s", name, address)
				}
			}
		}
	}
}

func TestClusterOwnerGoneWhenOwnerUnreachable(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	c.b.forward.Close()
	started := time.Now()
	response, err := c.request(t, context.Background(), c.a, http.MethodGet, "/headers", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("X-OMA-Port-Error") != "waking" {
		t.Fatalf("status=%d error=%q body=%s", response.StatusCode, response.Header.Get("X-OMA-Port-Error"), body)
	}
	assertNoInternalAddress(t, c, response, body)
	if elapsed := time.Since(started); elapsed > 4*time.Second {
		t.Fatalf("owner failure took %s", elapsed)
	}
	if owner := c.owner(t); owner != "" {
		t.Fatalf("stale owner kept: %q", owner)
	}
}

func TestClusterOwnerWithoutTunnelWaitsForReconnect(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	if err := c.redis.Set(context.Background(), "oma:session_port:owner:"+porttunnel.TestCodeSessionUUID, c.b.owner, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		client, _, err := porttunnel.DialFakeClientWithOptions(c.a.public.URL, porttunnel.TestToken, porttunnel.TestCodeSessionID, porttunnel.FakeClientOptions{})
		if err == nil {
			t.Cleanup(client.Close)
		}
	}()
	response, err := c.request(t, context.Background(), c.a, http.MethodGet, "/headers", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || string(body) != "GET:" {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if owner := c.owner(t); owner != c.a.owner {
		t.Fatalf("owner after reconnect = %q", owner)
	}
}

func TestClusterQuotaIsCountedOnOwner(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{MaxConcurrentRequests: 1})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	held, err := c.request(t, context.Background(), c.a, http.MethodGet, "/hold", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Body.Close()
	if line, err := bufio.NewReader(held.Body).ReadString('\n'); err != nil || line != "held\n" {
		t.Fatalf("held line=%q err=%v", line, err)
	}
	response, err := c.request(t, context.Background(), c.a, http.MethodGet, "/headers", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	close(c.app.release)
}

func TestClusterForwardsHTTPWithGatewayHeaderRules(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	response, err := c.request(t, context.Background(), c.a, http.MethodPost, "/headers", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusCreated || string(body) != "POST:payload" {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if c.b.hits.Load() != 1 || c.a.hits.Load() != 0 {
		t.Fatalf("forward hits a=%d b=%d", c.a.hits.Load(), c.b.hits.Load())
	}
	if cookie := response.Header.Get("X-Seen-Cookie"); cookie != "theme=dark" {
		t.Fatalf("cookie seen by app = %q", cookie)
	}
	if host := response.Header.Get("X-Seen-Forwarded-Host"); host != c.host {
		t.Fatalf("forwarded host = %q", host)
	}
	if response.Header.Get("X-Hop") != "" || response.Header.Get("Referrer-Policy") == "" {
		t.Fatalf("response headers not processed: %v", response.Header)
	}
	porttunnel.WaitFor(t, func() bool { return c.a.sandbox.calls.Load() > 0 })
	if c.b.sandbox.calls.Load() != 0 {
		t.Fatalf("owner renewed sandbox %d times", c.b.sandbox.calls.Load())
	}
}

func TestClusterStreamsLargeDownloadToSlowReader(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{FlowControl: true})
	response, err := c.request(t, context.Background(), c.a, http.MethodGet, "/big", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	time.Sleep(500 * time.Millisecond)
	digest := sha256.New()
	read, err := io.Copy(digest, response.Body)
	if err != nil || read != clusterBigBytes {
		t.Fatalf("read=%d err=%v", read, err)
	}
	expected := sha256.New()
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	for written := 0; written < clusterBigBytes; written += len(chunk) {
		expected.Write(chunk)
	}
	if !bytes.Equal(digest.Sum(nil), expected.Sum(nil)) {
		t.Fatal("download corrupted")
	}
}

func TestClusterStreamsServerSentEventsWithoutBuffering(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	response, err := c.request(t, context.Background(), c.a, http.MethodGet, "/sse", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "data: first\n" {
		t.Fatalf("first=%q err=%v", first, err)
	}
	close(c.app.release)
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "\ndata: second\n\n" {
		t.Fatalf("rest=%q err=%v", rest, err)
	}
}

func TestClusterPropagatesCallerCancellationToOwner(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	response, err := c.request(t, ctx, c.a, http.MethodGet, "/hold", nil)
	if err != nil {
		t.Fatal(err)
	}
	if line, err := bufio.NewReader(response.Body).ReadString('\n'); err != nil || line != "held\n" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	cancel()
	response.Body.Close()
	select {
	case <-c.app.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not reach the sandbox service")
	}
}

func TestClusterForwardsWebSocket(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	header := http.Header{"Cookie": []string{"sessionKey=secret"}}
	socket, response, err := c.dialer(c.a).Dial("ws://"+c.host+"/ws", header)
	if err != nil {
		t.Fatalf("dial: %v %v", err, response)
	}
	defer socket.Close()
	if c.b.hits.Load() != 1 {
		t.Fatalf("websocket was not forwarded to the owner: hits=%d", c.b.hits.Load())
	}
	if socket.Subprotocol() != "echo.v1" {
		t.Fatalf("subprotocol = %q", socket.Subprotocol())
	}
	for _, message := range []struct {
		kind int
		data string
	}{{websocket.TextMessage, "hello"}, {websocket.BinaryMessage, "\x00\x01"}} {
		if err := socket.WriteMessage(message.kind, []byte(message.data)); err != nil {
			t.Fatal(err)
		}
		kind, data, err := socket.ReadMessage()
		if err != nil || kind != message.kind || string(data) != "echo:"+message.data {
			t.Fatalf("kind=%d data=%q err=%v", kind, data, err)
		}
	}
	if err := socket.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4001, "bye"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-c.app.closes:
		if code != 4001 {
			t.Fatalf("close code = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close code did not reach the sandbox service")
	}
}

func TestClusterForwardsLargeRequestHeaders(t *testing.T) {
	c := startCluster(t, porttunnel.Limits{})
	c.connectOwner(t, porttunnel.FakeClientOptions{})
	large := strings.Repeat("h", clusterLargeHeaderBytes)
	request, err := http.NewRequest(http.MethodGet, c.a.public.URL+"/large-headers", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = c.host
	request.Header.Set("X-Large", large)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || response.Header.Get("X-Seen-Large-Bytes") != strconv.Itoa(clusterLargeHeaderBytes) {
		t.Fatalf("status=%d seen=%q body=%s", response.StatusCode, response.Header.Get("X-Seen-Large-Bytes"), body)
	}
	socket, handshake, err := c.dialer(c.a).Dial("ws://"+c.host+"/ws", http.Header{"X-Large": []string{large}})
	if err != nil {
		status := 0
		if handshake != nil {
			status = handshake.StatusCode
		}
		t.Fatalf("websocket dial: %v status=%d", err, status)
	}
	defer socket.Close()
	if err := socket.WriteMessage(websocket.TextMessage, []byte("large")); err != nil {
		t.Fatal(err)
	}
	if _, data, err := socket.ReadMessage(); err != nil || string(data) != "echo:large" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if c.b.hits.Load() != 2 {
		t.Fatalf("forward hits = %d", c.b.hits.Load())
	}
}
