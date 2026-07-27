package platformapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/modelmapping"

	"github.com/go-chi/chi/v5"
)

func RegisterOrganizationProxyRoutes(r chi.Router, cfg config.Config) {
	r.Post("/proxy/v1/messages", handleProxyMessages(cfg))
}

type messagesRewriteFields struct {
	Model string `json:"model"`
}

type rawJSONEnvelope map[string]json.RawMessage

func handleProxyMessages(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := visibleOrgUUID(w, r); !ok {
			return
		}
		targetURL, err := anthropicMessagesEndpointFromConfig(cfg)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": err.Error()})
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("failed to read body"))
			return
		}
		defer func() { _ = r.Body.Close() }()

		rewrittenBody, err := rewriteMappedModel(body, cfg.AnthropicUpstream.ModelMappings)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_request_error", "message": err.Error()})
			return
		}
		body = rewrittenBody

		upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(body))
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": err.Error()})
			return
		}
		upstreamReq.Header = r.Header.Clone()
		upstreamReq.Header.Del("Authorization")
		upstreamReq.Header.Del("Host")
		upstreamReq.Header.Set("X-API-Key", strings.TrimSpace(cfg.AnthropicUpstream.APIKey))

		upstreamRes, err := http.DefaultClient.Do(upstreamReq)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": err.Error()})
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
			w.Header().Set("Connection", "keep-alive")
			w.Header().Set("X-Accel-Buffering", "no")
			w.WriteHeader(upstreamRes.StatusCode)
			proxyMessagesStream(w, upstreamRes.Body)
			return
		}

		responseBody, err := io.ReadAll(upstreamRes.Body)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": "proxy_error", "message": err.Error()})
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

func rewriteMappedModel(body []byte, mappings map[string]string) ([]byte, error) {
	if len(mappings) == 0 {
		return body, nil
	}
	var fields messagesRewriteFields
	if err := json.Unmarshal(body, &fields); err != nil {
		return body, nil
	}
	upstreamModel := modelmapping.Resolve(fields.Model, mappings)
	if upstreamModel == fields.Model {
		return body, nil
	}
	var payload rawJSONEnvelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return body, nil
	}
	encodedModel, err := json.Marshal(upstreamModel)
	if err != nil {
		return nil, fmt.Errorf("encode mapped Messages model: %w", err)
	}
	payload["model"] = encodedModel
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Messages request: %w", err)
	}
	return rewritten, nil
}

func anthropicMessagesEndpointFromConfig(cfg config.Config) (string, error) {
	baseURL := strings.TrimSpace(cfg.AnthropicUpstream.BaseURL)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/messages"
	return parsed.String(), nil
}

func proxyMessagesWantsStream(body []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(body, &payload) == nil && payload.Stream
}
