package llmproviders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxUpstreamModelsBodyBytes = 1 << 20
	anthropicVersion           = "2023-06-01"
)

var ErrUpstreamModels = errors.New("failed to list models from provider")

type upstreamModelsResponse struct {
	Data    *[]upstreamModel `json:"data"`
	Success *bool            `json:"success"`
	Error   json.RawMessage  `json:"error"`
}

type upstreamModel struct {
	ID string `json:"id"`
}

func ListUpstreamModels(ctx context.Context, client *http.Client, upstream Upstream) ([]string, error) {
	if client == nil {
		return nil, ErrUpstreamModels
	}
	endpoint, err := Endpoint(upstream.BaseURL, "/v1/models", "")
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUpstreamModels, err.Error())
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Anthropic-Version", anthropicVersion)
	ApplyAPIKey(request.Header, upstream.APIKey)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUpstreamModels, err.Error())
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxUpstreamModelsBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUpstreamModels, err.Error())
	}
	if len(body) > maxUpstreamModelsBodyBytes {
		return nil, fmt.Errorf("%w: response too large", ErrUpstreamModels)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d", ErrUpstreamModels, response.StatusCode)
	}
	return parseUpstreamModelIDs(body)
}

func parseUpstreamModelIDs(body []byte) ([]string, error) {
	var page upstreamModelsResponse
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("%w: invalid JSON", ErrUpstreamModels)
	}
	if page.rejected() {
		return nil, fmt.Errorf("%w: provider rejected the request", ErrUpstreamModels)
	}
	if page.Data == nil {
		return nil, fmt.Errorf("%w: response is missing data", ErrUpstreamModels)
	}
	rawModelIDs := make([]string, 0, len(*page.Data))
	for _, model := range *page.Data {
		rawModelIDs = append(rawModelIDs, model.ID)
	}
	if len(rawModelIDs) == 0 {
		return []string{}, nil
	}
	return MergeModelIDs(nil, rawModelIDs, len(rawModelIDs)), nil
}

func MergeModelIDs(existing, incoming []string, max int) []string {
	if max < 1 {
		return nil
	}
	merged := make([]string, 0, len(existing)+len(incoming))
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, values := range [][]string{existing, incoming} {
		for _, value := range values {
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			merged = append(merged, value)
			if len(merged) == max {
				return merged
			}
		}
	}
	return merged
}

func (page upstreamModelsResponse) rejected() bool {
	if page.Success != nil && !*page.Success {
		return true
	}
	if len(page.Error) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(string(page.Error))
	return trimmed != "" && trimmed != "null" && trimmed != `""`
}
