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
	var result struct {
		Results []Source `json:"results"`
	}
	if err := c.request(ctx, "search", struct {
		Query       string `json:"query"`
		Count       int    `json:"count"`
		NeedContent bool   `json:"need_content"`
	}{input.Query, 10, false}, &result); err != nil {
		return SearchResponse{}, err
	}
	return SearchResponse{Sources: result.Results}, nil
}

func (c *deepSeekSearch) request(ctx context.Context, method string, input, output any) error {
	if c.key == "" {
		return unavailable()
	}
	body, err := json.Marshal(input)
	if err != nil {
		return invalidRequest("Invalid web tool input")
	}
	for attempt := range 2 {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/v1/tools/"+method, bytes.NewReader(body))
		if err != nil {
			return requestFailed()
		}
		request.Header.Set("Authorization", "Bearer "+c.key)
		request.Header.Set("Content-Type", "application/json")
		response, err := c.http.Do(request)
		if err != nil {
			return requestFailed()
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			if attempt == 0 && retryable(response.StatusCode) {
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return requestFailed()
				case <-timer.C:
					continue
				}
			}
			return upstreamFailure(response.StatusCode)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		_ = response.Body.Close()
		if readErr != nil || len(raw) > 2<<20 || !utf8.Valid(raw) || json.Unmarshal(raw, output) != nil {
			return invalidResponse()
		}
		return nil
	}
	return requestFailed()
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500
}
