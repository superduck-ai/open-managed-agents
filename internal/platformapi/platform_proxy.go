package platformapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/llmproviders"
	"github.com/superduck-ai/open-managed-agents/internal/secrets"

	"github.com/go-chi/chi/v5"
)

func RegisterOrganizationProxyRoutes(r chi.Router, database *db.DB, secretService *secrets.Service) {
	r.Post("/proxy/v1/messages", handleProxyMessages(database, secretService, llmproviders.NewHTTPClient(0)))
}

func handleProxyMessages(database *db.DB, secretService *secrets.Service, client *http.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := visibleOrgUUID(w, r); !ok {
			return
		}
		principal, ok := auth.PrincipalFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "authentication_error"})
			return
		}
		defer func() { _ = r.Body.Close() }()
		if r.ContentLength > llmproviders.MaxMessagesRequestBodyBytes {
			writeProxyRequestTooLarge(w)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, llmproviders.MaxMessagesRequestBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeProxyRequestTooLarge(w)
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request_error", "message": "failed to read body"})
			return
		}
		modelID, err := proxyMessagesModel(body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request_error", "message": err.Error()})
			return
		}
		upstream, err := llmproviders.Resolve(
			r.Context(),
			database,
			secretService,
			principal.OrganizationUUID,
			principal.WorkspaceUUID,
			modelID,
		)
		if err != nil {
			writeProxyProviderError(w, err)
			return
		}
		targetURL, err := llmproviders.Endpoint(upstream.BaseURL, "/v1/messages", "")
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "proxy_error", "message": "LLM provider is unavailable"})
			return
		}
		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": "failed to build upstream request"})
			return
		}
		upstreamReq.ContentLength = int64(len(body))
		upstreamReq.Header = proxyMessagesHeaders(r.Header, upstream.APIKey)

		upstreamRes, err := client.Do(upstreamReq)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": "LLM provider request failed"})
			return
		}
		defer upstreamRes.Body.Close()

		if proxyMessagesWantsStream(body) {
			contentType := upstreamRes.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "text/event-stream"
			}
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(upstreamRes.StatusCode)
			proxyMessagesStream(w, upstreamRes.Body)
			return
		}

		responseBody, err := io.ReadAll(upstreamRes.Body)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": "failed to read LLM provider response"})
			return
		}
		contentType := upstreamRes.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(upstreamRes.StatusCode)
		_, _ = w.Write(responseBody)
	}
}

func writeProxyRequestTooLarge(w http.ResponseWriter) {
	writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{
		"error":   "request_too_large",
		"message": "Request body exceeds maximum size",
	})
}

func proxyMessagesHeaders(source http.Header, apiKey string) http.Header {
	headers := make(http.Header)
	for _, name := range []string{"Accept", "Content-Type", "Anthropic-Version", "Anthropic-Beta"} {
		for _, value := range source.Values(name) {
			headers.Add(name, value)
		}
	}
	llmproviders.ApplyAPIKey(headers, apiKey)
	return headers
}

func proxyMessagesModel(body []byte) (string, error) {
	return llmproviders.MessageRequestModel(body)
}

func writeProxyProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, llmproviders.ErrModelNotConfigured):
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request_error", "message": "model is not configured for this workspace"})
	case errors.Is(err, llmproviders.ErrNotConfigured):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "proxy_error", "message": "workspace has no LLM provider configured"})
	default:
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "proxy_error", "message": "workspace LLM provider is unavailable"})
	}
}

func proxyMessagesWantsStream(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Stream
}
