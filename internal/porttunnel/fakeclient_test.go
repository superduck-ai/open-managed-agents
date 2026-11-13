package porttunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type fakeHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type fakeTunnelRequest struct {
	SessionID   string `json:"sessionId"`
	HTTPRequest *struct {
		RequestID string       `json:"requestId"`
		Port      int          `json:"port"`
		Method    string       `json:"method"`
		Path      string       `json:"path"`
		Headers   []fakeHeader `json:"headers"`
		Body      []byte       `json:"body"`
	} `json:"httpRequest"`
	HTTPCancel *struct {
		RequestID string `json:"requestId"`
	} `json:"httpCancel"`
	WSOpen *struct {
		TunnelID  string       `json:"tunnelId"`
		Port      int          `json:"port"`
		Path      string       `json:"path"`
		Headers   []fakeHeader `json:"headers"`
		Protocols []string     `json:"protocols"`
	} `json:"wsOpen"`
	WSMessage *struct {
		TunnelID string `json:"tunnelId"`
		Data     []byte `json:"data"`
		IsBinary bool   `json:"isBinary"`
	} `json:"wsMessage"`
	WSClose *struct {
		TunnelID string `json:"tunnelId"`
		Code     int    `json:"code"`
		Reason   string `json:"reason"`
	} `json:"wsClose"`
	WindowUpdate *struct {
		StreamID string `json:"streamId"`
		Bytes    int64  `json:"bytes"`
	} `json:"windowUpdate"`
}

type FakeClientOptions struct {
	FlowControl   bool
	IgnoreCredits bool
	MaxChunkBytes int
}

type fakeCredit struct {
	mu        sync.Mutex
	available int64
	closed    bool
	changed   chan struct{}
}

type fakeLocalSocket struct {
	conn  *websocket.Conn
	queue chan fakeLocalMessage
}

type fakeLocalMessage struct {
	kind  int
	data  []byte
	close *[2]any
}

type FakeClient struct {
	options    FakeClientOptions
	flow       bool
	credits    map[string]*fakeCredit
	local      map[string]*fakeLocalSocket
	HoldLocal  atomic.Bool
	Granted    atomic.Int64
	GrantsSent atomic.Int64
	socket     *websocket.Conn
	writeMu    sync.Mutex
	mu         sync.Mutex
	requests   map[string]context.CancelFunc
	sockets    map[string]*websocket.Conn
	Keepalives atomic.Int64
	CloseCode  atomic.Int64
	Chunks     atomic.Int64
	Cancels    chan string
	RawFrames  chan string
	done       chan struct{}
}

func DialFakeClient(serverURL, token, sessionID string) (*FakeClient, *http.Response, error) {
	return DialFakeClientWithOptions(serverURL, token, sessionID, FakeClientOptions{})
}

func DialFakeClientWithOptions(serverURL, token, sessionID string, options FakeClientOptions) (*FakeClient, *http.Response, error) {
	header := http.Header{}
	if options.FlowControl {
		header.Set(CapabilitiesHeader, FlowControlCapability)
	}
	header.Set("Authorization", "Bearer "+token)
	header.Set("X-Session-ID", sessionID)
	url := "ws" + strings.TrimPrefix(serverURL, "http") + Path
	socket, response, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		return nil, response, err
	}
	client := &FakeClient{
		options:   options,
		flow:      options.FlowControl && response.Header.Get(CapabilitiesHeader) == FlowControlCapability,
		credits:   make(map[string]*fakeCredit),
		local:     make(map[string]*fakeLocalSocket),
		socket:    socket,
		requests:  make(map[string]context.CancelFunc),
		sockets:   make(map[string]*websocket.Conn),
		Cancels:   make(chan string, 16),
		RawFrames: make(chan string, 64),
		done:      make(chan struct{}),
	}
	go client.run()
	return client, response, nil
}

func (c *FakeClient) Done() <-chan struct{} {
	return c.done
}

func (c *FakeClient) Close() {
	_ = c.socket.Close()
	<-c.done
}

func (c *FakeClient) Ping() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.socket.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second))
}

func (c *FakeClient) send(frame map[string]any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.socket.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return c.socket.WriteMessage(websocket.TextMessage, data)
}

func (c *FakeClient) run() {
	defer close(c.done)
	for {
		_, data, err := c.socket.ReadMessage()
		if err != nil {
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				c.CloseCode.Store(int64(closeErr.Code))
			}
			c.mu.Lock()
			for _, cancel := range c.requests {
				cancel()
			}
			for _, local := range c.sockets {
				_ = local.Close()
			}
			c.mu.Unlock()
			return
		}
		select {
		case c.RawFrames <- string(data):
		default:
		}
		var request fakeTunnelRequest
		if err := json.Unmarshal(data, &request); err != nil {
			continue
		}
		c.dispatch(request)
	}
}

