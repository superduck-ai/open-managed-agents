package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultEndpoint = "https://api.tavily.com/search"
	defaultTimeout  = 30 * time.Second
	maxResponseSize = 2 << 20
	maxResults      = 10
)

type Result struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Content       string `json:"content"`
	PublishedDate string `json:"published_date,omitempty"`
}

type SearchOptions struct {
	MaxResults     int
	IncludeDomains []string
	ExcludeDomains []string
}

type TavilyClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

var _ Provider = (*TavilyClient)(nil)

func NewTavilyClient(endpoint string, apiKey string, timeout time.Duration, client *http.Client) *TavilyClient {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = DefaultEndpoint
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if client == nil {
		client = &http.Client{}
	}
	configured := *client
	configured.Timeout = timeout
	return &TavilyClient{endpoint: strings.TrimRight(strings.TrimSpace(endpoint), "/"), apiKey: strings.TrimSpace(apiKey), client: &configured}
}

func (c *TavilyClient) Search(ctx context.Context, query string, options SearchOptions) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("web search query is required")
	}
	if c == nil || c.apiKey == "" {
		return nil, errors.New("web search provider is not configured")
	}
	if options.MaxResults <= 0 || options.MaxResults > maxResults {
		options.MaxResults = 5
	}
	payload := map[string]any{"api_key": c.apiKey, "query": query, "search_depth": "basic", "max_results": options.MaxResults}
	if len(options.IncludeDomains) > 0 {
		payload["include_domains"] = options.IncludeDomains
	}
	if len(options.ExcludeDomains) > 0 {
		payload["exclude_domains"] = options.ExcludeDomains
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal web search request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build web search request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("web search request failed: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := readResponse(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read web search response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("web search provider returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Results []Result `json:"results"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return nil, fmt.Errorf("decode web search response: %w", err)
	}
	return result.Results, nil
}

func readResponse(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxResponseSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseSize {
		return nil, errors.New("web search response exceeds maximum size")
	}
	return data, nil
}
