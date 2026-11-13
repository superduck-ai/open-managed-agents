package porttunnel_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/portgateway"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

const e2eSessionUUID = "3d9a7a52-1f0c-4c7e-8a55-0c6f2b9d4e11"

type e2eStore struct {
	touches atomic.Int64
}

func (s *e2eStore) FindSessionPortTarget(_ context.Context, sessionUUID string) (db.SessionPortTarget, error) {
	if sessionUUID != e2eSessionUUID {
		return db.SessionPortTarget{}, db.ErrNotFound
	}
	return db.SessionPortTarget{
		SessionUUID: sessionUUID, SessionExternalID: "sesn_e2e", OrganizationUUID: "org", WorkspaceUUID: "workspace",
		CodeSessionUUID: porttunnel.TestCodeSessionUUID, CodeSessionExternalID: porttunnel.TestCodeSessionID,
		ProviderSandboxID: "sbx_e2e", SandboxState: "running",
	}, nil
}

func (s *e2eStore) TouchCodeSessionPortAccess(context.Context, string, string, string, time.Time, time.Duration) error {
	s.touches.Add(1)
	return nil
}

type e2eSandboxes struct {
	calls atomic.Int64
}

func (s *e2eSandboxes) SetTimeout(context.Context, string, time.Duration) error {
	s.calls.Add(1)
	return nil
}

type e2eEnvironment struct {
	server     *porttunnel.TunnelServer
	settings   *sessionports.Settings
	closeCodes chan int
	host       string
	store      *e2eStore
	sandboxes  *e2eSandboxes
	release    chan struct{}
}

type e2eOptions struct {
	limits           porttunnel.Limits
	writeTimeout     time.Duration
	flowWriteTimeout time.Duration
}

func startE2E(t *testing.T) *e2eEnvironment {
	t.Helper()
	return startE2EWithOptions(t, e2eOptions{})
}

func startE2EWithOptions(t *testing.T, options e2eOptions) *e2eEnvironment {
	t.Helper()
	release := make(chan struct{})
	closeCodes := make(chan int, 4)
	app := httptest.NewServer(e2eApp(release, closeCodes))
	t.Cleanup(app.Close)
	codec, err := sessionports.NewSlugCodec(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ports := config.SessionPortsConfig{
		DomainSuffix: "preview.localhost", URLScheme: "http", WakeTimeout: 2 * time.Second,
		ResponseIdleTimeout: 5 * time.Second, MaxRequestBodyBytes: 1 << 20, StripCookieNames: []string{"sessionKey"},
	}
	settings := sessionports.NewWithCodec(ports, 0, codec)
	environment := &e2eEnvironment{store: &e2eStore{}, sandboxes: &e2eSandboxes{}, release: release, settings: settings, closeCodes: closeCodes}
	var gateway *portgateway.Gateway
	environment.server = porttunnel.StartTunnelServer(t, porttunnel.TunnelServerOptions{
		Limits: options.limits,
		Fallback: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gateway.Middleware(http.NotFoundHandler()).ServeHTTP(w, r)
		}),
	})
	gateway = portgateway.New(portgateway.Deps{
		Settings: settings, Config: ports, SandboxTimeout: 30 * time.Second,
		Store: environment.store, Router: environment.server.Registry, Sandboxes: environment.sandboxes,
		Logger:                        slog.New(slog.NewTextHandler(io.Discard, nil)),
		CallerWriteTimeout:            options.writeTimeout,
		FlowControlCallerWriteTimeout: options.flowWriteTimeout,
	})
	address, err := settings.URL(e2eSessionUUID, porttunnel.LocalPort(t, app))
	if err != nil {
		t.Fatal(err)
	}
	environment.host = strings.TrimSuffix(strings.TrimPrefix(address, "http://"), "/")
	return environment
}

