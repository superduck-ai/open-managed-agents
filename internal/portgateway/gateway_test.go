package portgateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
	"github.com/superduck-ai/open-managed-agents/internal/sessionports"
)

const testSessionUUID = "8c2b3f61-5a4e-4d2c-9f1a-7b6e5d4c3b2a"

type fakeStore struct {
	target  db.SessionPortTarget
	err     error
	touches atomic.Int64
}

func (s *fakeStore) FindSessionPortTarget(_ context.Context, sessionUUID string) (db.SessionPortTarget, error) {
	if s.err != nil {
		return db.SessionPortTarget{}, s.err
	}
	if sessionUUID != testSessionUUID {
		return db.SessionPortTarget{}, db.ErrNotFound
	}
	return s.target, nil
}

func (s *fakeStore) TouchCodeSessionPortAccess(context.Context, string, string, string, time.Time, time.Duration) error {
	s.touches.Add(1)
	return nil
}

type fakeSandboxes struct {
	calls   atomic.Int64
	err     error
	release chan struct{}
}

func (s *fakeSandboxes) SetTimeout(context.Context, string, time.Duration) error {
	s.calls.Add(1)
	if s.release != nil {
		<-s.release
	}
	return s.err
}

type fakeRouter struct {
	mu      sync.Mutex
	tunnel  porttunnel.Tunnel
	waits   atomic.Int64
	onWait  func(int64)
	online  chan struct{}
	waitErr error
}

func (r *fakeRouter) Lookup(context.Context, string) (porttunnel.Tunnel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tunnel == nil {
		return nil, porttunnel.ErrNoTunnel
	}
	return r.tunnel, nil
}

func (r *fakeRouter) WaitOnline(ctx context.Context, _ string) error {
	count := r.waits.Add(1)
	if r.onWait != nil {
		r.onWait(count)
	}
	if r.online == nil {
		<-ctx.Done()
		return ctx.Err()
	}
	select {
	case <-r.online:
		return r.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

type fakeBody struct {
	chunks [][]byte
	block  bool
	closed atomic.Bool
	idle   time.Duration
}

func (b *fakeBody) Next(ctx context.Context, idle time.Duration) ([]byte, error) {
	b.idle = idle
	if b.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if len(b.chunks) == 0 {
		return nil, io.EOF
	}
	chunk := b.chunks[0]
	b.chunks = b.chunks[1:]
	return chunk, nil
}

func (b *fakeBody) Close() {
	b.closed.Store(true)
}

type fakeTunnel struct {
	err      error
	response *porttunnel.HTTPResponse
	requests chan porttunnel.HTTPRequest
	newBody  func() porttunnel.ResponseBody
}

func (t *fakeTunnel) RoundTrip(_ context.Context, request porttunnel.HTTPRequest) (*porttunnel.HTTPResponse, error) {
	if t.requests != nil {
		t.requests <- request
	}
	if t.err != nil {
		return nil, t.err
	}
	if t.newBody != nil {
		return &porttunnel.HTTPResponse{StatusCode: http.StatusOK, Body: t.newBody()}, nil
	}
	return t.response, nil
}

func (t *fakeTunnel) FlowControl() bool {
	return false
}

func (t *fakeTunnel) OpenWebSocket(context.Context, porttunnel.WebSocketOpen) (porttunnel.WebSocketStream, error) {
	return nil, t.err
}

type harness struct {
	gateway   *Gateway
	store     *fakeStore
	sandboxes *fakeSandboxes
	router    *fakeRouter
	host      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	codec, err := sessionports.NewSlugCodec(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	ports := config.SessionPortsConfig{
		DomainSuffix: "preview.example.com", URLScheme: "https", WakeTimeout: 200 * time.Millisecond,
		ResponseIdleTimeout: time.Minute, MaxRequestBodyBytes: 16, StripCookieNames: []string{"sessionKey", "lastActiveOrg"},
	}
	settings := sessionports.NewWithCodec(ports, 3001, codec)
	h := &harness{
		store: &fakeStore{target: db.SessionPortTarget{
			SessionUUID: testSessionUUID, SessionExternalID: "sesn_1", OrganizationUUID: "org", WorkspaceUUID: "ws",
			CodeSessionUUID: "cs-uuid", CodeSessionExternalID: "cse_1", ProviderSandboxID: "sbx_1", SandboxState: sandboxRunning,
		}},
		sandboxes: &fakeSandboxes{},
		router:    &fakeRouter{},
	}
	h.gateway = New(Deps{
		Settings: settings, Config: ports, SandboxTimeout: 30 * time.Second, Store: h.store, Router: h.router,
		Sandboxes: h.sandboxes, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	address, err := settings.URL(testSessionUUID, 4000)
	if err != nil {
		t.Fatal(err)
	}
	h.host = strings.TrimSuffix(strings.TrimPrefix(address, "https://"), "/")
	return h
}

func (h *harness) serve(request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	h.gateway.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})).ServeHTTP(recorder, request)
	return recorder
}

func (h *harness) request(method, body string) *http.Request {
	request := httptest.NewRequest(method, "http://"+h.host+"/app?q=1", strings.NewReader(body))
	request.Host = h.host
	return request
}

func assertGatewayError(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if recorder.Code != status || recorder.Header().Get(errorHeader) != code {
		t.Fatalf("status = %d code = %q, want %d %q (body %q)", recorder.Code, recorder.Header().Get(errorHeader), status, code, recorder.Body.String())
	}
	if recorder.Header().Get(referrerPolicyHeader) != referrerPolicy {
		t.Fatal("gateway error missing Referrer-Policy")
	}
}

func TestGatewayReturnsNotFoundForUnknownAddresses(t *testing.T) {
	h := newHarness(t)
	for name, host := range map[string]string{
		"malformed": "nope.preview.example.com",
		"blocked":   strings.Replace(h.host, "4000-", "49983-", 1),
		"forged":    "4000-aaaaaaaaaaaaaaaaaaaaaaaaaa.preview.example.com",
	} {
		t.Run(name, func(t *testing.T) {
			request := h.request(http.MethodGet, "")
			request.Host = host
			assertGatewayError(t, h.serve(request), http.StatusNotFound, codeNotFound)
		})
	}
	h.store.target.Removed = true
	assertGatewayError(t, h.serve(h.request(http.MethodGet, "")), http.StatusNotFound, codeNotFound)
}

func TestGatewayReturnsConflictWhenSandboxIsGone(t *testing.T) {
	for name, mutate := range map[string]func(*harness){
		"never started": func(h *harness) { h.store.target.CodeSessionUUID = "" },
		"reclaimed":     func(h *harness) { h.store.target.SandboxState = "stopped" },
		"provider gone": func(h *harness) { h.sandboxes.err = sandboxruntime.ErrSandboxNotFound },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			mutate(h)
			assertGatewayError(t, h.serve(h.request(http.MethodGet, "")), http.StatusConflict, codeUnavailable)
		})
	}
}