func (c *FakeClient) dispatch(request fakeTunnelRequest) {
	switch {
	case request.HTTPRequest != nil:
		ctx, cancel := context.WithCancel(context.Background())
		c.mu.Lock()
		c.requests[request.HTTPRequest.RequestID] = cancel
		c.mu.Unlock()
		go c.handleHTTP(ctx, request)
	case request.HTTPCancel != nil:
		c.adjustCredit(request.HTTPCancel.RequestID, 0, true)
		c.Cancels <- request.HTTPCancel.RequestID
		c.mu.Lock()
		if cancel := c.requests[request.HTTPCancel.RequestID]; cancel != nil {
			cancel()
		}
		c.mu.Unlock()
	case request.WSOpen != nil:
		go c.handleWSOpen(request)
	case request.WSMessage != nil:
		c.mu.Lock()
		local := c.sockets[request.WSMessage.TunnelID]
		queued := c.local[request.WSMessage.TunnelID]
		c.mu.Unlock()
		kind := websocket.TextMessage
		if request.WSMessage.IsBinary {
			kind = websocket.BinaryMessage
		}
		switch {
		case queued != nil:
			queued.queue <- fakeLocalMessage{kind: kind, data: request.WSMessage.Data}
		case local != nil:
			_ = local.WriteMessage(kind, request.WSMessage.Data)
		}
	case request.WSClose != nil:
		c.mu.Lock()
		local := c.sockets[request.WSClose.TunnelID]
		queued := c.local[request.WSClose.TunnelID]
		c.mu.Unlock()
		c.adjustCredit(request.WSClose.TunnelID, 0, true)
		switch {
		case queued != nil:
			queued.queue <- fakeLocalMessage{close: &[2]any{request.WSClose.Code, request.WSClose.Reason}}
		case local != nil:
			_ = local.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(request.WSClose.Code, request.WSClose.Reason), time.Now().Add(time.Second))
		}
	case request.WindowUpdate != nil:
		c.Granted.Add(1)
		c.adjustCredit(request.WindowUpdate.StreamID, request.WindowUpdate.Bytes, false)
	default:
		c.Keepalives.Add(1)
	}
}

func (c *FakeClient) handleHTTP(ctx context.Context, request fakeTunnelRequest) {
	message := request.HTTPRequest
	defer func() {
		c.mu.Lock()
		delete(c.requests, message.RequestID)
		c.mu.Unlock()
	}()
	fail := func(text string) {
		_ = c.send(map[string]any{"httpError": map[string]any{"requestId": message.RequestID, "errorMessage": text}})
	}
	outbound, err := http.NewRequestWithContext(ctx, message.Method, "http://localhost:"+strconv.Itoa(message.Port)+message.Path, bytes.NewReader(message.Body))
	if err != nil {
		fail("failed to create request: " + err.Error())
		return
	}
	for _, header := range message.Headers {
		if !strings.EqualFold(header.Name, "Host") {
			outbound.Header.Add(header.Name, header.Value)
		}
	}
	transport := &http.Transport{DisableCompression: true, ResponseHeaderTimeout: 2 * time.Second}
	defer transport.CloseIdleConnections()
	response, err := transport.RoundTrip(outbound)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fail("request canceled")
			return
		}
		if strings.Contains(err.Error(), "timeout awaiting response headers") {
			fail("request failed: response header timeout")
			return
		}
		fail("request failed: " + err.Error())
		return
	}
	defer response.Body.Close()
	headers := make([]map[string]string, 0, len(response.Header))
	for name, values := range response.Header {
		for _, value := range values {
			headers = append(headers, map[string]string{"name": strings.ToLower(name), "value": value})
		}
	}
	if err := c.send(map[string]any{"httpHeaders": map[string]any{
		"requestId": message.RequestID, "statusCode": response.StatusCode, "statusText": http.StatusText(response.StatusCode), "headers": headers,
	}}); err != nil {
		return
	}
	c.streamBody(ctx, message.RequestID, response.Body, fail)
}

