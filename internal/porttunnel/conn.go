package porttunnel

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	defaultReadIdleTimeout = 90 * time.Second
	StallTimeout           = 5 * time.Second
	streamBufferBytes      = 4 << 20
	eventOverheadBytes     = 256
	writeTimeout           = 10 * time.Second
	maxInboundFrameBytes   = 64 << 20
)

type Identity struct {
	OrganizationUUID      string
	WorkspaceUUID         string
	CodeSessionUUID       string
	CodeSessionExternalID string
}

type Limits struct {
	KeepaliveInterval     time.Duration
	MaxConcurrentRequests int
	MaxWebSockets         int
}

type Conn struct {
	identity        Identity
	socket          *websocket.Conn
	limits          Limits
	logger          *slog.Logger
	readIdleTimeout time.Duration
	stallLimit      time.Duration
	flowControl     bool
	outbound        *outboundFrames
	requests        chan struct{}
	websockets      chan struct{}
	mu              sync.Mutex
	httpStreams     map[string]*httpStream
	wsStreams       map[string]*wsStream
	closed          chan struct{}
	closeOnce       sync.Once
	closeErr        error
}

func newConn(identity Identity, socket *websocket.Conn, limits Limits, logger *slog.Logger) *Conn {
	return &Conn{
		identity:    identity,
		socket:      socket,
		limits:      limits,
		logger:      logger,
		outbound:    newOutboundFrames(outboundQueueBytes),
		requests:    make(chan struct{}, limits.MaxConcurrentRequests),
		websockets:  make(chan struct{}, limits.MaxWebSockets),
		httpStreams: make(map[string]*httpStream),
		wsStreams:   make(map[string]*wsStream),
		closed:      make(chan struct{}),

		readIdleTimeout: defaultReadIdleTimeout,
		stallLimit:      StallTimeout,
	}
}

func (c *Conn) Done() <-chan struct{} {
	return c.closed
}

func (c *Conn) Err() error {
	select {
	case <-c.closed:
		return c.closeErr
	default:
		return nil
	}
}

func (c *Conn) run() error {
	c.socket.SetReadLimit(maxInboundFrameBytes)
	c.socket.SetPingHandler(c.handlePing)
	c.socket.SetPongHandler(func(string) error { return c.extendReadDeadline() })
	go c.writeLoop()
	err := c.readLoop()
	c.Close(err)
	return err
}

func (c *Conn) handlePing(data string) error {
	if err := c.extendReadDeadline(); err != nil {
		return err
	}
	err := c.socket.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(writeTimeout))
	var netErr net.Error
	if errors.Is(err, websocket.ErrCloseSent) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return nil
	}
	return err
}

func (c *Conn) extendReadDeadline() error {
	return c.socket.SetReadDeadline(time.Now().Add(c.readIdleTimeout))
}

func (c *Conn) readLoop() error {
	for {
		if err := c.extendReadDeadline(); err != nil {
			return err
		}
		kind, data, err := c.socket.ReadMessage()
		if err != nil {
			return err
		}
		if kind != websocket.TextMessage && kind != websocket.BinaryMessage {
			continue
		}
		response, err := decodeResponse(data)
		if err != nil {
			c.logger.Warn("decode session port tunnel frame", "code_session_id", c.identity.CodeSessionExternalID, "error", err)
			continue
		}
		c.dispatch(response)
	}
}

func (c *Conn) writeLoop() {
	keepalive := time.NewTicker(c.limits.KeepaliveInterval)
	defer keepalive.Stop()
	ping, err := encodeRequest(tunnelRequest{SessionID: c.identity.CodeSessionExternalID})
	if err != nil {
		c.Close(err)
		return
	}
	for {
		select {
		case <-c.closed:
			return
		case <-c.outbound.ready:
			for frame, ok := c.outbound.pop(); ok; frame, ok = c.outbound.pop() {
				if !c.writeFrame(frame) {
					return
				}
			}
		case <-keepalive.C:
			if !c.writeFrame(ping) {
				return
			}
		}
	}
}

func (c *Conn) writeFrame(frame []byte) bool {
	if err := c.socket.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		c.Close(err)
		return false
	}
	if err := c.socket.WriteMessage(websocket.TextMessage, frame); err != nil {
		c.Close(err)
		return false
	}
	return true
}

