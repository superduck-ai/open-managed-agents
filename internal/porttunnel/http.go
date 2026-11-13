package porttunnel

import (
	"context"
	"errors"
	"io"
	"time"
)

const responseHeaderTimeout = 35 * time.Second

type HTTPRequest struct {
	Port    int
	Method  string
	Path    string
	Headers []Header
	Body    []byte
}

type HTTPResponse struct {
	StatusCode int
	Headers    []Header
	Body       ResponseBody
}

type ResponseBody interface {
	Next(ctx context.Context, idle time.Duration) ([]byte, error)
	Close()
}

type httpBody struct {
	conn     *Conn
	stream   *httpStream
	finished bool
}

func (c *Conn) RoundTrip(ctx context.Context, request HTTPRequest) (*HTTPResponse, error) {
	if !tryAcquire(c.requests) {
		return nil, ErrTooManyRequests
	}
	stream := newQueue[streamEvent]()
	c.registerHTTP(stream)
	err := c.enqueue(ctx, tunnelRequest{HTTPRequest: &httpRequestMsg{
		RequestID: stream.id,
		Port:      int32(request.Port),
		Method:    request.Method,
		Path:      request.Path,
		Headers:   request.Headers,
		Body:      request.Body,
	}})
	if err != nil {
		c.finishHTTP(stream)
		return nil, err
	}
	event, err := stream.next(ctx, c, responseHeaderTimeout)
	if err == nil && event.err != nil {
		err = event.err
	}
	if err == nil && event.headers == nil {
		err = errProtocol
	}
	if err != nil {
		c.cancelHTTP(stream, err)
		return nil, err
	}
	return &HTTPResponse{
		StatusCode: int(event.headers.StatusCode),
		Headers:    event.headers.Headers,
		Body:       &httpBody{conn: c, stream: stream},
	}, nil
}

func (c *Conn) cancelHTTP(stream *httpStream, cause error) {
	var upstream *UpstreamError
	if !errors.As(cause, &upstream) && !errors.Is(cause, ErrTunnelDisconnected) && !errors.Is(cause, ErrStalled) {
		c.enqueueControl(tunnelRequest{HTTPCancel: &httpCancelMsg{RequestID: stream.id}})
	}
	c.finishHTTP(stream)
}

func (b *httpBody) Next(ctx context.Context, idle time.Duration) ([]byte, error) {
	if b.finished {
		return nil, io.EOF
	}
	event, err := b.stream.next(ctx, b.conn, idle)
	if err == nil {
		err = event.err
	}
	if err != nil {
		b.finished = true
		b.conn.cancelHTTP(b.stream, err)
		return nil, err
	}
	if event.final {
		b.finished = true
		b.conn.finishHTTP(b.stream)
		if len(event.data) == 0 {
			return nil, io.EOF
		}
	}
	return event.data, nil
}

func (b *httpBody) Close() {
	if b.finished {
		return
	}
	b.finished = true
	b.conn.cancelHTTP(b.stream, context.Canceled)
}
