package networkpolicy

import (
	"encoding/json"
	"net"
)

// Reason 是机器可测的授权结论，用于服务端审计日志；不下发给 Sandbox。
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

// Decision 是一次授权结论：是否放行、归一化 host 与 reason。
type Decision struct {
	Allow  bool
	Reason Reason
	Host   string
}

// Subject 是已认证的运行上下文。Config 为 Environment 当前配置 JSON，
// AgentSnapshot 为 Session 的 AgentSnapshot JSON；均由调用方按 workspace
// 作用域从数据库新鲜读取，绝不信任 Sandbox 提交的配置。
type Subject struct {
	Config        json.RawMessage
	AgentSnapshot json.RawMessage
}

// AuthorizeHTTPS 判断 limited/unrestricted 策略下 target（`host:443`）是否
// 允许经 proxy 出口。只接受 443 端口；SSRF、公网 IP 与 DNS rebinding 检查
// 由 proxy 保留，本函数不替代。
func AuthorizeHTTPS(subject Subject, target string) Decision {
	host, port, err := net.SplitHostPort(target)
	if err != nil || port != "443" {
		return Decision{Reason: ReasonInvalidTarget}
	}
	normalized, err := NormalizeHost(host)
	if err != nil {
		return Decision{Reason: ReasonInvalidTarget}
	}
	config, err := ParseConfig(subject.Config)
	if err != nil {
		return Decision{Reason: ReasonPolicyUnavailable, Host: normalized}
	}
	if config.Type == TypeUnrestricted {
		return Decision{Allow: true, Reason: ReasonUnrestricted, Host: normalized}
	}
	var mcpHosts []string
	if config.AllowMCPServers {
		mcpHosts, err = MCPAllowedHosts(subject.AgentSnapshot)
		if err != nil {
			return Decision{Reason: ReasonPolicyUnavailable, Host: normalized}
		}
	}
	for _, entry := range config.AllowedHosts {
		if matchAllowedEntry(entry, normalized) {
			return Decision{Allow: true, Reason: ReasonExplicitHost, Host: normalized}
		}
	}
	if config.AllowMCPServers {
		for _, mcpHost := range mcpHosts {
			if mcpHost == normalized {
				return Decision{Allow: true, Reason: ReasonMCPHost, Host: normalized}
			}
		}
	}
	if config.AllowPackageManagers {
		if isPackageManagerHost(normalized) {
			return Decision{Allow: true, Reason: ReasonPackageManagerHost, Host: normalized}
		}
	}
	return Decision{Reason: ReasonHostNotAllowed, Host: normalized}
}
