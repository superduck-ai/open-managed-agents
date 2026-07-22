package websearch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type TavilyClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

type tavilySearchRequest struct {
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth"`
	MaxResults     int      `json:"max_results"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
}

var _ Provider = (*TavilyClient)(nil)

func NewTavilyClient(endpoint, apiKey string, timeout time.Duration, client *http.Client) *TavilyClient {
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

func (c *TavilyClient) Search(ctx context.Context, request SearchRequest) (SearchResponse, error) {
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return SearchResponse{}, errors.New("web search query is required")
	}
	if c == nil || c.apiKey == "" {
		return SearchResponse{}, errors.New("web search provider is not configured")
	}
	options := request.Options
	if options.MaxResults <= 0 || options.MaxResults > maxResults {
		options.MaxResults = 5
	}
	payload := tavilySearchRequest{
		Query: query, SearchDepth: "basic", MaxResults: options.MaxResults,
		IncludeDomains: options.IncludeDomains, ExcludeDomains: options.ExcludeDomains,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("marshal web search request: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return SearchResponse{}, fmt.Errorf("build web search request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+c.apiKey)
	responseBody, err := fetchLimitedBody(c.client, httpRequest, maxResponseSize, "tavily")
	if err != nil {
		return SearchResponse{}, err
	}
	var result struct {
		Results []struct {
			Title         string `json:"title"`
			URL           string `json:"url"`
			Content       string `json:"content"`
			PublishedDate string `json:"published_date"`
		} `json:"results"`
	}
	if err := json.Unmarshal(responseBody, &result); err != nil {
		return SearchResponse{}, fmt.Errorf("decode web search response: %w", err)
	}
	results := make([]Result, 0, len(result.Results))
	for _, item := range result.Results {
		results = append(results, Result{
			Title:         item.Title,
			URL:           item.URL,
			Snippet:       item.Content,
			PublishedDate: item.PublishedDate,
		})
	}
	return SearchResponse{Results: results}, nil
}
