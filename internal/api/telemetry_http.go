package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

var routeParamAttributeKeys = map[string]attribute.Key{
	"session_id":      telemetry.AttrSessionID,
	"sessionId":       telemetry.AttrSessionID,
	"code_session_id": telemetry.AttrCodeSessionID,
	"thread_id":       telemetry.AttrThreadID,
}

func telemetryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skipsRequestTelemetry(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		startedAt := time.Now()
		ctx, span := telemetry.StartRoot(r.Context(), r.Method, "",
			attribute.String("http.request.method", r.Method),
			telemetry.AttrRequestID.String(httpapi.RequestID(r.Context())),
		)
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(wrapped, r.WithContext(ctx))
		status := wrapped.Status()
		if status == 0 {
			status = http.StatusOK
		}
		route := requestRoutePattern(r)
		span.SetName(r.Method + " " + route)
		span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", status))
		span.SetAttributes(routeParamAttributes(r)...)
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
		span.End()
		telemetry.RecordHTTPRequest(ctx, r.Method, route, status, time.Since(startedAt))
	})
}

func skipsRequestTelemetry(path string) bool {
	switch {
	case path == "/healthz", path == "/readyz":
		return true
	case strings.HasSuffix(path, "/stream"), strings.HasSuffix(path, "/worker/heartbeat"):
		return true
	case strings.Contains(path, "/upstreamproxy/ws"), strings.Contains(path, "/worker/otlp/"):
		return true
	case strings.HasPrefix(path, "/v1/filestore/"), strings.HasPrefix(path, "/connector"):
		return true
	default:
		return false
	}
}

func requestRoutePattern(r *http.Request) string {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		if pattern := routeContext.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

func routeParamAttributes(r *http.Request) []attribute.KeyValue {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, 2)
	for index, key := range routeContext.URLParams.Keys {
		attributeKey, ok := routeParamAttributeKeys[key]
		if ok && index < len(routeContext.URLParams.Values) {
			attrs = append(attrs, attributeKey.String(routeContext.URLParams.Values[index]))
		}
	}
	return attrs
}

func annotatePrincipalSpan(ctx context.Context, principal auth.Principal) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		telemetry.AttrOrganizationUUID.String(principal.OrganizationUUID),
		telemetry.AttrWorkspaceUUID.String(principal.WorkspaceUUID),
	)
	if principal.CodeSessionExternalID != "" {
		span.SetAttributes(
			telemetry.AttrCodeSessionID.String(principal.CodeSessionExternalID),
			telemetry.AttrSessionID.String(principal.PublicSessionExternalID),
		)
	}
}
