package networkpolicy

import (
	"net"

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

// Decision 是单个已归一化 HTTPS target 的授权结果。
type Decision struct {
	Allow  bool
	Reason Reason
	Host   string
}

// Policy 是解析并编译后的 Environment 网络策略。原始 JSON 只存在于
// ParsePolicy 边界，授权逻辑仅处理类型化状态。
type Policy struct {
	policyType           Type
	explicitHosts        hostMatcher
	mcpHosts             map[string]struct{}
	allowPackageManagers bool
}

// ParsePolicy 在策略加载边界解析数据库 JSON，并编译归一化的 host 索引。
// 任一已启用的策略来源格式错误，都会使整份策略失效。
func ParsePolicy(configRaw, agentSnapshotRaw []byte) (Policy, error) {
	config, err := ParseConfig(configRaw)
	if err != nil {
		return Policy{}, err
	}
	policy := Policy{policyType: config.Type}
	if config.Type == TypeUnrestricted {
		return policy, nil
	}
	policy.explicitHosts = newHostMatcher(config.allowedHosts)
	policy.allowPackageManagers = config.AllowPackageManagers
	if config.AllowMCPServers {
		hosts, err := MCPAllowedHosts(agentSnapshotRaw)
		if err != nil {
			return Policy{}, err
		}
		policy.mcpHosts = collections.StringSet(hosts)
	}
	return policy, nil
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
	if p.policyType != TypeUnrestricted && p.policyType != TypeLimited {
		return Decision{Reason: ReasonPolicyUnavailable, Host: normalized}
	}
	if p.policyType == TypeUnrestricted {
		return Decision{Allow: true, Reason: ReasonUnrestricted, Host: normalized}
	}
	if p.explicitHosts.match(normalized) {
		return Decision{Allow: true, Reason: ReasonExplicitHost, Host: normalized}
	}
	if _, ok := p.mcpHosts[normalized]; ok {
		return Decision{Allow: true, Reason: ReasonMCPHost, Host: normalized}
	}
	if p.allowPackageManagers && isPackageManagerHost(normalized) {
		return Decision{Allow: true, Reason: ReasonPackageManagerHost, Host: normalized}
	}
	return Decision{Reason: ReasonHostNotAllowed, Host: normalized}
}
