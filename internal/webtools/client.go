package webtools

import (
	"net/http"
	"strings"
)

type Client struct {
	search    SearchProvider
	fetchHTTP http.RoundTripper
}

func Load(apiKey string) (*Client, error) {
	apiKey = strings.TrimSpace(apiKey)
	if strings.ContainsAny(apiKey, "\r\n\t ") {
		return nil, invalidConfig("web_tools.deepseek_api_key must not contain whitespace")
	}
	client := NewClient(nil, nil)
	if apiKey != "" {
		client.search = NewDeepSeekSearch(apiKey, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	}
	return client, nil
}

func NewClient(search SearchProvider, fetchTransport http.RoundTripper) *Client {
	return &Client{search: search, fetchHTTP: fetchTransport}
}