func e2eApp(release <-chan struct{}, closeCodes chan<- int) http.Handler {
	upgrader := websocket.Upgrader{Subprotocols: []string{"echo.v1"}}
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Seen-Cookie", r.Header.Get("Cookie"))
		w.Header().Set("X-Seen-Forwarded-Host", r.Header.Get("X-Forwarded-Host"))
		w.Header().Set("X-Seen-Host", r.Host)
		http.SetCookie(w, &http.Cookie{Name: "app", Value: "1", Domain: "preview.localhost", Path: "/"})
		_, _ = io.WriteString(w, "part-1\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "part-2\n")
	})
	chunk := bytes.Repeat([]byte("f"), 32*1024)
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/flood", func(w http.ResponseWriter, r *http.Request) {
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	})
	mux.HandleFunc("/ws-flood", func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		for {
			if err := socket.WriteMessage(websocket.BinaryMessage, chunk); err != nil {
				return
			}
		}
	})
	mux.HandleFunc("/ws-drop", func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_, _, _ = socket.ReadMessage()
		_ = socket.NetConn().Close()
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
					closeCodes <- closeErr.Code
				} else {
					closeCodes <- -1
				}
				return
			}
			if err := socket.WriteMessage(kind, append([]byte("echo:"), data...)); err != nil {
				return
			}
		}
	})
	return mux
}

func (e *e2eEnvironment) hostFor(t *testing.T, port int) string {
	t.Helper()
	address, err := e.settings.URL(e2eSessionUUID, port)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(strings.TrimPrefix(address, "http://"), "/")
}

func (e *e2eEnvironment) get(host, path string) (*http.Response, error) {
	request, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Host = host
	return http.DefaultClient.Do(request)
}

func (e *e2eEnvironment) stallCaller(t *testing.T, path string) net.Conn {
	t.Helper()
	dialer := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var controlErr error
		if err := raw.Control(func(fd uintptr) {
			controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_RCVBUF, 4096)
		}); err != nil {
			return err
		}
		return controlErr
	}}
	conn, err := dialer.Dial("tcp", strings.TrimPrefix(e.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := io.WriteString(conn, "GET "+path+" HTTP/1.1\r\nHost: "+e.host+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	return conn
}

func (e *e2eEnvironment) dialer() *websocket.Dialer {
	serverAddress := strings.TrimPrefix(e.server.URL, "http://")
	return &websocket.Dialer{
		Subprotocols:     []string{"echo.v1"},
		HandshakeTimeout: 5 * time.Second,
		NetDialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, serverAddress)
		},
	}
}

func TestGatewayEndToEndRejectsForgedAddress(t *testing.T) {
	environment := startE2E(t)
	environment.server.Connect(t)
	label, _, _ := strings.Cut(environment.host, ".")
	port, slug, _ := strings.Cut(label, "-")
	forged := []byte(slug)
	forged[0] = map[bool]byte{true: 'b', false: 'a'}[forged[0] == 'a']
	request, _ := http.NewRequest(http.MethodGet, environment.server.URL+"/stream", nil)
	request.Host = port + "-" + string(forged) + ".preview.localhost:" + strconv.Itoa(80)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound || response.Header.Get("X-OMA-Port-Error") != "not_found" {
		t.Fatalf("forged status = %d header = %q", response.StatusCode, response.Header.Get("X-OMA-Port-Error"))
	}
	portNumber, _ := strconv.Atoi(port)
	swapped, _ := http.NewRequest(http.MethodGet, environment.server.URL+"/stream", nil)
	swapped.Host = strconv.Itoa(portNumber+1) + "-" + slug + ".preview.localhost"
	swappedResponse, err := http.DefaultClient.Do(swapped)
	if err != nil {
		t.Fatal(err)
	}
	defer swappedResponse.Body.Close()
	if swappedResponse.StatusCode != http.StatusNotFound || swappedResponse.Header.Get("X-OMA-Port-Error") != "not_found" {
		t.Fatalf("port-swapped status = %d header = %q", swappedResponse.StatusCode, swappedResponse.Header.Get("X-OMA-Port-Error"))
	}
}

