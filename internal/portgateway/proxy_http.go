package portgateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

func (g *Gateway) proxyHTTP(w http.ResponseWriter, r *http.Request, resolved target, tunnel porttunnel.Tunnel) {
	body, failure := g.readRequestBody(w, r)
	if failure != nil {
		g.fail(w, r, failure)
		return
	}
	controller := http.NewResponseController(w)
	request := porttunnel.HTTPRequest{
		Port:    resolved.address.Port,
		Method:  r.Method,
		Path:    requestPath(r),
		Headers: forwardRequestHeaders(r, g.cfg.StripCookieNames, false),
		Body:    body,
	}
	response, tunnel, err := retryWhenOwnerGone(r.Context(), g, resolved, tunnel, func(tunnel porttunnel.Tunnel) (*porttunnel.HTTPResponse, error) {
		return tunnel.RoundTrip(r.Context(), request)
	})
	if err != nil {
		if r.Context().Err() == nil {
			g.fail(w, r, roundTripFailure(err))
		}
		return
	}
	defer response.Body.Close()
	applyResponseHeaders(w.Header(), response.Headers)
	writeTimeout := g.callerWriteTimeout(tunnel)
	_ = extendWriteDeadline(controller, writeTimeout)
	w.WriteHeader(response.StatusCode)
	_ = controller.Flush()
	if err := g.streamBody(r.Context(), w, controller, resolved, response.Body, writeTimeout); err != nil {
		g.logger.InfoContext(r.Context(), "session port response aborted", "session_id", resolved.session.SessionExternalID, "port", resolved.address.Port, "error", err)
		panic(http.ErrAbortHandler)
	}
}

func (g *Gateway) readRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, *gatewayError) {
	if r.ContentLength > g.cfg.MaxRequestBodyBytes {
		return nil, requestTooLarge()
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, g.cfg.MaxRequestBodyBytes))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, requestTooLarge()
	}
	if err != nil {
		return nil, tunnelDisconnected(err)
	}
	return body, nil
}

func (g *Gateway) streamBody(ctx context.Context, w http.ResponseWriter, controller *http.ResponseController, resolved target, body porttunnel.ResponseBody, writeTimeout time.Duration) error {
	for {
		data, err := body.Next(ctx, g.cfg.ResponseIdleTimeout)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := extendWriteDeadline(controller, writeTimeout); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := controller.Flush(); err != nil {
			return err
		}
		g.renewIfDue(resolved.session, time.Now())
	}
}

func extendWriteDeadline(controller *http.ResponseController, timeout time.Duration) error {
	err := controller.SetWriteDeadline(time.Now().Add(timeout))
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func requestPath(r *http.Request) string {
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	return path
}

func roundTripFailure(err error) *gatewayError {
	var failure *gatewayError
	if errors.As(err, &failure) {
		return failure
	}
	switch {
	case errors.Is(err, porttunnel.ErrTunnelGone):
		return waking(err)
	case errors.Is(err, porttunnel.ErrTooManyRequests), errors.Is(err, porttunnel.ErrTooManyWebSockets):
		return tooManyRequests(err)
	case porttunnel.IsHeaderTimeout(err), errors.Is(err, context.DeadlineExceeded):
		return upstreamTimeout(err)
	default:
		return tunnelDisconnected(err)
	}
}
