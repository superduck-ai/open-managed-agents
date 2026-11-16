package portgateway

import (
	"net"
	"net/http"
	"net/textproto"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

const (
	referrerPolicyHeader = "Referrer-Policy"
	referrerPolicy       = "no-referrer"
)

var hopByHopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

var websocketHandshakeHeaders = []string{
	"Sec-Websocket-Key", "Sec-Websocket-Version", "Sec-Websocket-Extensions", "Sec-Websocket-Protocol", "Sec-Websocket-Accept",
}

func removeHopByHop(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if name := strings.TrimSpace(token); name != "" {
				header.Del(name)
			}
		}
	}
	for _, name := range hopByHopHeaders {
		header.Del(name)
	}
}

func forwardRequestHeaders(r *http.Request, stripCookies []string, websocket bool) []porttunnel.Header {
	header := r.Header.Clone()
	removeHopByHop(header)
	header.Del("Host")
	if websocket {
		for _, name := range websocketHandshakeHeaders {
			header.Del(name)
		}
	}
	filterCookies(header, stripCookies)
	appendForwarded(header, r)
	return flattenHeaders(header)
}

func filterCookies(header http.Header, stripCookies []string) {
	values := header.Values("Cookie")
	header.Del("Cookie")
	var kept []string
	for _, value := range values {
		for _, pair := range strings.Split(value, ";") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			name, _, _ := strings.Cut(pair, "=")
			if slices.Contains(stripCookies, strings.TrimSpace(name)) {
				continue
			}
			kept = append(kept, pair)
		}
	}
	if len(kept) > 0 {
		header.Set("Cookie", strings.Join(kept, "; "))
	}
}

func appendForwarded(header http.Header, r *http.Request) {
	if client, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if prior := header.Get("X-Forwarded-For"); prior != "" {
			client = prior + ", " + client
		}
		header.Set("X-Forwarded-For", client)
	}
	if header.Get("X-Forwarded-Proto") == "" {
		proto := "http"
		if r.TLS != nil {
			proto = "https"
		}
		header.Set("X-Forwarded-Proto", proto)
	}
	header.Set("X-Forwarded-Host", r.Host)
}

func flattenHeaders(header http.Header) []porttunnel.Header {
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	slices.Sort(names)
	var flattened []porttunnel.Header
	for _, name := range names {
		for _, value := range header[name] {
			flattened = append(flattened, porttunnel.Header{Name: name, Value: value})
		}
	}
	return flattened
}

func applyResponseHeaders(destination http.Header, source []porttunnel.Header) {
	header := make(http.Header, len(source))
	for _, item := range source {
		if item.Name == "" {
			continue
		}
		header.Add(item.Name, item.Value)
	}
	removeHopByHop(header)
	for name, values := range header {
		canonical := textproto.CanonicalMIMEHeaderKey(name)
		for _, value := range values {
			if canonical == "Set-Cookie" {
				value = stripCookieDomain(value)
			}
			destination.Add(canonical, value)
		}
	}
	destination.Set(referrerPolicyHeader, referrerPolicy)
}

func stripCookieDomain(value string) string {
	parts := strings.Split(value, ";")
	kept := parts[:1]
	for _, part := range parts[1:] {
		name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
		if strings.EqualFold(strings.TrimSpace(name), "domain") {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, ";")
}