func TestGatewayEndToEndStreamsHTTPResponseThroughTunnel(t *testing.T) {
	environment := startE2E(t)
	environment.server.Connect(t)
	request, err := http.NewRequest(http.MethodGet, environment.server.URL+"/stream?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = environment.host
	request.Header.Set("Cookie", "sessionKey=secret; keep=yes")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("status = %d headers = %v", response.StatusCode, response.Header)
	}
	if got := response.Header.Get("X-Seen-Cookie"); got != "keep=yes" {
		t.Fatalf("forwarded cookie = %q", got)
	}
	if got := response.Header.Get("X-Seen-Forwarded-Host"); got != environment.host {
		t.Fatalf("forwarded host = %q", got)
	}
	if got := response.Header.Get("X-Seen-Host"); !strings.HasPrefix(got, "localhost:") {
		t.Fatalf("upstream host = %q", got)
	}
	if cookie := response.Header.Get("Set-Cookie"); strings.Contains(strings.ToLower(cookie), "domain") || !strings.HasPrefix(cookie, "app=1") {
		t.Fatalf("set-cookie = %q", cookie)
	}
	reader := bufio.NewReader(response.Body)
	first, err := reader.ReadString('\n')
	if err != nil || first != "part-1\n" {
		t.Fatalf("first chunk = %q, %v", first, err)
	}
	close(environment.release)
	rest, err := io.ReadAll(reader)
	if err != nil || string(rest) != "part-2\n" {
		t.Fatalf("rest = %q, %v", rest, err)
	}
	porttunnel.WaitFor(t, func() bool { return environment.store.touches.Load() > 0 })
	if environment.store.touches.Load() != 1 {
		t.Fatalf("port access touches = %d", environment.store.touches.Load())
	}
}

func TestGatewayEndToEndRelaysWebSocketThroughTunnel(t *testing.T) {
	environment := startE2E(t)
	environment.server.Connect(t)
	socket, response, err := environment.dialer().Dial("ws://"+environment.host+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v (%+v)", err, response)
	}
	defer socket.Close()
	if socket.Subprotocol() != "echo.v1" {
		t.Fatalf("subprotocol = %q", socket.Subprotocol())
	}
	for _, message := range []struct {
		kind int
		data []byte
	}{{websocket.TextMessage, []byte("hello")}, {websocket.BinaryMessage, []byte{0, 1, 2}}} {
		if err := socket.WriteMessage(message.kind, message.data); err != nil {
			t.Fatal(err)
		}
		_ = socket.SetReadDeadline(time.Now().Add(5 * time.Second))
		kind, data, err := socket.ReadMessage()
		if err != nil || kind != message.kind || !bytes.Equal(data, append([]byte("echo:"), message.data...)) {
			t.Fatalf("echo = %d %q %v", kind, data, err)
		}
	}
	if err := socket.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")); err != nil {
		t.Fatal(err)
	}
	_, _, err = socket.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("close = %v", err)
	}
}

func TestGatewayEndToEndWakesSandboxAndWaitsForTunnel(t *testing.T) {
	environment := startE2E(t)
	close(environment.release)
	result := make(chan *http.Response, 1)
	go func() {
		request, _ := http.NewRequest(http.MethodGet, environment.server.URL+"/stream", nil)
		request.Host = environment.host
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			result <- nil
			return
		}
		result <- response
	}()
	porttunnel.WaitFor(t, func() bool { return environment.sandboxes.calls.Load() == 1 })
	environment.server.Connect(t)
	response := <-result
	if response == nil {
		t.Fatal("woken request failed")
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "part-1\npart-2\n" {
		t.Fatalf("status = %d body = %q", response.StatusCode, body)
	}
}

