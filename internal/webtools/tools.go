package webtools

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

func (c *Client) Search(ctx context.Context, input SearchRequest, policy networkpolicy.Config) (SearchResponse, error) {
	response := SearchResponse{Results: []SearchResult{}}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" || utf8.RuneCountInString(input.Query) > 128 {
		return response, invalidRequest("query must contain 1–128 Unicode characters")
	}
	if input.Count != nil && (*input.Count < 1 || *input.Count > 50) {
		return response, invalidRequest("count must be between 1 and 50")
	}
	if input.OriginalQuestion != nil && (utf8.RuneCountInString(*input.OriginalQuestion) < 1 || utf8.RuneCountInString(*input.OriginalQuestion) > 128) {
		return response, invalidRequest("ori_question must contain 1–128 Unicode characters")
	}
	if policy.Type == networkpolicy.TypeLimited {
		var sites []string
		for _, pattern := range policy.AllowedHostPatterns() {
			sites = append(sites, "site:"+strings.TrimPrefix(pattern, "*."))
		}
		if len(sites) == 0 {
			return response, denied("WebSearch is denied by the Environment network policy")
		}
		input.Query += " " + strings.Join(sites, " OR ")
		if utf8.RuneCountInString(input.Query) > 128 {
			return response, invalidRequest("query including Environment host filters exceeds 128 Unicode characters")
		}
	}
	var result SearchResponse
	if err := c.request(ctx, "search", input, &result); err != nil {
		return response, err
	}
	if result.Results == nil || len(result.Results) > 50 {
		return response, invalidResponse()
	}
	for _, item := range result.Results {
		target, err := publicURL(item.URL)
		if err == nil && permittedHost(target.Hostname(), policy) {
			response.Results = append(response.Results, item)
		}
	}
	return response, nil
}

func (c *Client) Fetch(ctx context.Context, input FetchRequest, policy networkpolicy.Config) (FetchResponse, error) {
	var response FetchResponse
	if policy.Type != networkpolicy.TypeUnrestricted {
		return response, denied("WebFetch is unavailable for limited networking because the service does not expose redirect targets for policy verification")
	}
	input.URL = strings.TrimSpace(input.URL)
	if input.URL == "" || utf8.RuneCountInString(input.URL) > 8192 {
		return response, invalidRequest("url must contain 1–8192 Unicode characters")
	}
	if _, err := publicURL(input.URL); err != nil {
		return response, err
	}
	if err := c.request(ctx, "fetch", input, &response); err != nil {
		return response, err
	}
	if strings.TrimSpace(response.Content) == "" {
		return response, invalidResponse()
	}
	return response, nil
}