func TestGatewayReturnsWakingAfterTimeout(t *testing.T) {
	h := newHarness(t)
	recorder := h.serve(h.request(http.MethodGet, ""))
	assertGatewayError(t, recorder, http.StatusServiceUnavailable, codeWaking)
	if recorder.Header().Get("Retry-After") != "5" || h.sandboxes.calls.Load() != 1 {
		t.Fatalf("retry-after = %q wakes = %d", recorder.Header().Get("Retry-After"), h.sandboxes.calls.Load())
	}
}

func TestGatewayMapsTunnelFailures(t *testing.T) {
	for name, testCase := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"too many":       {porttunnel.ErrTooManyRequests, http.StatusTooManyRequests, codeTooMany},
		"disconnected":   {porttunnel.ErrTunnelDisconnected, http.StatusBadGateway, codeDisconnected},
		"refused":        {&porttunnel.UpstreamError{Message: "request failed: connect to localhost:4000: refused"}, http.StatusBadGateway, codeDisconnected},
		"header timeout": {&porttunnel.UpstreamError{Message: "request failed: response header timeout"}, http.StatusGatewayTimeout, codeUpstreamTimeout},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.router.tunnel = &fakeTunnel{err: testCase.err}
			assertGatewayError(t, h.serve(h.request(http.MethodGet, "")), testCase.status, testCase.code)
		})
	}
}

func TestGatewayRejectsOversizedBodies(t *testing.T) {
	h := newHarness(t)
	h.router.tunnel = &fakeTunnel{err: errors.New("must not be called")}
	assertGatewayError(t, h.serve(h.request(http.MethodPost, strings.Repeat("x", 17))), http.StatusRequestEntityTooLarge, codeTooLarge)
	request := h.request(http.MethodPost, strings.Repeat("x", 17))
	request.ContentLength = -1
	assertGatewayError(t, h.serve(request), http.StatusRequestEntityTooLarge, codeTooLarge)
}

