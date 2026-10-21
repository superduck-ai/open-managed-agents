package webtools

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const maxFetchBytes = 5_000_000

type fetchPage struct {
	FetchResponse
	FullContent string `json:"full_content,omitempty"`
}

func (c *Client) Fetch(ctx context.Context, input FetchRequest, policy Policy) (fetchPage, error) {
	var page fetchPage
	target, err := publicURL(strings.TrimSpace(input.URL))
	if err != nil {
		return page, err
	}
	if len(input.URL) > 8192 {
		return page, invalidRequest("url exceeds 8192 bytes")
	}
	if !policy.Allows(target) {
		return page, denied("url_not_allowed")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return publicDial(ctx, network, address, net.DefaultResolver)
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return denied("WebFetch redirect limit exceeded")
		}
		u, err := publicURL(r.URL.String())
		if err != nil {
			return err
		}
		if !policy.Allows(u) {
			return denied("url_not_allowed")
		}
		return nil
	}}
	if c != nil && c.fetchHTTP != nil {
		client.Transport = c.fetchHTTP
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return page, invalidRequest("Invalid URL")
	}
	request.Header.Set("User-Agent", "OMA-WebFetch/1.0")
	request.Header.Set("Accept", "text/html, text/plain, application/json, application/xml")
	response, err := client.Do(request)
	if err != nil {
		return page, fetchFailure(err)
	}
	defer response.Body.Close()
	if response.ContentLength > maxFetchBytes {
		return page, invalidRequest("WebFetch response exceeds 5000000 bytes")
	}
	kind := strings.ToLower(strings.Split(response.Header.Get("Content-Type"), ";")[0])
	if !(strings.HasPrefix(kind, "text/") || kind == "application/json" || kind == "application/xml" || kind == "application/xhtml+xml") {
		return page, invalidRequest("Unsupported WebFetch content type")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxFetchBytes+1))
	if err != nil {
		return page, fetchFailure(err)
	}
	if len(raw) > maxFetchBytes {
		return page, invalidRequest("WebFetch response exceeds 5000000 bytes")
	}
	reader, err := charset.NewReader(strings.NewReader(string(raw)), response.Header.Get("Content-Type"))
	if err != nil {
		return page, invalidResponse()
	}
	decoded, err := io.ReadAll(io.LimitReader(reader, maxFetchBytes+1))
	if err != nil || len(decoded) > maxFetchBytes {
		return page, invalidResponse()
	}
	content := string(decoded)
	title := ""
	if kind == "text/html" || kind == "application/xhtml+xml" {
		title, content = renderHTML(content)
	}
	budget := 20000
	for _, config := range policy.Tools {
		if config.MaxContentTokens != nil {
			budget = *config.MaxContentTokens
		}
	}
	page = fetchPage{URL: response.Request.URL.String(), StatusCode: response.StatusCode, Title: title, Content: content}
	if len(content) > budget {
		page.FullContent = content
		end := budget
		for end > 0 && !utf8.RuneStart(content[end]) {
			end--
		}
		page.Content = content[:end]
		page.Truncated = true
	}
	return page, nil
}

func publicDial(ctx context.Context, network, address string, resolver *net.Resolver) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, denied("WebFetch target has no public address")
	}
	for _, ip := range addresses {
		if ip.Zone() != "" || !networkpolicy.PublicAddress(ip) || blockedTranslatedAddress(ip) {
			return nil, denied("WebFetch target is not public")
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	var failure error
	for _, ip := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		failure = err
	}
	return nil, failure
}

func blockedTranslatedAddress(ip netip.Addr) bool {
	if !netip.MustParsePrefix("64:ff9b::/96").Contains(ip) {
		return false
	}
	bytes := ip.As16()
	return !networkpolicy.PublicAddress(netip.AddrFrom4([4]byte{bytes[12], bytes[13], bytes[14], bytes[15]}))
}

func renderHTML(source string) (string, string) {
	tokenizer := html.NewTokenizer(strings.NewReader(source))
	var body, title strings.Builder
	hidden := 0
	inTitle := false
	pre := false
	for {
		token := tokenizer.Next()
		if token == html.ErrorToken {
			break
		}
		if token == html.StartTagToken || token == html.EndTagToken || token == html.SelfClosingTagToken {
			item := tokenizer.Token()
			name := item.Data
			if name == "title" {
				inTitle = token == html.StartTagToken
				continue
			}
			if slices.Contains([]string{"script", "style", "noscript", "template", "iframe"}, name) {
				if token == html.StartTagToken {
					hidden++
				}
				if token == html.EndTagToken && hidden > 0 {
					hidden--
				}
				continue
			}
			if hidden > 0 {
				continue
			}
			if name == "pre" {
				pre = token == html.StartTagToken
				body.WriteString("\n```\n")
			}
			if slices.Contains([]string{"p", "div", "br", "section", "article", "tr", "ul", "ol"}, name) {
				body.WriteString("\n")
			}
			if token == html.StartTagToken {
				switch name {
				case "h1", "h2", "h3", "h4", "h5", "h6":
					body.WriteString("\n" + strings.Repeat("#", int(name[1]-'0')) + " ")
				case "li":
					body.WriteString("\n- ")
				}
			}
			continue
		}
		if token == html.TextToken && hidden == 0 {
			text := string(tokenizer.Text())
			if inTitle {
				title.WriteString(text)
				continue
			}
			if !pre {
				text = strings.Join(strings.Fields(text), " ")
				if text != "" {
					text += " "
				}
			}
			body.WriteString(text)
		}
	}
	return strings.TrimSpace(title.String()), strings.TrimSpace(body.String())
}
