package networkpolicy

import (
	"net"

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
)

// Reason is the machine-readable authorization result used in audit logs.
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

// Decision is the authorization result for one normalized HTTPS target.
type Decision struct {
	Allow  bool
	Reason Reason
	Host   string
}

// Policy is a parsed and compiled Environment network policy. Raw JSON is
// confined to ParsePolicy; authorization only operates on typed state.
type Policy struct {
	policyType           Type
	explicitHosts        hostMatcher
	mcpHosts             map[string]struct{}
	allowPackageManagers bool
}

// ParsePolicy parses database JSON at the policy-loading boundary and compiles
// normalized host indexes. A malformed enabled source invalidates the policy.
func ParsePolicy(configRaw, agentSnapshotRaw []byte) (Policy, error) {
	config, err := ParseConfig(configRaw)
	if err != nil {
		return Policy{}, err
	}
	policy := Policy{policyType: config.Type}
	if config.Type == TypeUnrestricted {
		return policy, nil
	}
	policy.explicitHosts, err = newHostMatcher(config.AllowedHosts)
	if err != nil {
		return Policy{}, err
	}
	policy.allowPackageManagers = config.AllowPackageManagers
	policy.mcpHosts = map[string]struct{}{}
	if config.AllowMCPServers {
		hosts, err := MCPAllowedHosts(agentSnapshotRaw)
		if err != nil {
			return Policy{}, err
		}
		policy.mcpHosts = collections.StringSet(hosts)
	}
	return policy, nil
}

// AuthorizeHTTPS authorizes a target in host:443 form. SSRF, public-address,
// and DNS-rebinding checks remain the responsibility of the proxy dialer.
func (p Policy) AuthorizeHTTPS(target string) Decision {
	host, port, err := net.SplitHostPort(target)
	if err != nil || port != "443" {
		return Decision{Reason: ReasonInvalidTarget}
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return Decision{Reason: ReasonInvalidTarget}
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
