package api

import (
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

var apiClientUserAgentPattern = regexp.MustCompile(`\b(anthropic-sdk|curl|httpie|postman|insomnia|python-requests|okhttp)\b`)

func requestLoggingMiddleware(logger *slog.Logger, routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fields := requestLogFields(r)
			requestID := httpapi.RequestID(r.Context())
			startedAt := time.Now()
			logger.DebugContext(r.Context(), "http request", httpLogAttrs("request", requestID, fields)...)

			response := serveObserved(w, r, next)
			elapsed := time.Since(startedAt)
			attrs := httpLogAttrs("response", requestID, fields,
				"status", response.status,
				"duration_ms", durationMilliseconds(elapsed),
			)
			attrs = append(attrs, routeParamLogAttrs(r)...)
			logger.Log(r.Context(), responseLogLevel(response.status, response.canceled, elapsed, requestTelemetryFor(routes, r).longPoll), "http response", attrs...)
		})
	}
}

type logRequestFields struct {
	method                  string
	url                     string
	path                    string
	host                    string
	userAgent               string
	clientKind              string
	anthropicClientPlatform string
	anthropicClientApp      string
}

func requestLogFields(r *http.Request) logRequestFields {
	path := ""
	if r.URL != nil {
		path = r.URL.Path
	}
	return logRequestFields{
		method:                  r.Method,
		url:                     requestLogURL(r),
		path:                    path,
		host:                    r.Host,
		userAgent:               strings.TrimSpace(r.Header.Get("User-Agent")),
		clientKind:              detectRequestClientKind(r),
		anthropicClientPlatform: strings.TrimSpace(r.Header.Get("Anthropic-Client-Platform")),
		anthropicClientApp:      strings.TrimSpace(r.Header.Get("Anthropic-Client-App")),
	}
}

func requestLogURL(r *http.Request) string {
	if r.URL == nil {
		return ""
	}
	// Query values commonly carry OAuth state/code and other credentials. Access
	// logs keep the established "url" field but intentionally store path only.
	return r.URL.Path
}

func httpLogAttrs(event string, requestID string, fields logRequestFields, afterURL ...any) []any {
	attrs := []any{
		"event", event,
		"request_id", requestID,
		"method", fields.method,
		"url", fields.url,
	}
	attrs = append(attrs, afterURL...)
	attrs = append(attrs,
		"path", fields.path,
		"host", fields.host,
		"user_agent", fields.userAgent,
		"client_kind", fields.clientKind,
	)
	if fields.anthropicClientPlatform != "" {
		attrs = append(attrs, "anthropic_client_platform", fields.anthropicClientPlatform)
	}
	if fields.anthropicClientApp != "" {
		attrs = append(attrs, "anthropic_client_app", fields.anthropicClientApp)
	}
	return attrs
}

func responseLogLevel(status int, canceled bool, elapsed time.Duration, longPoll bool) slog.Level {
	switch {
	case canceled:
		return slog.LevelInfo
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	case !longPoll && elapsed >= slowRequestLogThreshold:
		return slog.LevelInfo
	default:
		return slog.LevelDebug
	}
}

func durationMilliseconds(elapsed time.Duration) float64 {
	ms := float64(elapsed.Microseconds()) / 1000
	return math.Round(ms*10) / 10
}

func detectRequestClientKind(r *http.Request) string {
	clientPlatform := normalizedLogHeader(r, "Anthropic-Client-Platform")
	clientApp := normalizedLogHeader(r, "Anthropic-Client-App")
	userAgent := normalizedLogHeader(r, "User-Agent")

	if kind := clientKindFromPlatform(clientPlatform); kind != "" {
		return kind
	}
	if kind := clientKindFromApp(clientApp); kind != "" {
		return kind
	}
	return clientKindFromUserAgent(r, userAgent)
}

func clientKindFromPlatform(clientPlatform string) string {
	if containsAnyLogValue(clientPlatform, "ios", "iphone", "ipad", "ipod") {
		return "ios"
	}
	if strings.Contains(clientPlatform, "android") {
		return "android"
	}
	if strings.Contains(clientPlatform, "desktop") {
		return "desktop"
	}
	if strings.Contains(clientPlatform, "web") || clientPlatform == "claude_ai" || clientPlatform == "claude-ai" {
		return "web"
	}
	return ""
}

func clientKindFromApp(clientApp string) string {
	if containsAnyLogValue(clientApp, "ios", "iphone", "ipad", "ipod") {
		return "ios"
	}
	if strings.Contains(clientApp, "android") {
		return "android"
	}
	if containsAnyLogValue(clientApp, "claudefordesktop", "desktop", "electron", "todesktop") {
		return "desktop"
	}
	return ""
}

func clientKindFromUserAgent(r *http.Request, userAgent string) string {
	if containsAnyLogValue(userAgent, "iphone", "ipad", "ipod", "cfnetwork") {
		return "ios"
	}
	if strings.Contains(userAgent, "android") {
		return "android"
	}
	if containsAnyLogValue(userAgent, "claude", "anthropic") && containsAnyLogValue(userAgent, "desktop", "nest") {
		return "desktop"
	}
	if containsAnyLogValue(userAgent, "electron", "todesktop", "claudefordesktop") {
		return "desktop"
	}
	if hasStainlessClientHeader(r) || apiClientUserAgentPattern.MatchString(userAgent) {
		return "api"
	}
	if containsAnyLogValue(userAgent, "mozilla", "chrome", "chromium", "safari", "firefox", "edg", "opera") {
		return "web"
	}
	if userAgent != "" {
		return "api"
	}
	return "unknown"
}

func containsAnyLogValue(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func normalizedLogHeader(r *http.Request, name string) string {
	return strings.ToLower(strings.TrimSpace(r.Header.Get(name)))
}

func hasStainlessClientHeader(r *http.Request) bool {
	return r.Header.Get("X-Stainless-Lang") != "" ||
		r.Header.Get("X-Stainless-Package-Version") != "" ||
		r.Header.Get("X-Stainless-Runtime") != "" ||
		r.Header.Get("X-Stainless-Runtime-Version") != ""
}