func TestGatewayEndToEndReleasesSlotWhenCallerStopsReading(t *testing.T) {
	environment := startE2EWithOptions(t, e2eOptions{
		limits:       porttunnel.Limits{MaxConcurrentRequests: 1, MaxWebSockets: 1},
		writeTimeout: 200 * time.Millisecond,
	})
	environment.server.Connect(t)
	stalled, err := net.Dial("tcp", strings.TrimPrefix(environment.server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Close()
	if _, err := io.WriteString(stalled, "GET /flood HTTP/1.1\r\nHost: "+environment.host+"\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	porttunnel.WaitFor(t, func() bool {
		request, _ := http.NewRequest(http.MethodGet, environment.server.URL+"/ok", nil)
		request.Host = environment.host
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return false
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response.StatusCode == http.StatusOK && string(body) == "ok"
	})
	stalledSocket, response, err := environment.dialer().Dial("ws://"+environment.host+"/ws-flood", nil)
	if err != nil {
		t.Fatalf("dial: %v (%+v)", err, response)
	}
	defer stalledSocket.Close()
	porttunnel.WaitFor(t, func() bool {
		socket, response, err := environment.dialer().Dial("ws://"+environment.host+"/ws", nil)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err != nil {
			return false
		}
		_ = socket.Close()
		return true
	})
}

func TestGatewayEndToEndStalledCallerDoesNotBlockOtherPorts(t *testing.T) {
	environment := startE2E(t)
	client := environment.server.Connect(t)
	tunnel := environment.server.Tunnel(t)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "other")
	}))
	t.Cleanup(other.Close)
	otherHost := environment.hostFor(t, porttunnel.LocalPort(t, other))
	environment.stallCaller(t, "/flood")
	time.Sleep(500 * time.Millisecond)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		started := time.Now()
		response, err := environment.get(otherHost, "/")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		elapsed := time.Since(started)
		if response.StatusCode != http.StatusOK || string(body) != "other" {
			t.Fatalf("other port = %d %q after %s", response.StatusCode, body, elapsed)
		}
		if elapsed > 6*time.Second {
			t.Fatalf("other port took %s while a caller was stalled", elapsed)
		}
		time.Sleep(time.Second)
	}
	select {
	case <-client.Done():
		t.Fatal("stalled caller broke the tunnel")
	default:
	}
	current := environment.server.Tunnel(t)
	if current != tunnel {
		t.Fatal("tunnel reconnected while a caller was stalled")
	}
}

func TestGatewayEndToEndSlowReaderReceivesWholeResponse(t *testing.T) {
	environment := startE2E(t)
	environment.server.Connect(t)
	const total = 24 << 20
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("s"), 32*1024)
		for written := 0; written < total; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(big.Close)
	response, err := environment.get(environment.hostFor(t, porttunnel.LocalPort(t, big)), "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	buffer := make([]byte, 256*1024)
	received := 0
	for {
		read, err := io.ReadFull(response.Body, buffer)
		received += read
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Millisecond)
	}
	if received != total {
		t.Fatalf("slow reader received %d of %d bytes", received, total)
	}
}

func TestGatewayEndToEndAbnormalCallerCloseSendsValidCode(t *testing.T) {
	environment := startE2E(t)
	environment.server.Connect(t)
	socket, response, err := environment.dialer().Dial("ws://"+environment.host+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v (%+v)", err, response)
	}
	if err := socket.WriteMessage(websocket.TextMessage, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := socket.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	_ = socket.NetConn().Close()
	select {
	case code := <-environment.closeCodes:
		if code != websocket.CloseGoingAway {
			t.Fatalf("upstream close code = %d, want 1001", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upstream websocket was not closed")
	}
}

func TestGatewayEndToEndAbnormalUpstreamCloseSendsValidCode(t *testing.T) {
	environment := startE2E(t)
	environment.server.Connect(t)
	socket, response, err := environment.dialer().Dial("ws://"+environment.host+"/ws-drop", nil)
	if err != nil {
		t.Fatalf("dial: %v (%+v)", err, response)
	}
	defer socket.Close()
	if err := socket.WriteMessage(websocket.TextMessage, []byte("drop")); err != nil {
		t.Fatal(err)
	}
	_ = socket.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err = socket.ReadMessage()
	if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
		t.Fatalf("caller close = %v, want 1001", err)
	}
}

func timedGet(t *testing.T, environment *e2eEnvironment, host string) time.Duration {
	t.Helper()
	started := time.Now()
	response, err := environment.get(host, "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "other" {
		t.Fatalf("other port = %d %q", response.StatusCode, body)
	}
	return time.Since(started)
}

func otherPortHost(t *testing.T, environment *e2eEnvironment) string {
	t.Helper()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "other")
	}))
	t.Cleanup(other.Close)
	return environment.hostFor(t, porttunnel.LocalPort(t, other))
}

