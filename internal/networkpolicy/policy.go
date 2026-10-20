package networkpolicy

import (
	"net"
	"net/url"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
)

// Reason 是审计日志使用的机器可读授权结果。
type Reason string

const (
	ReasonUnrestricted       Reason = "unrestricted"
	ReasonExplicitHost       Reason = "explicit_host"
	ReasonMCPHost            Reason = "mcp_host"
	ReasonPackageManagerHost Reason = "package_manager_host"
	ReasonHostNotAllowed     Reason = "host_not_allowed"
	ReasonInvalidTarget      Reason = "invalid_target"
	ReasonPolicyUnavailable  Reason = "policy_unavailable"
)

// Decision 是单个已归一化网络 target 的授权结果。
type Decision struct {
	Allow  bool
	Reason Reason
	Host   string
}

// Policy 是解析并编译后的 Environment 网络策略。原始 JSON 只存在于策略
// 解析边界，授权逻辑仅处理类型化状态。
type Policy struct {
	policyType           Type
	explicitHosts        hostMatcher
	mcpHosts             map[string]struct{}
	allowPackageManagers bool
	packageHosts         map[string]struct{}
}

// MCPProxyPolicy 是 MCP HTTP proxy 的编译后授权策略，同时约束 Session
// 配置的精确 URL 与 Environment 网络策略。
type MCPProxyPolicy struct {
	policy        Policy
	mcpURLSet     map[string]struct{}
	mcpURLsByName map[string]string
}

// ParsePolicy 在策略加载边界解析数据库 JSON，并编译归一化的 host 索引。
// 任一已启用的策略来源格式错误，都会使整份策略失效。
func ParsePolicy(configRaw, agentSnapshotRaw []byte, packageHosts ...string) (Policy, error) {
	config, err := ParseConfig(configRaw)
	if err != nil {
		return Policy{}, err
	}
	var mcpHosts []string
	if config.AllowMCPServers {
		hosts, err := MCPAllowedHosts(agentSnapshotRaw)
		if err != nil {
			return Policy{}, err
		}
		mcpHosts = hosts
	}
	policy := compilePolicy(config, mcpHosts)
	policy.packageHosts = collections.StringSet(packageHosts)
	return policy, nil
}

// ParseMCPProxyPolicy 在 MCP proxy 的策略加载边界一次性解析 Environment
// 配置与 Session Agent Snapshot，并编译精确 URL 集合和 host matcher。
func ParseMCPProxyPolicy(configRaw, agentSnapshotRaw []byte) (MCPProxyPolicy, error) {
	config, err := ParseConfig(configRaw)
	if err != nil {
		return MCPProxyPolicy{}, err
	}
	targets, err := parseMCPServerTargets(agentSnapshotRaw)
	if err != nil {
		return MCPProxyPolicy{}, err
	}
	var mcpHosts []string
	if config.AllowMCPServers {
		mcpHosts = targets.hosts
	}
	mcpURLSet := make(map[string]struct{}, len(targets.urls))
	for _, rawURL := range targets.urls {
		key, err := canonicalMCPURLKey(rawURL)
		if err != nil {
			return MCPProxyPolicy{}, err
		}
		mcpURLSet[key] = struct{}{}
	}
	return MCPProxyPolicy{
		policy:        compilePolicy(config, mcpHosts),
		mcpURLSet:     mcpURLSet,
		mcpURLsByName: targets.urlsByName,
	}, nil
}

// MCPServerURL resolves a remote MCP target by the stable server name stored in
// the Session Agent Snapshot. The runtime never supplies an arbitrary URL.
func (p MCPProxyPolicy) MCPServerURL(name string) (string, bool) {
	target, ok := p.mcpURLsByName[name]
	return target, ok
}

func compilePolicy(config Config, mcpHosts []string) Policy {
	policy := Policy{policyType: config.Type}
	if config.Type == TypeUnrestricted {
		return policy
	}
	policy.explicitHosts = newHostMatcher(config.allowedHosts)
	policy.allowPackageManagers = config.AllowPackageManagers
	policy.mcpHosts = collections.StringSet(mcpHosts)
	return policy
}

