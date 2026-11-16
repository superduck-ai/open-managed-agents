package porttunnel

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandlerRejectsInvalidCredentials(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{})
	for name, credentials := range map[string][2]string{
		"missing token":      {"", TestCodeSessionID},
		"missing session":    {TestToken, ""},
		"wrong token":        {"bad", TestCodeSessionID},
		"mismatched session": {TestToken, "cse_other"},
	} {
		t.Run(name, func(t *testing.T) {
			_, response, err := DialFakeClient(server.URL, credentials[0], credentials[1])
			if err == nil {
				t.Fatal("upgrade accepted")
			}
			if response == nil || response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("response = %+v", response)
			}
		})
	}
}

func TestConnClosesWhenCredentialIsRevoked(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{Revalidate: 20 * time.Millisecond})
	client := server.Connect(t)
	server.Auth.Revoked.Store(true)
	select {
	case <-client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("revoked tunnel stayed open")
	}
	if _, err := server.Registry.Lookup(context.Background(), TestCodeSessionUUID); !errors.Is(err, ErrNoTunnel) {
		t.Fatalf("lookup after revoke = %v", err)
	}
}

func TestConnClosesAfterReadIdleTimeout(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{ReadIdleTimeout: 150 * time.Millisecond})
	client := server.Connect(t)
	select {
	case <-client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("silent tunnel stayed open")
	}
}

func TestConnPingsKeepIdleTunnelAlive(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{ReadIdleTimeout: 200 * time.Millisecond})
	client := server.Connect(t)
	for range 10 {
		if err := client.Ping(); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case <-client.Done():
		t.Fatal("tunnel closed despite pings")
	default:
	}
}

func TestConnSendsEmptyPayloadKeepalives(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{Limits: Limits{KeepaliveInterval: 20 * time.Millisecond}})
	client := server.Connect(t)
	WaitFor(t, func() bool { return client.Keepalives.Load() >= 3 })
	frame := <-client.RawFrames
	if frame != `{"sessionId":"`+TestCodeSessionID+`"}` {
		t.Fatalf("keepalive frame = %s", frame)
	}
}

func TestNewConnectionReplacesOldAndFailsInFlightRequests(t *testing.T) {
	release := make(chan struct{})
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(app.Close)
	t.Cleanup(func() { close(release) })
	server := StartTunnelServer(t, TunnelServerOptions{})
	first := server.Connect(t)
	oldTunnel := server.Tunnel(t)
	result := make(chan error, 1)
	go func() {
		_, err := oldTunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"})
		result <- err
	}()
	time.Sleep(50 * time.Millisecond)
	second, _, err := DialFakeClient(server.URL, TestToken, TestCodeSessionID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.Close)
	select {
	case err := <-result:
		if !errors.Is(err, ErrTunnelDisconnected) {
			t.Fatalf("in-flight request error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not fail")
	}
	<-first.Done()
	WaitFor(t, func() bool {
		current, err := server.Registry.Lookup(context.Background(), TestCodeSessionUUID)
		return err == nil && current != oldTunnel
	})
}

func TestConnEnforcesConcurrencyLimits(t *testing.T) {
	release := make(chan struct{})
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(app.Close)
	t.Cleanup(func() { close(release) })
	server := StartTunnelServer(t, TunnelServerOptions{Limits: Limits{MaxConcurrentRequests: 1, MaxWebSockets: 1}})
	server.Connect(t)
	tunnel := server.Tunnel(t)
	response, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"}); !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("second request error = %v", err)
	}
	response.Body.Close()
	conn := tunnel.(*Conn)
	conn.websockets <- struct{}{}
	if _, err := tunnel.OpenWebSocket(context.Background(), WebSocketOpen{Port: 1, Path: "/"}); !errors.Is(err, ErrTooManyWebSockets) {
		t.Fatalf("websocket error = %v", err)
	}
}

func TestConnMapsUpstreamFailures(t *testing.T) {
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(app.Close)
	server := StartTunnelServer(t, TunnelServerOptions{})
	server.Connect(t)
	tunnel := server.Tunnel(t)
	_, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"})
	if !IsHeaderTimeout(err) {
		t.Fatalf("slow upstream error = %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	port := LocalPort(t, closed)
	closed.Close()
	_, err = tunnel.RoundTrip(context.Background(), HTTPRequest{Port: port, Method: http.MethodGet, Path: "/"})
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || IsHeaderTimeout(err) || !strings.Contains(upstream.Message, "request failed") {
		t.Fatalf("refused upstream error = %v", err)
	}
}

func TestConnCancelsStalledStream(t *testing.T) {
	payload := strings.Repeat("x", 32*1024)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 512 {
			if _, err := io.WriteString(w, payload); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(app.Close)
	server := StartTunnelServer(t, TunnelServerOptions{StallLimit: 100 * time.Millisecond})
	client := server.Connect(t)
	tunnel := server.Tunnel(t)
	response, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.Cancels:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled stream was not cancelled")
	}
	var bodyErr error
	for bodyErr == nil {
		_, bodyErr = response.Body.Next(context.Background(), time.Second)
	}
	if !errors.Is(bodyErr, ErrStalled) {
		t.Fatalf("body error = %v", bodyErr)
	}
	response.Body.Close()
	quick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "quick")
	}))
	t.Cleanup(quick.Close)
	started := time.Now()
	followUp, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, quick), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer followUp.Body.Close()
	data, err := followUp.Body.Next(context.Background(), 5*time.Second)
	if err != nil || string(data) != "quick" || time.Since(started) > 2*time.Second {
		t.Fatalf("follow-up after cancelled stream = %q, %v in %s", data, err, time.Since(started))
	}
	select {
	case <-client.Done():
		t.Fatal("stall closed the whole tunnel")
	default:
	}
}

func TestConnStreamsSlowCallerWithoutUnboundedBuffering(t *testing.T) {
	const chunks = 2048
	payload := strings.Repeat("y", 32*1024)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range chunks {
			if _, err := io.WriteString(w, payload); err != nil {
				return
			}
		}
	}))
	t.Cleanup(app.Close)
	server := StartTunnelServer(t, TunnelServerOptions{StallLimit: 5 * time.Second})
	client := server.Connect(t)
	response, err := server.Tunnel(t).RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	time.Sleep(500 * time.Millisecond)
	if sent := client.Chunks.Load(); sent >= chunks/2 {
		t.Fatalf("client sent %d of %d chunks while caller was paused", sent, chunks)
	}
	total := 0
	for {
		data, err := response.Body.Next(context.Background(), 5*time.Second)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Trim(string(data), "y") != "" {
			t.Fatal("corrupted body chunk")
		}
		total += len(data)
		if total%(256*1024) == 0 {
			time.Sleep(time.Millisecond)
		}
	}
	if total != chunks*len(payload) {
		t.Fatalf("received %d bytes, want %d", total, chunks*len(payload))
	}
}

func TestResponseBodyIdleTimeoutCancelsRequest(t *testing.T) {
	release := make(chan struct{})
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(app.Close)
	t.Cleanup(func() { close(release) })
	server := StartTunnelServer(t, TunnelServerOptions{})
	client := server.Connect(t)
	response, err := server.Tunnel(t).RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, app), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := response.Body.Next(context.Background(), 50*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("idle body error = %v", err)
	}
	select {
	case <-client.Cancels:
	case <-time.After(5 * time.Second):
		t.Fatal("idle request was not cancelled")
	}
}
