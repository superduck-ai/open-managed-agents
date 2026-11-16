package porttunnel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const socketCloseWriteTimeout = time.Second

type socketStream struct {
	conn         *websocket.Conn
	protocol     string
	writeTimeout time.Duration
	writeMu      sync.Mutex
	closeOnce    sync.Once
}

func newSocketStream(conn *websocket.Conn, protocol string, writeTimeout time.Duration) *socketStream {
	return &socketStream{conn: conn, protocol: protocol, writeTimeout: writeTimeout}
}

func (s *socketStream) Protocol() string {
	return s.protocol
}

func (s *socketStream) Receive(ctx context.Context) (WebSocketMessage, error) {
	stop := context.AfterFunc(ctx, func() { _ = s.conn.Close() })
	defer stop()
	kind, data, err := s.conn.ReadMessage()
	if err != nil {
		if ctx.Err() != nil {
			return WebSocketMessage{}, ctx.Err()
		}
		var closeErr *websocket.CloseError
		if errors.As(err, &closeErr) {
			return WebSocketMessage{}, &CloseError{Code: closeErr.Code, Reason: closeErr.Text}
		}
		return WebSocketMessage{}, fmt.Errorf("%w: %w", ErrTunnelDisconnected, err)
	}
	return WebSocketMessage{Data: data, Binary: kind == websocket.BinaryMessage}, nil
}

func (s *socketStream) Send(_ context.Context, message WebSocketMessage) error {
	kind := websocket.TextMessage
	if message.Binary {
		kind = websocket.BinaryMessage
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout)); err != nil {
		return fmt.Errorf("%w: %w", ErrTunnelDisconnected, err)
	}
	if err := s.conn.WriteMessage(kind, message.Data); err != nil {
		return fmt.Errorf("%w: %w", ErrTunnelDisconnected, err)
	}
	return nil
}

func (s *socketStream) Close(code int, reason string) {
	s.closeOnce.Do(func() {
		code, reason = NormalizeClose(code, reason)
		s.writeMu.Lock()
		_ = s.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(socketCloseWriteTimeout))
		s.writeMu.Unlock()
		_ = s.conn.Close()
	})
}

func relayWebSockets(left, right WebSocketStream) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pipeWebSocket(ctx, right, left)
	}()
	pipeWebSocket(ctx, left, right)
	cancel()
	<-done
}

func pipeWebSocket(ctx context.Context, from, to WebSocketStream) {
	for {
		message, err := from.Receive(ctx)
		if err != nil {
			to.Close(relayCloseStatus(err))
			return
		}
		if err := to.Send(ctx, message); err != nil {
			from.Close(closeGoingAway, "session port peer disconnected")
			return
		}
	}
}

func relayCloseStatus(err error) (int, string) {
	var closeErr *CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code, closeErr.Reason
	}
	if errors.Is(err, ErrStalled) {
		return websocket.ClosePolicyViolation, "caller stopped reading"
	}
	return closeGoingAway, "session port tunnel disconnected"
}
