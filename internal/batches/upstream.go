package batches

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/aiupstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type UpstreamClient interface {
	Send(ctx context.Context, batch db.MessageBatch, req db.MessageBatchRequest) (UpstreamResult, error)
}

type UpstreamResult struct {
	Status            string
	Result            json.RawMessage
	UpstreamRequestID string
	HTTPStatus        int
}

type HTTPUpstreamClient struct {
	cfg    config.Config
	client *http.Client
}

func NewHTTPUpstreamClient(cfg config.Config) *HTTPUpstreamClient {
	timeout := cfg.Batch.UpstreamTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	return &HTTPUpstreamClient{
		cfg:    cfg,
		client: aiupstream.NewHTTPClient(nil, timeout),
	}
}

func (c *HTTPUpstreamClient) Send(ctx context.Context, batch db.MessageBatch, req db.MessageBatchRequest) (UpstreamResult, error) {
	body := normalizeParams(req.Params)
	endpoint, err := aiupstream.Endpoint(c.cfg.AnthropicUpstream.BaseURL, "v1/messages", "")
	if err != nil {
		return erroredResult("api_error", "invalid upstream base URL"), nil
	}
	if batch.APIVariant == "beta" {
		endpoint += "?beta=true"
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return erroredResult("api_error", "could not create upstream request"), nil
	}
	httpReq.Header.Set("x-api-key", c.cfg.AnthropicUpstream.APIKey)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("anthropic-version", batch.AnthropicVersion)
	if batch.APIVariant == "beta" && len(batch.BetaHeaders) > 0 {
		httpReq.Header.Set("anthropic-beta", strings.Join(batch.BetaHeaders, ", "))
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		if isTimeout(err) {
			return erroredResult("timeout_error", "upstream request timed out"), nil
		}
		return erroredResult("api_error", "upstream request failed: "+err.Error()), nil
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return erroredResult("api_error", "could not read upstream response"), nil
	}
	requestID := resp.Header.Get("request-id")
	if requestID == "" {
		requestID = resp.Header.Get("x-request-id")
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		result, err := json.Marshal(map[string]json.RawMessage{
			"type":    json.RawMessage(`"succeeded"`),
			"message": data,
		})
		if err != nil {
			return erroredResult("api_error", "could not encode upstream result"), nil
		}
		return UpstreamResult{Status: "succeeded", Result: result, UpstreamRequestID: requestID, HTTPStatus: resp.StatusCode}, nil
	}

	errorResponse := normalizeErrorResponse(data, requestID, "api_error", fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode))
	result, err := json.Marshal(map[string]json.RawMessage{
		"type":  json.RawMessage(`"errored"`),
		"error": errorResponse,
	})
	if err != nil {
		return erroredResult("api_error", "could not encode upstream error result"), nil
	}
	return UpstreamResult{Status: "errored", Result: result, UpstreamRequestID: requestID, HTTPStatus: resp.StatusCode}, nil
}

func normalizeParams(raw json.RawMessage) json.RawMessage {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil {
		return raw
	}
	if rawStream, ok := params["stream"]; ok {
		var stream bool
		if json.Unmarshal(rawStream, &stream) == nil && !stream {
			delete(params, "stream")
		}
	}
	data, err := json.Marshal(params)
	if err != nil {
		return raw
	}
	return data
}

func normalizeErrorResponse(data []byte, requestID, fallbackType, fallbackMessage string) json.RawMessage {
	var envelope struct {
		Type      string          `json:"type"`
		RequestID *string         `json:"request_id"`
		Error     json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &envelope) == nil && envelope.Type == "error" && len(envelope.Error) > 0 {
		if envelope.RequestID == nil && requestID != "" {
			envelope.RequestID = &requestID
		}
		out, err := json.Marshal(envelope)
		if err == nil {
			return out
		}
	}
	return errorResponse(fallbackType, fallbackMessage, requestID)
}

func erroredResult(errorType, message string) UpstreamResult {
	errorResponse := errorResponse(errorType, message, "")
	result, _ := json.Marshal(map[string]json.RawMessage{
		"type":  json.RawMessage(`"errored"`),
		"error": errorResponse,
	})
	return UpstreamResult{Status: "errored", Result: result}
}

func errorResponse(errorType, message, requestID string) json.RawMessage {
	requestIDJSON := []byte("null")
	if requestID != "" {
		requestIDJSON, _ = json.Marshal(requestID)
	}
	messageJSON, _ := json.Marshal(message)
	errorTypeJSON, _ := json.Marshal(errorType)
	data := fmt.Sprintf(`{"type":"error","request_id":%s,"error":{"type":%s,"message":%s}}`, requestIDJSON, errorTypeJSON, messageJSON)
	return json.RawMessage(data)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