// AuthorizeHTTPS 授权 host:443 形式的 target。SSRF、公网地址与 DNS rebinding
// 检查仍由 proxy dialer 负责。
func (p Policy) AuthorizeHTTPS(target string) Decision {
	host, port, err := net.SplitHostPort(target)
	if err != nil || port != "443" {
		return Decision{Reason: ReasonInvalidTarget}
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return Decision{Reason: ReasonInvalidTarget}
	}
	return p.authorizeEndpoint(normalized, "https", port)
}

func (p Policy) AuthorizeHTTPURL(target *url.URL) Decision {
	if target == nil || target.Scheme != "http" || target.Hostname() == "" || target.User != nil || target.Fragment != "" || target.Opaque != "" {
		return Decision{Reason: ReasonInvalidTarget}
	}
	host, err := NormalizeHost(target.Hostname())
	if err != nil {
		return Decision{Reason: ReasonInvalidTarget}
	}
	port := target.Port()
	if port == "" {
		port = "80"
	}
	if !validAllowedPort(port) {
		return Decision{Reason: ReasonInvalidTarget}
	}
	return p.authorizeEndpoint(host, "http", port)
}

// AuthorizeMCPURL 授权实际 MCP HTTP(S) URL；URI 主机名表示可归一，实际目标与端口不得改写。
func (p MCPProxyPolicy) AuthorizeMCPURL(rawURL string) Decision {
	target, err := url.Parse(rawURL)
	if err != nil || !target.IsAbs() || target.Host == "" || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return Decision{Reason: ReasonInvalidTarget}
	}
	scheme := strings.ToLower(target.Scheme)
	if scheme != "http" && scheme != "https" {
		return Decision{Reason: ReasonInvalidTarget}
	}
	host, err := NormalizeHost(target.Hostname())
	if err != nil {
		return Decision{Reason: ReasonInvalidTarget}
	}
	key, err := canonicalMCPURLKey(rawURL)
	if err != nil {
		return Decision{Reason: ReasonInvalidTarget, Host: host}
	}
	if !p.matchesMCPTransportURL(key, target) {
		return Decision{Reason: ReasonHostNotAllowed, Host: host}
	}
	port := target.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	if !validAllowedPort(port) {
		return Decision{Reason: ReasonInvalidTarget, Host: host}
	}
	return p.policy.authorizeEndpoint(host, scheme, port)
}

func (p Policy) authorizeEndpoint(normalized string, scheme string, port string) Decision {
	if p.policyType != TypeUnrestricted && p.policyType != TypeLimited {
		return Decision{Reason: ReasonPolicyUnavailable, Host: normalized}
	}
	if p.policyType == TypeUnrestricted {
		return Decision{Allow: true, Reason: ReasonUnrestricted, Host: normalized}
	}
	if p.explicitHosts.match(normalized, port) {
		return Decision{Allow: true, Reason: ReasonExplicitHost, Host: normalized}
	}
	if _, ok := p.mcpHosts[normalized]; ok {
		return Decision{Allow: true, Reason: ReasonMCPHost, Host: normalized}
	}
	_, customPackageHost := p.packageHosts[normalized]
	if scheme == "https" && port == "443" && p.allowPackageManagers && (isPackageManagerHost(normalized) || customPackageHost) {
		return Decision{Allow: true, Reason: ReasonPackageManagerHost, Host: normalized}
	}
	return Decision{Reason: ReasonHostNotAllowed, Host: normalized}
}

func (p MCPProxyPolicy) matchesMCPTransportURL(key string, target *url.URL) bool {
	if _, ok := p.mcpURLSet[key]; ok {
		return true
	}
	query, err := url.ParseQuery(target.RawQuery)
	if err != nil || len(query["sessionid"]) != 1 || query.Get("sessionid") == "" {
		return false
	}
	parts := strings.Split(target.RawQuery, "&")
	kept := make([]string, 0, len(parts))
	removed := 0
	for _, part := range parts {
		if strings.HasPrefix(part, "sessionid=") {
			removed++
			continue
		}
		kept = append(kept, part)
	}
	if removed != 1 {
		return false
	}
	base := *target
	base.RawQuery = strings.Join(kept, "&")
	baseKey, err := canonicalMCPURLKey(base.String())
	if err != nil {
		return false
	}
	_, ok := p.mcpURLSet[baseKey]
	return ok
}
