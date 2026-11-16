package porttunnel

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func endlessApp(t *testing.T) *httptest.Server {
	t.Helper()
	chunk := bytes.Repeat([]byte("e"), 32*1024)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for r.Context().Err() == nil {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(app.Close)
	return app
}

func quickApp(t *testing.T) *httptest.Server {
	t.Helper()
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "quick")
	}))
	t.Cleanup(app.Close)
	return app
}

func roundTripBody(t *testing.T, tunnel Tunnel, port int) (string, time.Duration) {
	t.Helper()
	started := time.Now()
	response, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: port, Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body []byte
	for {
		data, err := response.Body.Next(context.Background(), 5*time.Second)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, data...)
	}
	return string(body), time.Since(started)
}

func TestFlowControlViolationCancelsOnlyThatStream(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{})
	client := server.ConnectWithOptions(t, FakeClientOptions{FlowControl: true, IgnoreCredits: true})
	tunnel := server.Tunnel(t)
	response, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, endlessApp(t)), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.Cancels:
	case <-time.After(5 * time.Second):
		t.Fatal("over-sending stream was not cancelled")
	}
	var bodyErr error
	for bodyErr == nil {
		_, bodyErr = response.Body.Next(context.Background(), time.Second)
	}
	if !errors.Is(bodyErr, ErrFlowControlViolation) {
		t.Fatalf("body error = %v", bodyErr)
	}
	if body, _ := roundTripBody(t, tunnel, LocalPort(t, quickApp(t))); body != "quick" {
		t.Fatalf("follow-up body = %q", body)
	}
	select {
	case <-client.Done():
		t.Fatal("violation closed the tunnel")
	default:
	}
}

func TestFlowControlIsNegotiatedOnlyWhenBothSidesAgree(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{})
	legacy := server.Connect(t)
	if legacy.FlowNegotiated() || server.Tunnel(t).FlowControl() {
		t.Fatal("legacy client negotiated flow control")
	}
	legacy.Close()
	modern := server.ConnectWithOptions(t, FakeClientOptions{FlowControl: true})
	if !modern.FlowNegotiated() {
		t.Fatal("flow control was not negotiated")
	}
	WaitFor(t, func() bool {
		tunnel, err := server.Registry.Lookup(context.Background(), TestCodeSessionUUID)
		return err == nil && tunnel.FlowControl()
	})
}

