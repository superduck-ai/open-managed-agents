package networkpolicy

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
	validation "k8s.io/apimachinery/pkg/util/validation"
)

var errAllowedHost = errors.New("config.networking.allowed_hosts entries must be hostnames without URL schemes")

// lookupProfile keeps browser-compatible IDNA lookup mapping. DNS syntax and
// length checks are delegated to Kubernetes' RFC 1123 validators afterward.
var lookupProfile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.CheckHyphens(false))

type parsedAllowedHost struct {
	host     string
	port     string
	wildcard bool
}

// ValidateAllowedHost validates the public allowed_hosts contract. IP parsing,
// IDNA mapping, and host/port splitting happen before DNS syntax is delegated
// to the Kubernetes validators used for Ingress hostnames.
func ValidateAllowedHost(entry string) error {
	_, err := parseAllowedHost(entry)
	return err
}

func parseAllowedHost(entry string) (parsedAllowedHost, error) {
	if strings.Contains(entry, "://") || strings.Contains(entry, "/") {
		return parsedAllowedHost{}, errAllowedHost
	}
	host, port, err := splitAllowedHost(entry)
	if err != nil || !validAllowedPort(port) {
		return parsedAllowedHost{}, errAllowedHost
	}
	wildcard := strings.HasPrefix(host, "*.")
	if wildcard {
		host = strings.TrimPrefix(host, "*.")
	}
	if strings.Contains(host, "*") {
		return parsedAllowedHost{}, errAllowedHost
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return parsedAllowedHost{}, errAllowedHost
	}
	if wildcard && net.ParseIP(normalized) != nil {
		return parsedAllowedHost{}, errAllowedHost
	}
	if wildcard && len(validation.IsWildcardDNS1123Subdomain("*."+normalized)) != 0 {
		return parsedAllowedHost{}, errAllowedHost
	}
	return parsedAllowedHost{host: normalized, port: port, wildcard: wildcard}, nil
}

func splitAllowedHost(entry string) (string, string, error) {
	if entry == "" || entry != strings.TrimSpace(entry) {
		return "", "", errAllowedHost
	}
	if addr, err := netip.ParseAddr(entry); err == nil {
		if addr.Zone() != "" {
			return "", "", errAllowedHost
		}
		return addr.Unmap().String(), "", nil
	}
	if strings.HasPrefix(entry, "[") {
		host, port, err := net.SplitHostPort(entry)
		if err != nil {
			return "", "", err
		}
		addr, err := netip.ParseAddr(host)
		if err != nil || addr.Zone() != "" {
			return "", "", errAllowedHost
		}
		return addr.Unmap().String(), port, nil
	}
	if strings.Count(entry, ":") == 1 {
		host, port, err := net.SplitHostPort(entry)
		if err != nil {
			return "", "", err
		}
		return host, port, nil
	}
	if strings.Contains(entry, ":") {
		return "", "", errAllowedHost
	}
	return entry, "", nil
}

func validAllowedPort(port string) bool {
	if port == "" {
		return true
	}
	value, err := strconv.Atoi(port)
	return err == nil && value >= 1 && value <= 65535
}

// NormalizeHost applies the same canonicalization to allowlist entries,
// metadata, MCP URLs, and CONNECT targets.
func NormalizeHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return "", errors.New("empty host")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return "", errors.New("zoned IP literals are not supported")
		}
		return addr.Unmap().String(), nil
	}
	ascii, err := lookupProfile.ToASCII(host)
	if err != nil {
		return "", err
	}
	if problems := validation.IsDNS1123Subdomain(ascii); len(problems) != 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	return ascii, nil
}

type hostMatcher struct {
	exact        map[string]struct{}
	wildcardRoot *hostMatcherNode
}

type hostMatcherNode struct {
	children map[string]*hostMatcherNode
	wildcard bool
}

func newHostMatcher(entries []string) (hostMatcher, error) {
	matcher := hostMatcher{
		exact:        make(map[string]struct{}, len(entries)),
		wildcardRoot: &hostMatcherNode{children: map[string]*hostMatcherNode{}},
	}
	for _, entry := range entries {
		parsed, err := parseAllowedHost(entry)
		if err != nil {
			return hostMatcher{}, err
		}
		if parsed.port != "" && parsed.port != "443" {
			continue
		}
		if parsed.wildcard {
			matcher.addWildcard(parsed.host)
			continue
		}
		matcher.exact[parsed.host] = struct{}{}
	}
	return matcher, nil
}

func (m hostMatcher) match(host string) bool {
	if _, ok := m.exact[host]; ok {
		return true
	}
	labels := strings.Split(host, ".")
	node := m.wildcardRoot
	for index := len(labels) - 1; index >= 0; index-- {
		next, ok := node.children[labels[index]]
		if !ok {
			return false
		}
		node = next
		if node.wildcard && index > 0 {
			return true
		}
	}
	return false
}

func (m hostMatcher) addWildcard(host string) {
	node := m.wildcardRoot
	labels := strings.Split(host, ".")
	for index := len(labels) - 1; index >= 0; index-- {
		label := labels[index]
		next, ok := node.children[label]
		if !ok {
			next = &hostMatcherNode{children: map[string]*hostMatcherNode{}}
			node.children[label] = next
		}
		node = next
	}
	node.wildcard = true
}
