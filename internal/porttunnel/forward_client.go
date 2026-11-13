package porttunnel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	ForwardDialTimeout           = 2 * time.Second
	ForwardResponseHeaderTimeout = responseHeaderTimeout + 10*time.Second
	forwardHandshakeTimeout      = websocketOpenTimeout + 10*time.Second
	forwardReadBufferBytes       = 32 << 10
)

type ForwardClient struct {
	signer *ForwardSigner
	http   *http.Client
	dialer *websocket.Dialer
}

func NewForwardClient(signer *ForwardSigner) *ForwardClient {
	netDialer := &net.Dialer{Timeout: ForwardDialTimeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           netDialer.DialContext,
		ResponseHeaderTimeout: ForwardResponseHeaderTimeout,
		MaxIdleConns:          256,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
	}
	return &ForwardClient{
		signer: signer,
		http: &http.Client{
			Transport:     transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		dialer: &websocket.Dialer{
			Proxy:            nil,
			NetDialContext:   netDialer.DialContext,
			HandshakeTimeout: forwardHandshakeTimeout,
		},
	}
}

type remoteTunnel struct {
	client          *ForwardClient
	owner           string
	address         string
	codeSessionUUID string
	release         func(owner string)
	logger          *slog.Logger
	flowControl     atomic.Bool
}

func (t *remoteTunnel) FlowControl() bool {
	return t.flowControl.Load()
}

func (t *remoteTunnel) RoundTrip(ctx context.Context, request HTTPRequest) (*HTTPResponse, error) {
	path := forwardHTTPPath(t.codeSessionUUID, request.Port)
	header, err := t.signedHeader(http.MethodPost, path, forwardRequest{Method: request.Method, Path: request.Path, Headers: request.Headers})
	if err != nil {
		return nil, err
	}
	forward, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+t.address+path, bytes.NewReader(request.Body))
	if err != nil {
		return nil, err
	}
	forward.Header = header
	forward.Header.Set("Content-Type", "application/octet-stream")
	response, err := t.client.http.Do(forward)
	if err != nil {
		return nil, t.transportError(ctx, err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		return nil, t.failure(response)
	}
	reader := bufio.NewReaderSize(response.Body, forwardReadBufferBytes)
	line, err := reader.ReadBytes('\n')
	var meta forwardMeta
	if err == nil {
		err = json.Unmarshal(line, &meta)
	}
	if err != nil {
		_ = response.Body.Close()
		return nil, t.transportError(ctx, err)
	}
	t.flowControl.Store(meta.FlowControl)
	return &HTTPResponse{
		StatusCode: meta.Status,
		Headers:    meta.Headers,
		Body:       &remoteBody{body: response.Body, reader: reader, buffer: make([]byte, forwardReadBufferBytes)},
	}, nil
}

func (t *remoteTunnel) OpenWebSocket(ctx context.Context, open WebSocketOpen) (WebSocketStream, error) {
	path := forwardWebSocketPath(t.codeSessionUUID, open.Port)
	header, err := t.signedHeader(http.MethodGet, path, forwardRequest{Path: open.Path, Headers: open.Headers, Protocols: open.Protocols})
	if err != nil {
		return nil, err
	}
	conn, response, err := t.client.dialer.DialContext(ctx, "ws://"+t.address+path, header)
	if err != nil {
		if response != nil && response.StatusCode != http.StatusSwitchingProtocols {
			defer response.Body.Close()
			return nil, t.failure(response)
		}
		return nil, t.transportError(ctx, err)
	}
	flowControl := response.Header.Get(forwardFlowHeader) == "1"
	t.flowControl.Store(flowControl)
	return newSocketStream(conn, response.Header.Get(forwardProtocolHeader), forwardWriteTimeout(flowControl)), nil
}

func (t *remoteTunnel) signedHeader(method, path string, request forwardRequest) (http.Header, error) {
	encoded, err := encodeForwardRequest(request)
	if err != nil {
		return nil, err
	}
	header := http.Header{}
	header.Set(forwardRequestHeader, encoded)
	header.Set(forwardSignatureHeader, t.client.signer.sign(method, path, t.owner, encoded))
	return header, nil
}

func (t *remoteTunnel) transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		t.gone(err)
		return ErrTunnelGone
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w: forward to session port tunnel owner: %w", context.DeadlineExceeded, err)
	}
	return fmt.Errorf("%w: forward to session port tunnel owner: %w", ErrTunnelDisconnected, err)
}

func (t *remoteTunnel) failure(response *http.Response) error {
	var failure forwardFailure
	if err := json.NewDecoder(io.LimitReader(response.Body, maxForwardFailure)).Decode(&failure); err != nil {
		failure.Code = forwardDisconnected
	}
	switch failure.Code {
	case forwardNoTunnel:
		t.gone(ErrNoTunnel)
	case forwardUnauthorized, forwardBadRequest:
		t.logger.Error("session port forward rejected by owner", "owner", t.owner, "code_session_uuid", t.codeSessionUUID, "code", failure.Code)
	}
	return failure.err()
}

func (t *remoteTunnel) gone(cause error) {
	t.logger.Info("session port tunnel owner gone", "owner", t.owner, "code_session_uuid", t.codeSessionUUID, "error", cause)
	t.release(t.owner)
}

type remoteBody struct {
	body     io.ReadCloser
	reader   *bufio.Reader
	buffer   []byte
	timedOut atomic.Bool
	finished bool
}

func (b *remoteBody) Next(ctx context.Context, idle time.Duration) ([]byte, error) {
	if b.finished {
		return nil, io.EOF
	}
	stop := context.AfterFunc(ctx, func() { _ = b.body.Close() })
	defer stop()
	if idle > 0 {
		timer := time.AfterFunc(idle, func() {
			b.timedOut.Store(true)
			_ = b.body.Close()
		})
		defer timer.Stop()
	}
	n, err := b.reader.Read(b.buffer)
	if n > 0 {
		return b.buffer[:n], nil
	}
	b.finished = true
	_ = b.body.Close()
	switch {
	case errors.Is(err, io.EOF):
		return nil, io.EOF
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case b.timedOut.Load():
		return nil, context.DeadlineExceeded
	default:
		return nil, fmt.Errorf("%w: %w", ErrTunnelDisconnected, err)
	}
}

func (b *remoteBody) Close() {
	if b.finished {
		return
	}
	b.finished = true
	_ = b.body.Close()
}
