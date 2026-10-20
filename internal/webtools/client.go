package webtools

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type Client struct {
	key  string
	http *http.Client
}

func Load(keyFile string) (*Client, error) {
	if keyFile == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, invalidCredentialFile()
	}
	key := strings.TrimSpace(string(raw))
	if key == "" || strings.ContainsAny(key, "\r\n\t ") {
		return nil, invalidCredentialFile()
	}
	return NewClient(key, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}), nil
}

func NewClient(key string, client *http.Client) *Client {
	return &Client{key: key, http: client}
}

func (c *Client) request(ctx context.Context, method string, input, output any) error {
	if c == nil {
		return unavailable()
	}
	body, err := json.Marshal(input)
	if err != nil {
		return invalidRequest("Invalid web tool input")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
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