func TestFlowControlUnreadStreamDoesNotDelayOthers(t *testing.T) {
	server := StartTunnelServer(t, TunnelServerOptions{StallLimit: time.Second})
	client := server.ConnectWithOptions(t, FakeClientOptions{FlowControl: true})
	tunnel := server.Tunnel(t)
	stalled, err := tunnel.RoundTrip(context.Background(), HTTPRequest{Port: LocalPort(t, endlessApp(t)), Method: http.MethodGet, Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer stalled.Body.Close()
	time.Sleep(500 * time.Millisecond)
	if sent := client.Chunks.Load(); sent > FlowControlWindow/(32*1024)+2 {
		t.Fatalf("client sent %d chunks for an unread stream, window allows %d", sent, FlowControlWindow/(32*1024)+1)
	}
	quick := LocalPort(t, quickApp(t))
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		body, elapsed := roundTripBody(t, tunnel, quick)
		if body != "quick" || elapsed > time.Second {
			t.Fatalf("other request = %q in %s", body, elapsed)
		}
		time.Sleep(100 * time.Millisecond)
	}
	select {
	case id := <-client.Cancels:
		t.Fatalf("platform cancelled unread stream %s", id)
	default:
	}
}

func TestFlowControlWindowUpdatesFollowConsumption(t *testing.T) {
	const total = 8 << 20
	chunk := bytes.Repeat([]byte("w"), 32*1024)
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for written := 0; written < total; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	t.Cleanup(app.Close)
	server := StartTunnelServer(t, TunnelServerOptions{})
	client := server.ConnectWithOptions(t, FakeClientOptions{FlowControl: true})
	body, _ := roundTripBody(t, server.Tunnel(t), LocalPort(t, app))
	if len(body) != total {
		t.Fatalf("received %d bytes", len(body))
	}
	want := int64(total/(FlowControlWindow/2)) - 2
	if granted := client.Granted.Load(); granted < want {
		t.Fatalf("window updates = %d, want at least %d", granted, want)
	}
}

func TestFlowControlWebSocketSendWaitsForCredit(t *testing.T) {
	upgrader := websocket.Upgrader{}
	app := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		socket, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer socket.Close()
		for {
			if _, _, err := socket.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(app.Close)
	server := StartTunnelServer(t, TunnelServerOptions{})
	client := server.ConnectWithOptions(t, FakeClientOptions{FlowControl: true})
	stream, err := server.Tunnel(t).OpenWebSocket(context.Background(), WebSocketOpen{Port: LocalPort(t, app), Path: "/"})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close(1000, "")
	client.HoldLocal.Store(true)
	half := WebSocketMessage{Data: bytes.Repeat([]byte("h"), FlowControlWindow/2), Binary: true}
	for range 2 {
		if err := stream.Send(context.Background(), half); err != nil {
			t.Fatal(err)
		}
	}
	waiting, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := stream.Send(waiting, half); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("send beyond window = %v", err)
	}
	client.HoldLocal.Store(false)
	released, cancelReleased := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelReleased()
	if err := stream.Send(released, half); err != nil {
		t.Fatalf("send after window update = %v", err)
	}
	if client.GrantsSent.Load() == 0 {
		t.Fatal("client sent no window update")
	}
}

func TestOutboundFramesWaitForSpaceWithoutDropping(t *testing.T) {
	frames := newOutboundFrames(10)
	closed := make(chan struct{})
	if err := frames.push(context.Background(), closed, []byte("123456")); err != nil {
		t.Fatal(err)
	}
	full, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := frames.push(full, closed, []byte("789012")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("push into full queue = %v", err)
	}
	pushed := make(chan error, 1)
	go func() { pushed <- frames.push(context.Background(), closed, []byte("abcdef")) }()
	time.Sleep(20 * time.Millisecond)
	if frame, ok := frames.pop(); !ok || string(frame) != "123456" {
		t.Fatalf("pop = %q %v", frame, ok)
	}
	if err := <-pushed; err != nil {
		t.Fatal(err)
	}
	if frame, ok := frames.pop(); !ok || string(frame) != "abcdef" {
		t.Fatalf("second pop = %q %v", frame, ok)
	}
	oversized := newOutboundFrames(4)
	if err := oversized.push(context.Background(), closed, []byte("larger than limit")); err != nil {
		t.Fatalf("oversized frame on empty queue = %v", err)
	}
}

func TestFlowControlZeroLengthFloodIsViolation(t *testing.T) {
	conn := &Conn{flowControl: true, closed: make(chan struct{})}
	stream := newQueue[streamEvent]()
	allowed := FlowControlWindow / FlowControlFrameOverhead
	for index := range allowed {
		if err := deliver(conn, stream, streamEvent{}, 0, frameCost(0), mergeHTTPChunk); err != nil {
			t.Fatalf("empty frame %d rejected: %v", index, err)
		}
	}
	if err := deliver(conn, stream, streamEvent{}, 0, frameCost(0), mergeHTTPChunk); !errors.Is(err, ErrFlowControlViolation) {
		t.Fatalf("empty frame beyond the window = %v", err)
	}
}

func TestMergeHTTPChunksPreservesOrderAndAccounting(t *testing.T) {
	conn := &Conn{flowControl: true, closed: make(chan struct{})}
	stream := newQueue[streamEvent]()
	push := func(event streamEvent, cost int) {
		t.Helper()
		if err := deliver(conn, stream, event, len(event.data), cost, mergeHTTPChunk); err != nil {
			t.Fatal(err)
		}
	}
	push(streamEvent{headers: &httpHeadersMsg{StatusCode: 200}}, 0)
	total := 0
	for _, part := range []string{"ab", "cd", "ef"} {
		push(streamEvent{data: []byte(part)}, frameCost(len(part)))
		total += frameCost(len(part))
	}
	large := bytes.Repeat([]byte("x"), 20<<10)
	push(streamEvent{data: large}, frameCost(len(large)))
	push(streamEvent{data: large}, frameCost(len(large)))
	push(streamEvent{final: true}, frameCost(0))
	total += 2*frameCost(len(large)) + frameCost(0)

	var events []streamEvent
	for {
		event, ok := stream.take(conn)
		if !ok {
			break
		}
		events = append(events, event)
	}
	if len(events) != 4 || events[0].headers == nil || !events[3].final {
		t.Fatalf("events = %d", len(events))
	}
	if len(events[1].data) != 6+len(large) || string(events[1].data[:6]) != "abcdef" {
		t.Fatalf("merged chunk = %d bytes, prefix %q", len(events[1].data), events[1].data[:6])
	}
	if !bytes.Equal(events[1].data[6:], large) || !bytes.Equal(events[2].data, large) {
		t.Fatal("merge crossed the 32 KiB limit or reordered data")
	}
	if stream.consumed != int64(total) || stream.received != int64(total) {
		t.Fatalf("accounting received=%d consumed=%d want %d", stream.received, stream.consumed, total)
	}
}

func TestOutboundControlFramesBypassByteLimit(t *testing.T) {
	frames := newOutboundFrames(10)
	closed := make(chan struct{})
	if err := frames.push(context.Background(), closed, []byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	frames.pushControl(closed, []byte("window_update"))
	frames.pushControl(closed, []byte("http_cancel"))
	for _, want := range []string{"0123456789", "window_update", "http_cancel"} {
		if frame, ok := frames.pop(); !ok || string(frame) != want {
			t.Fatalf("pop = %q %v, want %q", frame, ok, want)
		}
	}
	close(closed)
	frames.pushControl(closed, []byte("after close"))
	if frame, ok := frames.pop(); ok {
		t.Fatalf("control frame queued after close: %q", frame)
	}
}
