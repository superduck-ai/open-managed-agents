package webtools

import (
	"net/url"
	"slices"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

type Policy struct {
	Environment networkpolicy.Config
	Tools       []ToolConfig
}

func (p Policy) Allows(target *url.URL) bool {
	port := target.Port()
	if port == "" {
		port = "443"
		if target.Scheme == "http" {
			port = "80"
		}
	}
	if p.Environment.Type == networkpolicy.TypeLimited {
		allowed, err := networkpolicy.AllowsHost(p.Environment.AllowedHostPatterns(), target.Hostname(), port)
		if err != nil || !allowed {
			return false
		}
	}
	for _, c := range p.Tools {
		if c.Enabled != nil && !*c.Enabled {
			return false
		}
		if c.AllowedDomains != nil && !matchesDomains(target, c.AllowedDomains) {
			return false
		}
		if matchesDomains(target, c.BlockedDomains) {
			return false
		}
	}
	return true
}

func matchesDomains(target *url.URL, domains []string) bool {
	for _, domain := range domains {
		host, path, err := parseDomainScope(domain)
		if err == nil && matchesDomain(target.Hostname(), host) && (path == "" || strings.HasPrefix(target.Path, "/"+path)) {
			return true
		}
	}
	return false
}

func (p Policy) hasDomainIntersection() bool {
	var domains []string
	for _, tool := range p.Tools {
		if tool.AllowedDomains == nil {
			continue
		}
		if domains == nil {
			domains = slices.Clone(tool.AllowedDomains)
			continue
		}
		next := []string{}
		for _, a := range domains {
			for _, b := range tool.AllowedDomains {
				if domain, ok := intersectDomains(a, b); ok && !slices.Contains(next, domain) {
					next = append(next, domain)
				}
			}
		}
		domains = next
	}
	return domains == nil || len(domains) > 0
}

func intersectDomains(a, b string) (string, bool) {
	ah, ap, err := parseDomainScope(a)
	if err != nil {
		return "", false
	}
	bh, bp, err := parseDomainScope(b)
	if err != nil {
		return "", false
	}
	host := ah
	if matchesDomain(bh, ah) {
		host = bh
	} else if !matchesDomain(ah, bh) {
		return "", false
	}
	path := ap
	if strings.HasPrefix(bp, ap) {
		path = bp
	} else if !strings.HasPrefix(ap, bp) {
		return "", false
	}
	if path != "" {
		host += (&url.URL{Path: "/" + path}).EscapedPath()
	}
	return host, true
}

func parseDomainScope(domain string) (string, string, error) {
	host, path, _ := strings.Cut(domain, "/")
	path, err := url.PathUnescape(path)
	return strings.ToLower(host), path, err
}
