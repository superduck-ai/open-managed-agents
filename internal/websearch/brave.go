package websearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

const (
	DefaultBraveEndpoint = "https://api.search.brave.com/res/v1/web/search"
	braveMaxResponseSize = 2 << 20
	braveMaxResults      = 20
	braveMaxOffset       = 9
)

type BraveClient struct {
	endpoint       string
	apiKey         string
	client         *http.Client
	defaultOptions BraveOptions
}

var _ Provider = (*BraveClient)(nil)

type BraveClientConfig struct {
	Endpoint string
	APIKey   string
	Timeout  time.Duration
	Options  BraveOptions
}

type BraveOptions struct {
	Country          string   `json:"country"`
	SearchLanguage   string   `json:"search_language"`
	UILanguage       string   `json:"ui_language"`
	Freshness        string   `json:"freshness"`
	StartPublishedAt string   `json:"start_published_at"`
	EndPublishedAt   string   `json:"end_published_at"`
	SafeSearch       string   `json:"safe_search"`
	Spellcheck       *bool    `json:"spellcheck"`
	ResultFilter     string   `json:"result_filter"`
	Goggles          []string `json:"goggles"`
	ExtraSnippets    bool     `json:"extra_snippets"`
	Units            string   `json:"units"`
}

type braveFactory struct{}

func (braveFactory) New(cfg config.WebSearchProviderConfig, timeout time.Duration, client *http.Client) (Provider, error) {
	var options BraveOptions
	if err := decodeProviderOptions(cfg.Options, &options); err != nil {
		return nil, fmt.Errorf("configure brave web search: %w", err)
	}
	if err := options.validate(); err != nil {
		return nil, fmt.Errorf("configure brave web search: %w", err)
	}
	return NewBraveClient(BraveClientConfig{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey,
		Timeout:  timeout,
		Options:  options,
	}, client), nil
}

func (options BraveOptions) validate() error {
	if options.Country != "" && len(options.Country) != 2 {
		return errors.New("country must be a two-letter code")
	}
	switch options.SafeSearch {
	case "", "off", "moderate", "strict":
	default:
		return fmt.Errorf("safe_search %q is invalid", options.SafeSearch)
	}
	switch options.Units {
	case "", "metric", "imperial":
	default:
		return fmt.Errorf("units %q is invalid", options.Units)
	}
	if _, err := braveFreshness(options); err != nil {
		return err
	}
	return nil
}

func NewBraveClient(cfg BraveClientConfig, client *http.Client) *BraveClient {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = DefaultBraveEndpoint
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if client == nil {
		client = &http.Client{}
	}
	configured := *client
	configured.Timeout = cfg.Timeout
	return &BraveClient{
		endpoint:       strings.TrimSpace(cfg.Endpoint),
		apiKey:         strings.TrimSpace(cfg.APIKey),
		client:         &configured,
		defaultOptions: cloneBraveOptions(cfg.Options),
	}
}

func (*BraveClient) ValidateOptions(options SearchOptions) error {
	if len(options.IncludeDomains) > 0 || len(options.ExcludeDomains) > 0 {
		return errors.New("brave web search does not support domain restrictions")
	}
	return nil
}