func (c *Conn) dispatch(response tunnelResponse) {
	switch {
	case response.HTTPHeaders != nil:
		c.deliverHTTP(response.HTTPHeaders.RequestID, streamEvent{headers: response.HTTPHeaders}, 0)
	case response.HTTPChunk != nil:
		c.deliverHTTP(response.HTTPChunk.RequestID, streamEvent{data: response.HTTPChunk.Data, final: response.HTTPChunk.IsFinal}, frameCost(len(response.HTTPChunk.Data)))
	case response.HTTPError != nil:
		c.deliverHTTP(response.HTTPError.RequestID, streamEvent{err: &UpstreamError{Message: response.HTTPError.ErrorMessage}}, 0)
	case response.WSOpened != nil:
		c.deliverWS(response.WSOpened.TunnelID, wsEvent{opened: true, protocol: response.WSOpened.Protocol}, 0)
	case response.WSMessage != nil:
		c.deliverWS(response.WSMessage.TunnelID, wsEvent{data: response.WSMessage.Data, binary: response.WSMessage.IsBinary}, frameCost(len(response.WSMessage.Data)))
	case response.WSClose != nil:
		c.deliverWS(response.WSClose.TunnelID, wsEvent{closed: &CloseError{Code: int(response.WSClose.Code), Reason: response.WSClose.Reason}}, 0)
	case response.WSError != nil:
		c.deliverWS(response.WSError.TunnelID, wsEvent{err: &UpstreamError{Message: response.WSError.ErrorMessage}}, 0)
	case response.WindowUpdate != nil:
		c.grantSendCredit(response.WindowUpdate)
	}
}

func (c *Conn) deliverHTTP(requestID string, event streamEvent, cost int) {
	c.mu.Lock()
	stream := c.httpStreams[requestID]
	c.mu.Unlock()
	if stream == nil {
		return
	}
	if err := deliver(c, stream, event, len(event.data), cost, mergeHTTPChunk); err != nil {
		c.abortHTTP(stream, err)
	}
}

func (c *Conn) deliverWS(tunnelID string, event wsEvent, cost int) {
	c.mu.Lock()
	stream := c.wsStreams[tunnelID]
	c.mu.Unlock()
	if stream == nil {
		return
	}
	if err := deliver(c, stream, event, len(event.data), cost, nil); err != nil {
		c.abortWS(stream, err)
	}
}

func deliver[T any](c *Conn, q *queue[T], event T, payloadBytes, cost int, merge func(*T, T) bool) error {
	if q.isDone() {
		return nil
	}
	if c.flowControl {
		if !q.pushWithinWindow(event, cost, merge) {
			return ErrFlowControlViolation
		}
		return nil
	}
	if !deliverBlocking(c, q, event, payloadBytes) {
		return ErrStalled
	}
	return nil
}

func deliverBlocking[T any](c *Conn, q *queue[T], event T, payloadBytes int) bool {
	size := payloadBytes + eventOverheadBytes
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		if q.isDone() || q.tryPush(event, size, streamBufferBytes) {
			return true
		}
		if timer == nil {
			timer = time.NewTimer(c.stallLimit)
		} else {
			timer.Reset(c.stallLimit)
		}
		select {
		case <-q.progress:
		case <-timer.C:
			return false
		case <-q.done:
			return true
		case <-c.closed:
			return true
		}
	}
}

func (c *Conn) enqueue(ctx context.Context, request tunnelRequest) error {
	request.SessionID = c.identity.CodeSessionExternalID
	frame, err := encodeRequest(request)
	if err != nil {
		return err
	}
	return c.outbound.push(ctx, c.closed, frame)
}

func (c *Conn) enqueueControl(request tunnelRequest) {
	request.SessionID = c.identity.CodeSessionExternalID
	frame, err := encodeRequest(request)
	if err != nil {
		c.logger.Warn("encode session port tunnel control frame", "code_session_id", c.identity.CodeSessionExternalID, "error", err)
		return
	}
	c.outbound.pushControl(c.closed, frame)
}

func (c *Conn) Close(err error) {
	c.closeOnce.Do(func() {
		if err == nil {
			err = ErrTunnelDisconnected
		}
		c.closeErr = err
		close(c.closed)
		code := websocket.CloseNormalClosure
		if errors.Is(err, errShutdown) {
			code = websocket.CloseServiceRestart
		}
		_ = c.socket.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""), time.Now().Add(time.Second))
		_ = c.socket.Close()
	})
}

func tryAcquire(semaphore chan struct{}) bool {
	select {
	case semaphore <- struct{}{}:
		return true
	default:
		return false
	}
}

func newStreamID() string {
	return uuid.NewString()
}
