package portgateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

const (
	maxWebSocketMessageBytes = 1 << 20
	closeWriteTimeout        = time.Second
)

func (g *Gateway) proxyWebSocket(w http.ResponseWriter, r *http.Request, resolved target, tunnel porttunnel.Tunnel) {
	open := porttunnel.WebSocketOpen{
		Port:      resolved.address.Port,
		Path:      requestPath(r),
		Headers:   forwardRequestHeaders(r, g.cfg.StripCookieNames, true),
		Protocols: websocket.Subprotocols(r),
	}
	stream, tunnel, err := retryWhenOwnerGone(r.Context(), g, resolved, tunnel, func(tunnel porttunnel.Tunnel) (porttunnel.WebSocketStream, error) {
		return tunnel.OpenWebSocket(r.Context(), open)
	})
	if err != nil {
		if r.Context().Err() == nil {
			g.fail(w, r, roundTripFailure(err))
		}
		return
	}
	responseHeader := http.Header{}
	responseHeader.Set(referrerPolicyHeader, referrerPolicy)
	if protocol := stream.Protocol(); protocol != "" {
		responseHeader.Set("Sec-WebSocket-Protocol", protocol)
	}
	caller, err := g.upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		stream.Close(websocket.CloseGoingAway, "caller handshake failed")
		return
	}
	caller.SetReadLimit(maxWebSocketMessageBytes)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go g.renewDuring(ctx, resolved)
	go func() {
		defer cancel()
		pumpCallerToTunnel(ctx, caller, stream)
	}()
	pumpTunnelToCaller(ctx, caller, stream, g.callerWriteTimeout(tunnel))
	cancel()
	_ = caller.Close()
}

func (g *Gateway) renewDuring(ctx context.Context, resolved target) {
	interval := max(g.sandboxTimeout/3, time.Second)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			g.renewIfDue(resolved.session, now)
		}
	}
}

func pumpCallerToTunnel(ctx context.Context, caller *websocket.Conn, stream porttunnel.WebSocketStream) {
	for {
		kind, data, err := caller.ReadMessage()
		if err != nil {
			code, reason := callerCloseStatus(err)
			stream.Close(code, reason)
			return
		}
		message := porttunnel.WebSocketMessage{Data: data, Binary: kind == websocket.BinaryMessage}
		if err := stream.Send(ctx, message); err != nil {
			return
		}
	}
}

func callerCloseStatus(err error) (int, string) {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code, closeErr.Text
	}
	if errors.Is(err, websocket.ErrReadLimit) {
		return websocket.CloseMessageTooBig, "message too big"
	}
	return websocket.CloseGoingAway, "caller disconnected"
}

func pumpTunnelToCaller(ctx context.Context, caller *websocket.Conn, stream porttunnel.WebSocketStream, writeTimeout time.Duration) {
	for {
		message, err := stream.Receive(ctx)
		if err != nil {
			_ = caller.WriteControl(websocket.CloseMessage, tunnelCloseFrame(err), time.Now().Add(closeWriteTimeout))
			return
		}
		kind := websocket.TextMessage
		if message.Binary {
			kind = websocket.BinaryMessage
		}
		if err := caller.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			stream.Close(websocket.CloseGoingAway, "caller disconnected")
			return
		}
		if err := caller.WriteMessage(kind, message.Data); err != nil {
			stream.Close(websocket.CloseGoingAway, "caller disconnected")
			return
		}
	}
}

func tunnelCloseFrame(err error) []byte {
	var closeErr *porttunnel.CloseError
	if errors.As(err, &closeErr) {
		code, reason := porttunnel.NormalizeClose(closeErr.Code, closeErr.Reason)
		return websocket.FormatCloseMessage(code, reason)
	}
	if errors.Is(err, porttunnel.ErrStalled) {
		return websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "caller stopped reading")
	}
	return websocket.FormatCloseMessage(websocket.CloseGoingAway, "session port tunnel disconnected")
}
