package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

const (
	clientClosedRequestStatus = 499
	slowRequestLogThreshold   = 2 * time.Second
)

var httpRequestDuration = telemetry.NewInstruments(func(meter metric.Meter) metric.Float64Histogram {
	return telemetry.Histogram(meter, "http.server.request.duration", "s", "HTTP server request duration", telemetry.LatencyBuckets)
})

type routeParam struct {
	logKey string
	attr   attribute.Key
}

var routeParams = map[string]routeParam{
	"session_id":      {logKey: "session_id", attr: telemetry.AttrSessionID},
	"sessionId":       {logKey: "session_id", attr: telemetry.AttrSessionID},
	"code_session_id": {logKey: "code_session_id", attr: telemetry.AttrCodeSessionID},
	"thread_id":       {attr: telemetry.AttrThreadID},
}

type requestTelemetry struct {
	traced   bool
	measured bool
	longPoll bool
}

var (
	tracedRequest     = requestTelemetry{traced: true, measured: true}
	measuredRequest   = requestTelemetry{measured: true}
	unobservedRequest = requestTelemetry{}
	longPollRequest   = requestTelemetry{longPoll: true}
)

var routeTelemetry = map[string]requestTelemetry{
	"/healthz":           unobservedRequest,
	"/readyz":            unobservedRequest,
	"/connector/*":       unobservedRequest,
	"/v1/filestore/fs/*": unobservedRequest,
	"/v1/sessions/{session_id}/events/stream":                   unobservedRequest,
	"/v1/sessions/{session_id}/threads/{thread_id}/stream":      unobservedRequest,
	"/web-api/sessions/{sessionId}/stream":                      unobservedRequest,
	"/v1/code/upstreamproxy/ws":                                 unobservedRequest,
	"/v1/code/sessions/{code_session_id}/worker/events/stream":  unobservedRequest,
	"/v1/code/sessions/{code_session_id}/worker/otlp/metrics":   unobservedRequest,
	"/v1/code/sessions/{code_session_id}/worker/otlp/logs":      unobservedRequest,
	"/v1/code/sessions/{code_session_id}/worker/otlp/v1/logs":   unobservedRequest,
	"/v1/code/sessions/{code_session_id}/worker/otlp/v1/traces": unobservedRequest,
	"/v1/code/sessions/{code_session_id}/worker/heartbeat":      measuredRequest,
	"/v1/code/brain/claims":                                     longPollRequest,
}

type observedResponse struct {
	status   int
	canceled bool
}

func serveObserved(w http.ResponseWriter, r *http.Request, next http.Handler) observedResponse {
	wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
	next.ServeHTTP(wrapped, r)
	response := observedResponse{status: wrapped.Status()}
	if response.status == 0 {
		response.status = http.StatusOK
	}
	if response.status >= http.StatusInternalServerError && httpapi.ClientGone(r) {
		response.status, response.canceled = clientClosedRequestStatus, true
	}
	return response
}

func eachRouteParam(r *http.Request, visit func(routeParam, string)) {
	routeContext := chi.RouteContext(r.Context())
	if routeContext == nil {
		return
	}
	for index, key := range routeContext.URLParams.Keys {
		if param, ok := routeParams[key]; ok && index < len(routeContext.URLParams.Values) {
			visit(param, routeContext.URLParams.Values[index])
		}
	}
}

func routeParamLogAttrs(r *http.Request) []any {
	var attrs []any
	eachRouteParam(r, func(param routeParam, value string) {
		if param.logKey != "" {
			attrs = append(attrs, param.logKey, value)
		}
	})
	return attrs
}

func routeParamAttributes(r *http.Request) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	eachRouteParam(r, func(param routeParam, value string) {
		attrs = append(attrs, param.attr.String(value))
	})
	return attrs
}

func telemetryMiddleware(routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return observeRequests(routes, next)
	}
}

func observeRequests(routes chi.Routes, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		policy := requestTelemetryFor(routes, r)
		if !policy.measured {
			next.ServeHTTP(w, r)
			return
		}
		startedAt := time.Now()
		ctx, span := r.Context(), trace.SpanFromContext(r.Context())
		if policy.traced {
			ctx, span = telemetry.StartRoot(ctx, r.Method, workerTraceParent(r),
				attribute.String("http.request.method", r.Method),
				telemetry.AttrRequestID.String(httpapi.RequestID(r.Context())),
			)
		}
		r = r.WithContext(ctx)
		response := serveObserved(w, r, next)
		if response.canceled {
			span.SetAttributes(attribute.Bool("oma.client_canceled", true))
		}
		route := requestRoutePattern(r)
		span.SetName(r.Method + " " + route)
		span.SetAttributes(attribute.String("http.route", route), attribute.Int("http.response.status_code", response.status))
		span.SetAttributes(routeParamAttributes(r)...)
		if response.status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(response.status))
		}
		span.End()
		httpRequestDuration.Get().Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", response.status),
		))
	})
}

func workerTraceParent(r *http.Request) string {
	path := r.URL.Path
	if path == "/v1/messages" || strings.HasPrefix(path, "/v1/code/") || strings.HasPrefix(path, "/v2/ccr-sessions/") || strings.HasPrefix(path, "/v2/sessions/") {
		return r.Header.Get(telemetry.TraceParentHeader)
	}
	return ""
}

func requestTelemetryFor(routes chi.Routes, r *http.Request) requestTelemetry {
	if routes == nil {
		return tracedRequest
	}
	if policy, ok := routeTelemetry[routes.Find(chi.NewRouteContext(), r.Method, r.URL.Path)]; ok {
		return policy
	}
	return tracedRequest
}

func requestRoutePattern(r *http.Request) string {
	if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
		if pattern := routeContext.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
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
