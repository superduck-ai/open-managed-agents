package networkpolicy

import (
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

var errAllowedHost = errors.New("config.networking.allowed_hosts entries must be hostnames without URL schemes")

// lookupProfile 使用与浏览器兼容的 IDNA lookup 映射；后续再由 vendored 的
// Kubernetes RFC 1123 validator（dns1123.go）检查 DNS 语法与长度。
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
// host/port 拆分先执行，DNS 语法再交给 Ingress 同款 Kubernetes validator
// （vendored 于 dns1123.go）。
func ValidateAllowedHost(entry string) error {
	_, err := parseAllowedHost(entry)
	return err
}

// AllowsHost reports whether host:port matches any allowed_hosts entry using
// Environment networking hostname/wildcard semantics. An empty list never matches.
// Invalid entries fail closed.
func AllowsHost(allowedHosts []string, host string, port string) (bool, error) {
	entries, err := parseConfigAllowedHosts(allowedHosts)
	if err != nil {
		return false, err
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return false, nil
	}
	if !validAllowedPort(port) {
		return false, nil
	}
	return newHostMatcher(entries).match(normalized, port), nil
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
	if wildcard && len(isWildcardDNS1123Subdomain("*."+normalized)) != 0 {
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
	if problems := isDNS1123Subdomain(ascii); len(problems) != 0 {
		return "", errors.New(strings.Join(problems, "; "))
	}
	return ascii, nil
}

// hostMatcher 是按 DNS label 倒序存储的 trie。例如 api.example.com 的路径是
// root → com → example → api，这样遍历到 example 节点时即可判断
// *.example.com。索引只在 newHostMatcher 构造阶段写入，构造完成后可供多个
// goroutine 并发读取。
type hostMatcher struct {
	root *hostMatcherNode
}

// 每个节点的 children map 只表达下一层域名 label，保留完整的父子路径；不能按
// 整棵树的深度共用 map，否则 example.com 与 example.org 下的同名子节点会冲突。
// rules 只存在于一条规则的终止节点：key 为空表示任意有效端口，非空 key 表示
// 指定端口；value 分开标记 apex exact 与子域 wildcard，避免二者借用彼此的端口。
// 例如 example.com:443、*.example.com:8443、api.example.com:9443 会编译为：
//
//	root
//	└── children["com"]
//	    └── children["example"]
//	        ├── rules["443"]  = exact
//	        ├── rules["8443"] = wildcard
//	        └── children["api"]
//	            └── rules["9443"] = exact
type hostMatcherNode struct {
	children map[string]*hostMatcherNode
	rules    map[string]hostMatchRule
}

type hostMatchRule struct {
	exact    bool
	wildcard bool
}

func newHostMatcher(entries []allowedHost) hostMatcher {
	matcher := hostMatcher{root: newHostMatcherNode()}
	for _, entry := range entries {
		matcher.add(entry)
	}
	return matcher
}

func (m hostMatcher) match(host string, port string) bool {
	node := m.root
	if node == nil {
		return false
	}
	labels := strings.Split(host, ".")
	for index := len(labels) - 1; index >= 0; index-- {
		next, ok := node.children[labels[index]]
		if !ok {
			return false
		}
		node = next
		// 仍有 label 未消费时，当前节点只允许匹配 wildcard；因此
		// *.example.com 可以匹配 api.example.com，但不会匹配 apex example.com。
		if index > 0 && node.matchesWildcard(port) {
			return true
		}
	}
	// 所有 label 都已消费，只有终止节点的 exact 规则可以匹配 apex。
	return node.matchesExact(port)
}

func newHostMatcherNode() *hostMatcherNode {
	return &hostMatcherNode{children: map[string]*hostMatcherNode{}}
}

func (m hostMatcher) add(entry allowedHost) {
	node := m.root
	labels := strings.Split(entry.host, ".")
	for index := len(labels) - 1; index >= 0; index-- {
		label := labels[index]
		next, ok := node.children[label]
		if !ok {
			next = newHostMatcherNode()
			node.children[label] = next
		}
		node = next
	}
	if node.rules == nil {
		node.rules = map[string]hostMatchRule{}
	}
	rule := node.rules[entry.port]
	if entry.wildcard {
		rule.wildcard = true
	} else {
		rule.exact = true
	}
	node.rules[entry.port] = rule
}

func (n *hostMatcherNode) matchesExact(port string) bool {
	return n.rules[""].exact || n.rules[port].exact
}

func (n *hostMatcherNode) matchesWildcard(port string) bool {
	return n.rules[""].wildcard || n.rules[port].wildcard
}
