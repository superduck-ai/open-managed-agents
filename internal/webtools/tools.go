package webtools

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

func (c *Client) Search(ctx context.Context, input SearchRequest, policy Policy) (SearchResponse, error) {
	response := SearchResponse{Sources: []Source{}}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" || utf8.RuneCountInString(input.Query) > 128 {
		return response, invalidRequest("query must contain 1–128 Unicode characters")
	}
	if !policy.hasDomainIntersection() {
		return response, denied("url_not_allowed")
	}
	if c == nil || c.search == nil {
		return response, unavailable()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := c.search.Search(ctx, input)
	if err != nil {
		return response, err
	}
	if result.Sources == nil || len(result.Sources) > 50 {
		return response, invalidResponse()
	}
	for _, item := range result.Sources {
		target, err := publicURL(item.URL)
		if err == nil && policy.Allows(target) {
			response.Sources = append(response.Sources, item)
		}
	}
	return response, nil
}