func (c *BraveClient) Search(ctx context.Context, request SearchRequest) (SearchResponse, error) {
	if c == nil || c.apiKey == "" {
		return SearchResponse{}, errors.New("web search provider is not configured")
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return SearchResponse{}, errors.New("web search query is required")
	}
	if err := c.ValidateOptions(request.Options); err != nil {
		return SearchResponse{}, err
	}
	endpoint, err := c.searchURL(query, request.Options)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("build brave search endpoint: %w", err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SearchResponse{}, fmt.Errorf("build brave search request: %w", err)
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("X-Subscription-Token", c.apiKey)
	responseBody, err := fetchLimitedBody(c.client, httpRequest, braveMaxResponseSize, "brave")
	if err != nil {
		return SearchResponse{}, err
	}
	decoded, err := decodeBraveResponse(responseBody)
	if err != nil {
		return SearchResponse{}, err
	}
	if decoded.HasMore {
		offset := 0
		if request.Options.PageToken != "" {
			offset, err = strconv.Atoi(request.Options.PageToken)
			if err != nil {
				return SearchResponse{}, fmt.Errorf("parse brave search page token: %w", err)
			}
		}
		if offset < braveMaxOffset {
			decoded.NextPageToken = strconv.Itoa(offset + 1)
		}
	}
	return decoded, nil
}

func (c *BraveClient) searchURL(query string, options SearchOptions) (string, error) {
	parsed, err := url.Parse(c.endpoint)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	values.Set("q", query)
	if options.MaxResults > 0 {
		count := options.MaxResults
		if count > braveMaxResults {
			count = braveMaxResults
		}
		values.Set("count", strconv.Itoa(count))
	}
	if c.defaultOptions.Country != "" {
		values.Set("country", c.defaultOptions.Country)
	}
	if c.defaultOptions.SearchLanguage != "" {
		values.Set("search_lang", c.defaultOptions.SearchLanguage)
	}
	if c.defaultOptions.UILanguage != "" {
		values.Set("ui_lang", c.defaultOptions.UILanguage)
	}
	freshness, err := braveFreshness(c.defaultOptions)
	if err != nil {
		return "", err
	}
	if freshness != "" {
		values.Set("freshness", freshness)
	}
	if c.defaultOptions.SafeSearch != "" {
		values.Set("safesearch", c.defaultOptions.SafeSearch)
	}
	if c.defaultOptions.Spellcheck != nil {
		values.Set("spellcheck", strconv.FormatBool(*c.defaultOptions.Spellcheck))
	}
	if c.defaultOptions.ResultFilter != "" {
		values.Set("result_filter", c.defaultOptions.ResultFilter)
	}
	if len(c.defaultOptions.Goggles) > 0 {
		values.Del("goggles")
		values["goggles"] = append([]string(nil), c.defaultOptions.Goggles...)
	}
	if c.defaultOptions.ExtraSnippets {
		values.Set("extra_snippets", "true")
	}
	if c.defaultOptions.Units != "" {
		values.Set("units", c.defaultOptions.Units)
	}
	if options.PageToken != "" {
		offset, parseErr := strconv.Atoi(options.PageToken)
		if parseErr != nil || offset < 0 || offset > braveMaxOffset {
			return "", errors.New("brave search page token must be an offset from 0 to 9")
		}
		values.Set("offset", strconv.Itoa(offset))
	}
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

type braveResponse struct {
	ID    string `json:"id"`
	Query struct {
		MoreResultsAvailable bool `json:"more_results_available"`
	} `json:"query"`
	Web struct {
		Results []braveResult `json:"results"`
	} `json:"web"`
}

type braveResult struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Description   string   `json:"description"`
	PageAge       string   `json:"page_age"`
	ExtraSnippets []string `json:"extra_snippets"`
	Profile       struct {
		LongName string `json:"long_name"`
	} `json:"profile"`
	MetaURL struct {
		Favicon string `json:"favicon"`
	} `json:"meta_url"`
}

func decodeBraveResponse(body []byte) (SearchResponse, error) {
	var response braveResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return SearchResponse{}, fmt.Errorf("decode brave search response: %w", err)
	}
	results := make([]Result, 0, len(response.Web.Results))
	for _, item := range response.Web.Results {
		results = append(results, Result{
			ID:            item.ID,
			Title:         item.Title,
			URL:           item.URL,
			Snippet:       item.Description,
			Author:        item.Profile.LongName,
			Favicon:       item.MetaURL.Favicon,
			ExtraSnippets: append([]string(nil), item.ExtraSnippets...),
			PageAge:       item.PageAge,
		})
	}
	return SearchResponse{Results: results, HasMore: response.Query.MoreResultsAvailable, RequestID: response.ID}, nil
}

func braveFreshness(options BraveOptions) (string, error) {
	if options.Freshness != "" {
		return options.Freshness, nil
	}
	start, err := normalizeBraveDate(options.StartPublishedAt)
	if err != nil {
		return "", fmt.Errorf("start_published_at: %w", err)
	}
	end, err := normalizeBraveDate(options.EndPublishedAt)
	if err != nil {
		return "", fmt.Errorf("end_published_at: %w", err)
	}
	if start == "" && end == "" {
		return "", nil
	}
	if start == "" {
		start = end
	}
	if end == "" {
		end = start
	}
	return start + "to" + end, nil
}

func normalizeBraveDate(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return parsed.Format("2006-01-02"), nil
		}
	}
	return "", errors.New("must be an ISO-8601 date or timestamp")
}

func cloneBraveOptions(options BraveOptions) BraveOptions {
	options.Goggles = append([]string(nil), options.Goggles...)
	return options
}
