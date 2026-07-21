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

// lookupProfile 使用与浏览器兼容的 IDNA lookup 映射；后续再由 Kubernetes
// RFC 1123 validator 检查 DNS 语法与长度。
var lookupProfile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.CheckHyphens(false))

// allowedHost 是已经校验并归一化的 Environment allowlist 条目。
type allowedHost struct {
	host     string
	port     string
	wildcard bool
}

// pattern 返回 Sandbox provider 使用的 canonical host pattern。
func (h allowedHost) pattern() string {
	host := h.host
	if h.wildcard {
		host = "*." + host
	}
	if h.port == "" {
		return host
	}
	if strings.Contains(host, ":") {
		return net.JoinHostPort(host, h.port)
	}
	return host + ":" + h.port
}

// ValidateAllowedHost 校验公开的 allowed_hosts 合同。IP 解析、IDNA 映射与
// host/port 拆分先执行，DNS 语法再交给 Ingress 同款 Kubernetes validator。
func ValidateAllowedHost(entry string) error {
	_, err := parseAllowedHost(entry)
	return err
}

func parseAllowedHost(entry string) (allowedHost, error) {
	if strings.Contains(entry, "://") || strings.Contains(entry, "/") {
		return allowedHost{}, errAllowedHost
	}
	host, port, err := splitAllowedHost(entry)
	if err != nil || !validAllowedPort(port) {
		return allowedHost{}, errAllowedHost
	}
	wildcard := strings.HasPrefix(host, "*.")
	if wildcard {
		host = strings.TrimPrefix(host, "*.")
	}
	if strings.Contains(host, "*") {
		return allowedHost{}, errAllowedHost
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return allowedHost{}, errAllowedHost
	}
	if wildcard && net.ParseIP(normalized) != nil {
		return allowedHost{}, errAllowedHost
	}
	if wildcard && len(validation.IsWildcardDNS1123Subdomain("*."+normalized)) != 0 {
		return allowedHost{}, errAllowedHost
	}
	return allowedHost{host: normalized, port: port, wildcard: wildcard}, nil
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

// NormalizeHost 对 allowlist、metadata、MCP URL 与 CONNECT target 使用同一套
// canonicalization 规则。
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

// hostMatcher 是编译后的 allowlist 只读索引：只在 newHostMatcher 构造阶段
// 写入，构造完成后可供多个 goroutine 并发读取。
type hostMatcher struct {
	exact        map[string]struct{}
	wildcardRoot *hostMatcherNode
}

type hostMatcherNode struct {
	children map[string]*hostMatcherNode
	wildcard bool
}

func newHostMatcher(entries []allowedHost) hostMatcher {
	matcher := hostMatcher{
		exact:        make(map[string]struct{}, len(entries)),
		wildcardRoot: &hostMatcherNode{children: map[string]*hostMatcherNode{}},
	}
	for _, entry := range entries {
		if entry.port != "" && entry.port != "443" {
			continue
		}
		if entry.wildcard {
			matcher.addWildcard(entry.host)
			continue
		}
		matcher.exact[entry.host] = struct{}{}
	}
	return matcher
}

func (m hostMatcher) match(host string) bool {
	if _, ok := m.exact[host]; ok {
		return true
	}
	labels := strings.Split(host, ".")
	node := m.wildcardRoot
	if node == nil {
		return false
	}
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