func TestGatewayFlowControlUnreadCallerOnlyHitsItsOwnWriteTimeout(t *testing.T) {
	environment := startE2EWithOptions(t, e2eOptions{flowWriteTimeout: 3 * time.Second})
	client := environment.server.ConnectWithOptions(t, porttunnel.FakeClientOptions{FlowControl: true})
	otherHost := otherPortHost(t, environment)
	started := time.Now()
	environment.stallCaller(t, "/flood")
	cancelled := make(chan time.Duration, 1)
	go func() {
		select {
		case <-client.Cancels:
			cancelled <- time.Since(started)
		case <-time.After(15 * time.Second):
		}
	}()
	for time.Since(started) < 2*time.Second {
		if elapsed := timedGet(t, environment, otherHost); elapsed > time.Second {
			t.Fatalf("other port took %s with flow control", elapsed)
		}
		time.Sleep(100 * time.Millisecond)
	}
	select {
	case at := <-cancelled:
		t.Fatalf("unread stream cancelled after %s, before the caller write timeout", at)
	default:
	}
	select {
	case at := <-cancelled:
		if at < 2500*time.Millisecond || at > 10*time.Second {
			t.Fatalf("unread stream cancelled after %s, want about the 3s write timeout", at)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("unread stream was never cancelled")
	}
	select {
	case <-client.Done():
		t.Fatal("unread caller broke the tunnel")
	default:
	}
}

func TestGatewayFlowControlSlowReaderDoesNotSlowOtherPorts(t *testing.T) {
	environment := startE2E(t)
	environment.server.ConnectWithOptions(t, porttunnel.FakeClientOptions{FlowControl: true})
	otherHost := otherPortHost(t, environment)
	baseline := timedGet(t, environment, otherHost)
	const total = 16 << 20
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("s"), 32*1024)
		for written := 0; written < total; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(big.Close)
	response, err := environment.get(environment.hostFor(t, porttunnel.LocalPort(t, big)), "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	done := make(chan int, 1)
	go func() {
		buffer := make([]byte, 64*1024)
		received := 0
		for {
			read, err := response.Body.Read(buffer)
			received += read
			if err != nil {
				done <- received
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	var slowest time.Duration
	for range 10 {
		slowest = max(slowest, timedGet(t, environment, otherHost))
		time.Sleep(150 * time.Millisecond)
	}
	if slowest > time.Second {
		t.Fatalf("other port slowest %s during slow download (baseline %s)", slowest, baseline)
	}
	select {
	case received := <-done:
		t.Fatalf("slow download finished early with %d bytes", received)
	default:
	}
	_ = response.Body.Close()
}

func TestGatewayFlowControlDeliversManyTinyChunksToSlowReader(t *testing.T) {
	for _, chunks := range []int{20000, 50000} {
		t.Run(strconv.Itoa(chunks), func(t *testing.T) {
			environment := startE2E(t)
			environment.server.ConnectWithOptions(t, porttunnel.FakeClientOptions{FlowControl: true, MaxChunkBytes: 8})
			var expected strings.Builder
			for index := range chunks {
				fmt.Fprintf(&expected, "%07d\n", index)
			}
			app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, expected.String())
			}))
			t.Cleanup(app.Close)
			response, err := environment.get(environment.hostFor(t, porttunnel.LocalPort(t, app)), "/")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			var received bytes.Buffer
			buffer := make([]byte, 2048)
			for {
				read, err := response.Body.Read(buffer)
				received.Write(buffer[:read])
				if err != nil {
					if !errors.Is(err, io.EOF) {
						t.Fatalf("after %d bytes: %v", received.Len(), err)
					}
					break
				}
				time.Sleep(200 * time.Microsecond)
			}
			if received.String() != expected.String() {
				t.Fatalf("received %d bytes, want %d", received.Len(), expected.Len())
			}
		})
	}
}
