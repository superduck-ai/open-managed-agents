package webtools

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

type deepSeekSearch struct {
	key  string
	http *http.Client
}

func NewDeepSeekSearch(key string, client *http.Client) SearchProvider {
	return &deepSeekSearch{key: key, http: client}
}

func (c *deepSeekSearch) Search(ctx context.Context, input SearchRequest) (SearchResponse, error) {
	if c.key == "" {
		return SearchResponse{}, unavailable()
	}
	body, err := json.Marshal(struct {
		Query       string `json:"query"`
		Count       int    `json:"count"`
		NeedContent bool   `json:"need_content"`
	}{input.Query, 10, false})
	if err != nil {
		return SearchResponse{}, invalidRequest("Invalid web tool input")
	}
	for attempt := 0; ; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/tools/search", bytes.NewReader(body))
		if err != nil {
			return SearchResponse{}, requestFailed()
		}
		request.Header.Set("Authorization", "Bearer "+c.key)
		request.Header.Set("Content-Type", "application/json")
		response, err := c.http.Do(request)
		if err != nil {
			return SearchResponse{}, requestFailed()
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			if attempt == 0 && retryable(response.StatusCode) {
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return SearchResponse{}, requestFailed()
				case <-timer.C:
					continue
				}
			}
			return SearchResponse{}, upstreamFailure(response.StatusCode)
		}
		var result struct {
			Results []Source `json:"results"`
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		_ = response.Body.Close()
		if readErr != nil || len(raw) > 2<<20 || !utf8.Valid(raw) || json.Unmarshal(raw, &result) != nil {
			return SearchResponse{}, invalidResponse()
		}
		return SearchResponse{Sources: result.Results}, nil
	}
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}