func TestGatewayPassesThroughOtherHosts(t *testing.T) {
	h := newHarness(t)
	request := h.request(http.MethodGet, "")
	request.Host = "oma.example.com"
	if recorder := h.serve(request); recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestGatewaySharesOneWakeAcrossConcurrentRequests(t *testing.T) {
	h := newHarness(t)
	h.gateway.cfg.WakeTimeout = 5 * time.Second
	h.sandboxes.release = make(chan struct{})
	h.router.online = make(chan struct{})
	const callers = 5
	var released sync.Once
	h.router.onWait = func(count int64) {
		if count == callers {
			released.Do(func() {
				close(h.sandboxes.release)
				h.router.mu.Lock()
				h.router.tunnel = &fakeTunnel{newBody: func() porttunnel.ResponseBody { return &fakeBody{chunks: [][]byte{[]byte("ok")}} }}
				h.router.mu.Unlock()
				close(h.router.online)
			})
		}
	}
	var wait sync.WaitGroup
	statuses := make(chan int, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			statuses <- h.serve(h.request(http.MethodGet, "")).Code
		}()
	}
	wait.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK {
			t.Fatalf("status = %d", status)
		}
	}
	if calls := h.sandboxes.calls.Load(); calls != 1 {
		t.Fatalf("SetTimeout calls = %d", calls)
	}
}

func TestGatewayForwardsRequestAndThrottlesKeepAlive(t *testing.T) {
	h := newHarness(t)
	requests := make(chan porttunnel.HTTPRequest, 2)
	tunnel := &fakeTunnel{requests: requests}
	h.router.tunnel = tunnel
	for range 2 {
		body := &fakeBody{chunks: [][]byte{[]byte("hello "), []byte("world")}}
		tunnel.response = &porttunnel.HTTPResponse{StatusCode: http.StatusCreated, Body: body, Headers: []porttunnel.Header{
			{Name: "content-type", Value: "text/plain"},
			{Name: "set-cookie", Value: "a=1; Domain=.example.com; Path=/; HttpOnly"},
			{Name: "connection", Value: "x-hop"},
			{Name: "x-hop", Value: "drop"},
			{Name: "referrer-policy", Value: "unsafe-url"},
		}}
		request := h.request(http.MethodPost, "data")
		request.Header.Set("Cookie", "sessionKey=s; keep=1; lastActiveOrg=o")
		request.Header.Set("Connection", "keep-alive, x-private")
		request.Header.Set("X-Private", "drop")
		request.Header.Set("X-Forwarded-For", "10.0.0.1")
		recorder := h.serve(request)
		if recorder.Code != http.StatusCreated || recorder.Body.String() != "hello world" {
			t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Set-Cookie"); got != "a=1; Path=/; HttpOnly" {
			t.Fatalf("set-cookie = %q", got)
		}
		if recorder.Header().Get("X-Hop") != "" || recorder.Header().Get(referrerPolicyHeader) != referrerPolicy {
			t.Fatalf("response headers = %v", recorder.Header())
		}
		if !body.closed.Load() || body.idle != time.Minute {
			t.Fatalf("body closed = %v idle = %s", body.closed.Load(), body.idle)
		}
	}
	forwarded := <-requests
	if forwarded.Port != 4000 || forwarded.Path != "/app?q=1" || string(forwarded.Body) != "data" || forwarded.Method != http.MethodPost {
		t.Fatalf("forwarded = %+v", forwarded)
	}
	headers := map[string]string{}
	for _, header := range forwarded.Headers {
		headers[header.Name] = header.Value
	}
	if headers["Cookie"] != "keep=1" || headers["X-Private"] != "" || headers["Connection"] != "" || headers["Host"] != "" {
		t.Fatalf("forwarded headers = %v", headers)
	}
	if headers["X-Forwarded-For"] != "10.0.0.1, 192.0.2.1" || headers["X-Forwarded-Proto"] != "http" || headers["X-Forwarded-Host"] != h.host {
		t.Fatalf("forwarding headers = %v", headers)
	}
	deadline := time.Now().Add(5 * time.Second)
	for (h.store.touches.Load() == 0 || h.sandboxes.calls.Load() == 0) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if h.store.touches.Load() != 1 || h.sandboxes.calls.Load() != 1 {
		t.Fatalf("touches = %d renewals = %d", h.store.touches.Load(), h.sandboxes.calls.Load())
	}
}

func TestGatewayCancelsTunnelWhenCallerDisconnects(t *testing.T) {
	h := newHarness(t)
	body := &fakeBody{block: true}
	h.router.tunnel = &fakeTunnel{response: &porttunnel.HTTPResponse{StatusCode: http.StatusOK, Body: body}}
	ctx, cancel := context.WithCancel(context.Background())
	request := h.request(http.MethodGet, "").WithContext(ctx)
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		h.serve(request)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if recovered := <-done; recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %v", recovered)
	}
	if !body.closed.Load() {
		t.Fatal("tunnel stream not cancelled")
	}
}
