package codesessions

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/vaults"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type upstreamProxyExchangeKey struct{}

type upstreamProxyExchange struct {
	codeSessionID string
	host          string
	startedAt     time.Time
}

func withUpstreamProxyExchange(request *http.Request, codeSessionID string, host string) *http.Request {
	exchange := upstreamProxyExchange{codeSessionID: codeSessionID, host: host, startedAt: time.Now()}
	return request.WithContext(context.WithValue(request.Context(), upstreamProxyExchangeKey{}, exchange))
}

func (h *Handler) observeUpstreamProxyResponse(response *http.Response) error {
	if response.StatusCode < http.StatusBadRequest {
		return nil
	}
	request := response.Request
	ctx := request.Context()
	exchange, _ := ctx.Value(upstreamProxyExchangeKey{}).(upstreamProxyExchange)
	durationMS := time.Since(exchange.startedAt).Milliseconds()
	trace.SpanFromContext(ctx).AddEvent("upstream proxy response rejected", trace.WithAttributes(
		attribute.String("server.address", exchange.host),
		attribute.String("url.path", request.URL.Path),
		attribute.Int("http.response.status_code", response.StatusCode),
	))
	h.logger.WarnContext(ctx, "upstream proxy response rejected",
		"code_session_id", exchange.codeSessionID,
		"host", exchange.host,
		"method", request.Method,
		"path", request.URL.Path,
		"status", response.StatusCode,
		"duration_ms", durationMS,
	)
	return nil
}

func (h *Handler) observeUpstreamProxyFailure(request *http.Request, err error) {
	ctx := request.Context()
	exchange, _ := ctx.Value(upstreamProxyExchangeKey{}).(upstreamProxyExchange)
	kind := upstreamProxyErrorKind(err)
	trace.SpanFromContext(ctx).AddEvent("upstream proxy request failed", trace.WithAttributes(
		attribute.String("server.address", exchange.host),
		attribute.String("url.path", request.URL.Path),
		attribute.String("error.type", kind),
	))
	level := slog.LevelWarn
	if kind == "canceled" {
		level = slog.LevelInfo
	}
	h.logger.Log(ctx, level, "upstream proxy request failed",
		"code_session_id", exchange.codeSessionID,
		"host", exchange.host,
		"method", request.Method,
		"path", request.URL.Path,
		"duration_ms", time.Since(exchange.startedAt).Milliseconds(),
		"error_kind", kind,
		"error", err,
	)
}

func upstreamProxyErrorKind(err error) string {
	var dnsError *net.DNSError
	var networkError net.Error
	var operationError *net.OpError
	switch {
	case errors.Is(err, vaults.ErrInjectionRejected):
		return "credential_injection_rejected"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &dnsError):
		return "dns"
	case upstreamProxyTLSError(err):
		return "tls"
	case errors.As(err, &networkError) && networkError.Timeout():
		return "timeout"
	case errors.As(err, &operationError):
		return "connect"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		return "connection_closed"
	default:
		return "other"
	}
}

func upstreamProxyTLSError(err error) bool {
	var certificateError *tls.CertificateVerificationError
	var recordError tls.RecordHeaderError
	var alertError tls.AlertError
	var unknownAuthority x509.UnknownAuthorityError
	var hostnameError x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	return errors.As(err, &certificateError) ||
		errors.As(err, &recordError) ||
		errors.As(err, &alertError) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &hostnameError) ||
		errors.As(err, &invalidCertificate)
}
