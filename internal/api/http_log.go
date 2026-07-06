package api

import (
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"

	"github.com/go-chi/chi/v5/middleware"
)

var apiClientUserAgentPattern = regexp.MustCompile(`\b(anthropic-sdk|curl|httpie|postman|insomnia|python-requests|okhttp)\b`)

func requestLoggingMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fields := requestLogFields(r)
			requestID := httpapi.RequestID(r.Context())
			startedAt := time.Now()
			logger.Info(requestLogMessage(fields), httpLogAttrs("request", requestID, fields)...)

			wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r)

			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}
			attrs := httpLogAttrs("response", requestID, fields,
				"status", status,
				"durationMs", durationMilliseconds(startedAt),
			)
			message := responseLogMessage(fields, status)
			if status >= http.StatusOK && status < http.StatusMultipleChoices {
				logger.Info(message, attrs...)
			} else {
				logger.Error(message, attrs...)
			}
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
	if uri := r.URL.RequestURI(); uri != "" {
		return uri
	}
	return r.URL.Path
}

func httpLogAttrs(event string, requestID string, fields logRequestFields, afterURL ...any) []any {
	attrs := []any{
		"event", event,
		"requestId", requestID,
		"method", fields.method,
		"url", fields.url,
	}
	attrs = append(attrs, afterURL...)
	attrs = append(attrs,
		"path", fields.path,
		"host", fields.host,
		"userAgent", fields.userAgent,
		"clientKind", fields.clientKind,
	)
	if fields.anthropicClientPlatform != "" {
		attrs = append(attrs, "anthropicClientPlatform", fields.anthropicClientPlatform)
	}
	if fields.anthropicClientApp != "" {
		attrs = append(attrs, "anthropicClientApp", fields.anthropicClientApp)
	}
	return attrs
}

func requestLogMessage(fields logRequestFields) string {
	return ">>> " + fields.method + " " + fields.url
}

func responseLogMessage(fields logRequestFields, status int) string {
	return "<<< " + fields.method + " " + fields.url + " " + strconv.Itoa(status)
}

func durationMilliseconds(startedAt time.Time) float64 {
	ms := float64(time.Since(startedAt).Microseconds()) / 1000
	return math.Round(ms*10) / 10
}

func detectRequestClientKind(r *http.Request) string {
	clientPlatform := normalizedLogHeader(r, "Anthropic-Client-Platform")
	clientApp := normalizedLogHeader(r, "Anthropic-Client-App")
	userAgent := normalizedLogHeader(r, "User-Agent")

	if strings.Contains(clientPlatform, "ios") ||
		strings.Contains(clientPlatform, "iphone") ||
		strings.Contains(clientPlatform, "ipad") ||
		strings.Contains(clientPlatform, "ipod") {
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

	if strings.Contains(clientApp, "ios") ||
		strings.Contains(clientApp, "iphone") ||
		strings.Contains(clientApp, "ipad") ||
		strings.Contains(clientApp, "ipod") {
		return "ios"
	}
	if strings.Contains(clientApp, "android") {
		return "android"
	}
	if strings.Contains(clientApp, "claudefordesktop") ||
		strings.Contains(clientApp, "desktop") ||
		strings.Contains(clientApp, "electron") ||
		strings.Contains(clientApp, "todesktop") {
		return "desktop"
	}

	if strings.Contains(userAgent, "iphone") ||
		strings.Contains(userAgent, "ipad") ||
		strings.Contains(userAgent, "ipod") ||
		strings.Contains(userAgent, "cfnetwork") {
		return "ios"
	}
	if strings.Contains(userAgent, "android") {
		return "android"
	}
	if (strings.Contains(userAgent, "claude") || strings.Contains(userAgent, "anthropic")) &&
		(strings.Contains(userAgent, "desktop") || strings.Contains(userAgent, "nest")) {
		return "desktop"
	}
	if strings.Contains(userAgent, "electron") ||
		strings.Contains(userAgent, "todesktop") ||
		strings.Contains(userAgent, "claudefordesktop") {
		return "desktop"
	}
	if hasStainlessClientHeader(r) || apiClientUserAgentPattern.MatchString(userAgent) {
		return "api"
	}
	if strings.Contains(userAgent, "mozilla") ||
		strings.Contains(userAgent, "chrome") ||
		strings.Contains(userAgent, "chromium") ||
		strings.Contains(userAgent, "safari") ||
		strings.Contains(userAgent, "firefox") ||
		strings.Contains(userAgent, "edg") ||
		strings.Contains(userAgent, "opera") {
		return "web"
	}
	if userAgent != "" {
		return "api"
	}
	return "unknown"
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
