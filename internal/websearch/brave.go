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
	defaultOptions SearchOptions
}

var _ Provider = (*BraveClient)(nil)

type BraveClientConfig struct {
	Endpoint string
	APIKey   string
	Timeout  time.Duration
	Options  SearchOptions
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
		defaultOptions: cloneSearchOptions(cfg.Options),
	}
}

func (c *BraveClient) Search(ctx context.Context, request SearchRequest) (SearchResponse, error) {
	if c == nil || c.apiKey == "" {
		return SearchResponse{}, errors.New("web search provider is not configured")
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return SearchResponse{}, errors.New("web search query is required")
	}
	options := mergeSearchOptions(c.defaultOptions, request.Options)
	endpoint, err := c.searchURL(query, options)
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
		if options.PageToken != "" {
			offset, err = strconv.Atoi(options.PageToken)
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
	if options.Country != "" {
		values.Set("country", options.Country)
	}
	if options.SearchLanguage != "" {
		values.Set("search_lang", options.SearchLanguage)
	}
	if options.UILanguage != "" {
		values.Set("ui_lang", options.UILanguage)
	}
	if freshness := braveFreshness(options); freshness != "" {
		values.Set("freshness", freshness)
	}
	if options.SafeSearch != "" {
		values.Set("safesearch", options.SafeSearch)
	}
	if options.Spellcheck != nil {
		values.Set("spellcheck", strconv.FormatBool(*options.Spellcheck))
	}
	if options.ResultFilter != "" {
		values.Set("result_filter", options.ResultFilter)
	}
	if len(options.Goggles) > 0 {
		values.Del("goggles")
		values["goggles"] = append([]string(nil), options.Goggles...)
	}
	if options.ExtraSnippets {
		values.Set("extra_snippets", "true")
	}
	if options.Units != "" {
		values.Set("units", options.Units)
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

func braveFreshness(options SearchOptions) string {
	if options.Freshness != "" {
		return options.Freshness
	}
	if options.StartPublishedAt.IsZero() && options.EndPublishedAt.IsZero() {
		return ""
	}
	start := options.StartPublishedAt.Format("2006-01-02")
	end := options.EndPublishedAt.Format("2006-01-02")
	if options.StartPublishedAt.IsZero() {
		start = end
	}
	if options.EndPublishedAt.IsZero() {
		end = start
	}
	return start + "to" + end
}

func mergeSearchOptions(defaults, request SearchOptions) SearchOptions {
	merged := cloneSearchOptions(defaults)
	if request.MaxResults != 0 {
		merged.MaxResults = request.MaxResults
	}
	if request.Country != "" {
		merged.Country = request.Country
	}
	if request.SearchLanguage != "" {
		merged.SearchLanguage = request.SearchLanguage
	}
	if request.UILanguage != "" {
		merged.UILanguage = request.UILanguage
	}
	if request.Freshness != "" {
		merged.Freshness = request.Freshness
	}
	if request.SafeSearch != "" {
		merged.SafeSearch = request.SafeSearch
	}
	if request.Spellcheck != nil {
		merged.Spellcheck = request.Spellcheck
	}
	if request.ResultFilter != "" {
		merged.ResultFilter = request.ResultFilter
	}
	if len(request.Goggles) > 0 {
		merged.Goggles = append([]string(nil), request.Goggles...)
	}
	if request.ExtraSnippets {
		merged.ExtraSnippets = true
	}
	if request.Units != "" {
		merged.Units = request.Units
	}
	if request.PageToken != "" {
		merged.PageToken = request.PageToken
	}
	if !request.StartPublishedAt.IsZero() {
		merged.StartPublishedAt = request.StartPublishedAt
	}
	if !request.EndPublishedAt.IsZero() {
		merged.EndPublishedAt = request.EndPublishedAt
	}
	if (!request.StartPublishedAt.IsZero() || !request.EndPublishedAt.IsZero()) && request.Freshness == "" {
		merged.Freshness = ""
	}
	return merged
}

func cloneSearchOptions(options SearchOptions) SearchOptions {
	options.IncludeDomains = append([]string(nil), options.IncludeDomains...)
	options.ExcludeDomains = append([]string(nil), options.ExcludeDomains...)
	options.Goggles = append([]string(nil), options.Goggles...)
	return options
}
