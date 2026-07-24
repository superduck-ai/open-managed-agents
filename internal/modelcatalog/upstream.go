package modelcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/aiupstream"
	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const (
	anthropicAPIVersion     = "2023-06-01"
	maxModelsResponseBytes  = 4 << 20
	upstreamModelsPageLimit = 1000
)

var errInvalidUpstreamResponse = errors.New("invalid upstream model response")

type HTTPUpstream struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewHTTPUpstream(upstream config.AnthropicUpstreamConfig) *HTTPUpstream {
	return &HTTPUpstream{
		baseURL: upstream.BaseURL,
		apiKey:  strings.TrimSpace(upstream.APIKey),
		client:  aiupstream.NewHTTPClient(nil, 0),
	}
}

func (u *HTTPUpstream) List(ctx context.Context, afterID string) (Page, error) {
	if u.apiKey == "" {
		return Page{}, errors.New("model catalog upstream API key is not configured")
	}
	endpoint, err := modelsEndpoint(u.baseURL, afterID)
	if err != nil {
		return Page{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return Page{}, fmt.Errorf("create models request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Anthropic-Version", anthropicAPIVersion)
	request.Header.Set("X-Api-Key", u.apiKey)

	response, err := u.client.Do(request)
	if err != nil {
		return Page{}, fmt.Errorf("request models: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return Page{}, fmt.Errorf("models request returned %s", response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxModelsResponseBytes+1))
	if err != nil {
		return Page{}, fmt.Errorf("read models response: %w", err)
	}
	if len(body) > maxModelsResponseBytes {
		return Page{}, errInvalidUpstreamResponse
	}

	var payload upstreamModelsResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return Page{}, fmt.Errorf("%w: %v", errInvalidUpstreamResponse, err)
	}
	models := make([]Model, 0, len(payload.Data))
	for _, upstreamModel := range payload.Data {
		model, err := upstreamModel.catalogModel()
		if err != nil {
			return Page{}, err
		}
		models = append(models, model)
	}
	return Page{Models: models, HasMore: payload.HasMore, LastID: payload.LastID}, nil
}

func modelsEndpoint(baseURL string, afterID string) (string, error) {
	endpoint, err := aiupstream.Endpoint(baseURL, "v1/models", "")
	if err != nil {
		return "", err
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse models endpoint: %w", err)
	}
	query := endpointURL.Query()
	query.Set("limit", strconv.Itoa(upstreamModelsPageLimit))
	if afterID != "" {
		query.Set("after_id", afterID)
	}
	endpointURL.RawQuery = query.Encode()
	return endpointURL.String(), nil
}

type upstreamModelsResponse struct {
	Data    []upstreamModel `json:"data"`
	HasMore bool            `json:"has_more"`
	LastID  string          `json:"last_id"`
}

type upstreamModel struct {
	ID               string          `json:"id"`
	DisplayName      string          `json:"display_name"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	CreatedAt        json.RawMessage `json:"created_at"`
	Created          json.RawMessage `json:"created"`
	MaxInputTokens   *int            `json:"max_input_tokens"`
	MaxTokens        *int            `json:"max_tokens"`
	Capabilities     json.RawMessage `json:"capabilities"`
	SupportsThinking *bool           `json:"supports_thinking"`
	SupportsToolUse  *bool           `json:"supports_tool_use"`
}

func (m upstreamModel) catalogModel() (Model, error) {
	if m.ID == "" || m.ID != strings.TrimSpace(m.ID) {
		return Model{}, fmt.Errorf("%w: model id must be a non-empty trimmed string", errInvalidUpstreamResponse)
	}
	displayName := strings.TrimSpace(m.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(m.Name)
	}
	if displayName == "" {
		displayName = m.ID
	}
	createdAt, err := modelCreatedAt(m.CreatedAt, m.Created)
	if err != nil {
		return Model{}, err
	}
	capabilities, err := parseCapabilities(m.Capabilities)
	if err != nil {
		return Model{}, err
	}
	if m.SupportsThinking != nil {
		capabilities.setSupported("thinking", m.SupportsThinking)
	}
	if m.SupportsToolUse != nil {
		capabilities.setSupported("tool_use", m.SupportsToolUse)
	}
	return Model{
		ID:             m.ID,
		DisplayName:    displayName,
		Description:    strings.TrimSpace(m.Description),
		CreatedAt:      createdAt,
		MaxInputTokens: cloneInt(m.MaxInputTokens),
		MaxTokens:      cloneInt(m.MaxTokens),
		Capabilities:   capabilities,
	}, nil
}

func modelCreatedAt(createdAt json.RawMessage, created json.RawMessage) (string, error) {
	for _, raw := range []json.RawMessage{createdAt, created} {
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, nil
		}
		var seconds int64
		if err := json.Unmarshal(raw, &seconds); err == nil {
			return time.Unix(seconds, 0).UTC().Format(time.RFC3339), nil
		}
		return "", fmt.Errorf("%w: model created time is invalid", errInvalidUpstreamResponse)
	}
	return "", nil
}

func parseCapabilities(raw json.RawMessage) (Capabilities, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return Capabilities{}, nil
	}
	var capabilities Capabilities
	if err := json.Unmarshal(raw, &capabilities); err != nil {
		return Capabilities{}, fmt.Errorf("%w: %v", errInvalidUpstreamResponse, err)
	}
	return capabilities, nil
}