func (c *FakeClient) streamBody(ctx context.Context, requestID string, body io.Reader, fail func(string)) {
	buffer := make([]byte, 32*1024)
	if c.options.MaxChunkBytes > 0 {
		buffer = buffer[:c.options.MaxChunkBytes]
	}
	sequence := 1
	for {
		read, err := body.Read(buffer)
		if read > 0 {
			if spendErr := c.spend(ctx, requestID, frameCost(read)); spendErr != nil {
				return
			}
			if sendErr := c.send(map[string]any{"httpChunk": map[string]any{
				"requestId": requestID, "data": buffer[:read], "sequenceNumber": sequence,
			}}); sendErr != nil {
				return
			}
			c.Chunks.Add(1)
			sequence++
		}
		if errors.Is(err, io.EOF) {
			if spendErr := c.spend(ctx, requestID, frameCost(0)); spendErr != nil {
				return
			}
			_ = c.send(map[string]any{"httpChunk": map[string]any{"requestId": requestID, "isFinal": true, "sequenceNumber": sequence}})
			return
		}
		if err != nil {
			fail(err.Error())
			return
		}
	}
}

func (c *FakeClient) handleWSOpen(request fakeTunnelRequest) {
	open := request.WSOpen
	header := http.Header{}
	for _, item := range open.Headers {
		header.Add(item.Name, item.Value)
	}
	dialer := websocket.Dialer{Subprotocols: open.Protocols, HandshakeTimeout: 5 * time.Second}
	local, response, err := dialer.Dial("ws://localhost:"+strconv.Itoa(open.Port)+open.Path, header)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		_ = c.send(map[string]any{"wsError": map[string]any{"tunnelId": open.TunnelID, "errorMessage": err.Error()}})
		return
	}
	c.mu.Lock()
	c.sockets[open.TunnelID] = local
	c.mu.Unlock()
	if c.flow {
		c.startLocalWriter(open.TunnelID, local)
	}
	_ = c.send(map[string]any{"wsOpened": map[string]any{"tunnelId": open.TunnelID, "protocol": local.Subprotocol()}})
	for {
		kind, data, err := local.ReadMessage()
		if err != nil {
			code, reason := websocket.CloseAbnormalClosure, err.Error()
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				code, reason = closeErr.Code, closeErr.Text
			}
			_ = c.send(map[string]any{"wsClose": map[string]any{"tunnelId": open.TunnelID, "code": code, "reason": reason}})
			return
		}
		if err := c.spend(context.Background(), open.TunnelID, frameCost(len(data))); err != nil {
			return
		}
		_ = c.send(map[string]any{"wsMessage": map[string]any{"tunnelId": open.TunnelID, "data": data, "isBinary": kind == websocket.BinaryMessage}})
	}
}

func (c *FakeClient) FlowNegotiated() bool {
	return c.flow
}

func (c *FakeClient) credit(streamID string) *fakeCredit {
	c.mu.Lock()
	defer c.mu.Unlock()
	credit := c.credits[streamID]
	if credit == nil {
		credit = &fakeCredit{available: FlowControlWindow, changed: make(chan struct{}, 1)}
		c.credits[streamID] = credit
	}
	return credit
}

func (c *FakeClient) spend(ctx context.Context, streamID string, bytes int) error {
	if !c.flow || c.options.IgnoreCredits {
		return nil
	}
	credit := c.credit(streamID)
	for {
		credit.mu.Lock()
		if credit.closed {
			credit.mu.Unlock()
			return errors.New("stream closed")
		}
		if credit.available > 0 {
			credit.available -= int64(bytes)
			credit.mu.Unlock()
			return nil
		}
		credit.mu.Unlock()
		select {
		case <-credit.changed:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return errors.New("tunnel closed")
		}
	}
}

func (c *FakeClient) adjustCredit(streamID string, bytes int64, closeStream bool) {
	credit := c.credit(streamID)
	credit.mu.Lock()
	credit.available += bytes
	credit.closed = credit.closed || closeStream
	credit.mu.Unlock()
	select {
	case credit.changed <- struct{}{}:
	default:
	}
}

func (c *FakeClient) startLocalWriter(tunnelID string, local *websocket.Conn) *fakeLocalSocket {
	socket := &fakeLocalSocket{conn: local, queue: make(chan fakeLocalMessage, 4096)}
	c.mu.Lock()
	c.local[tunnelID] = socket
	c.mu.Unlock()
	go func() {
		var unacknowledged int64
		for message := range socket.queue {
			for c.HoldLocal.Load() {
				time.Sleep(5 * time.Millisecond)
			}
			if message.close != nil {
				_ = local.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(message.close[0].(int), message.close[1].(string)), time.Now().Add(time.Second))
				return
			}
			if err := local.WriteMessage(message.kind, message.data); err != nil {
				return
			}
			unacknowledged += int64(frameCost(len(message.data)))
			if unacknowledged >= FlowControlWindow/2 {
				if c.send(map[string]any{"windowUpdate": map[string]any{"streamId": tunnelID, "bytes": unacknowledged}}) == nil {
					c.GrantsSent.Add(1)
				}
				unacknowledged = 0
			}
		}
	}()
	return socket
}
