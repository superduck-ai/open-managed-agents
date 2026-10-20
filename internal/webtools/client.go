package webtools

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"
)

type Client struct {
	search    SearchProvider
	fetchHTTP http.RoundTripper
	resolver  *net.Resolver
}

func Load(keyFile, dnsResolver string) (*Client, error) {
	client := NewClient(nil, nil)
	if keyFile != "" {
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, invalidCredentialFile()
		}
		key := strings.TrimSpace(string(raw))
		if key == "" || strings.ContainsAny(key, "\r\n\t ") {
			return nil, invalidCredentialFile()
		}
		client.search = NewDeepSeekSearch(key, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }})
	}
	if dnsResolver != "" {
		host, port, err := net.SplitHostPort(dnsResolver)
		ip, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || !ip.IsValid() || port != "53" {
			return nil, invalidConfig("web_tools.dns_resolver must be an IP:53 DNS server address")
		}
		client.resolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", dnsResolver)
		}}
	}
	return client, nil
}

func NewClient(search SearchProvider, fetchTransport http.RoundTripper) *Client {
	return &Client{search: search, fetchHTTP: fetchTransport}
}
