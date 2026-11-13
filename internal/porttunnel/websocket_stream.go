package porttunnel

import (
	"context"
	"time"
	"unicode/utf8"
)

const websocketOpenTimeout = 30 * time.Second

type WebSocketOpen struct {
	Port      int
	Path      string
	Headers   []Header
	Protocols []string
}

type WebSocketMessage struct {
	Data   []byte
	Binary bool
}

type WebSocketStream interface {
	Protocol() string
	Receive(ctx context.Context) (WebSocketMessage, error)
	Send(ctx context.Context, message WebSocketMessage) error
	Close(code int, reason string)
}

type tunnelWebSocket struct {
	conn     *Conn
	stream   *wsStream
	protocol string
}

func (c *Conn) OpenWebSocket(ctx context.Context, open WebSocketOpen) (WebSocketStream, error) {
	if !tryAcquire(c.websockets) {
		return nil, ErrTooManyWebSockets
	}
	stream := newQueue[wsEvent]()
	c.registerWS(stream)
	err := c.enqueue(ctx, tunnelRequest{WSOpen: &wsOpenMsg{
		TunnelID:  stream.id,
		Port:      int32(open.Port),
		Path:      open.Path,
		Headers:   open.Headers,
		Protocols: open.Protocols,
	}})
	if err != nil {
		c.finishWS(stream)
		return nil, err
	}
	event, err := stream.next(ctx, c, websocketOpenTimeout)
	if err == nil {
		err = wsEventError(event)
	}
	if err == nil && !event.opened {
		err = errProtocol
	}
	if err != nil {
		c.enqueueControl(tunnelRequest{WSClose: &wsCloseMsg{TunnelID: stream.id, Code: 1001}})
		c.finishWS(stream)
		return nil, err
	}
	return &tunnelWebSocket{conn: c, stream: stream, protocol: event.protocol}, nil
}

func wsEventError(event wsEvent) error {
	if event.err != nil {
		return event.err
	}
	if event.closed != nil {
		return event.closed
	}
	return nil
}

func (w *tunnelWebSocket) Protocol() string {
	return w.protocol
}

func (w *tunnelWebSocket) Receive(ctx context.Context) (WebSocketMessage, error) {
	for {
		event, err := w.stream.next(ctx, w.conn, 0)
		if err == nil {
			err = wsEventError(event)
		}
		if err != nil {
			w.conn.finishWS(w.stream)
			return WebSocketMessage{}, err
		}
		if !event.opened {
			return WebSocketMessage{Data: event.data, Binary: event.binary}, nil
		}
	}
}

func (w *tunnelWebSocket) Send(ctx context.Context, message WebSocketMessage) error {
	if w.conn.flowControl {
		if err := w.stream.spendCredit(ctx, w.conn, frameCost(len(message.Data))); err != nil {
			return err
		}
	}
	return w.conn.enqueue(ctx, tunnelRequest{WSMessage: &wsMessageMsg{TunnelID: w.stream.id, Data: message.Data, IsBinary: message.Binary}})
}

func (w *tunnelWebSocket) Close(code int, reason string) {
	code, reason = NormalizeClose(code, reason)
	if code == closeNoStatus {
		code = closeGoingAway
	}
	if !w.stream.isDone() {
		w.conn.enqueueControl(tunnelRequest{WSClose: &wsCloseMsg{TunnelID: w.stream.id, Code: int32(code), Reason: reason}})
	}
	w.conn.finishWS(w.stream)
}

const (
	closeNoStatus      = 1005
	closeGoingAway     = 1001
	maxCloseReasonSize = 123
)

func NormalizeClose(code int, reason string) (int, string) {
	if code == closeNoStatus {
		return closeNoStatus, ""
	}
	if !sendableCloseCode(code) {
		code = closeGoingAway
	}
	for len(reason) > maxCloseReasonSize {
		_, size := utf8.DecodeLastRuneInString(reason)
		reason = reason[:len(reason)-size]
	}
	return code, reason
}

func sendableCloseCode(code int) bool {
	switch {
	case code >= 1000 && code <= 1003, code >= 1007 && code <= 1014, code >= 3000 && code <= 4999:
		return true
	default:
		return false
	}
}
