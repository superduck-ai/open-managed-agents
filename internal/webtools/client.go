package webtools

import "net/http"

type Client struct {
	search    SearchProvider
	fetchHTTP http.RoundTripper
}

func NewClient(search SearchProvider, fetchTransport http.RoundTripper) *Client {
	return &Client{search: search, fetchHTTP: fetchTransport}
}
