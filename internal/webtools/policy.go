package webtools

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

func publicURL(value string) (*url.URL, error) {
	target, err := url.Parse(value)
	if err != nil || target.User != nil || target.Opaque != "" || (target.Scheme != "http" && target.Scheme != "https") {
		return nil, invalidRequest("Only public HTTP(S) URLs without credentials are supported")
	}
	host := strings.ToLower(target.Hostname())
	_, lastLabel, _ := strings.CutLast(host, ".")
	_, numericLabel := strconv.Atoi(lastLabel)
	_, ipErr := netip.ParseAddr(host)
	if host == "" || ipErr == nil || numericLabel == nil || !strings.Contains(host, ".") || strings.HasSuffix(host, ".") ||
		(target.Port() != "" && target.Port() != "80" && target.Port() != "443") {
		return nil, invalidRequest("Only public hostnames on standard HTTP(S) ports are supported")
	}
	for _, suffix := range []string{"localhost", "local", "internal", "test", "invalid"} {
		if matchesDomain(host, suffix) {
			return nil, invalidRequest("Private web targets are not supported")
		}
	}
	if target.Hostname() != host {
		target.Host = strings.ToLower(target.Host)
	}
	return target, nil
}

func matchesDomain(host, domain string) bool {
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func permittedHost(host string, policy networkpolicy.Config) bool {
	if policy.Type == networkpolicy.TypeUnrestricted {
		return true
	}
	for _, pattern := range policy.AllowedHostPatterns() {
		if host == pattern || (strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:])) {
			return true
		}
	}
	return false
}
