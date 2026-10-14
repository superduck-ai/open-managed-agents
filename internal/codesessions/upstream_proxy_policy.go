package codesessions

import (
	"context"
	"net"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

type proxyPolicyScope struct {
	organizationUUID      string
	workspaceUUID         string
	environmentExternalID string
}

// upstreamProxyPolicyContext 是一次 CONNECT 授权所需的编译后策略与审计作用域。
type upstreamProxyPolicyContext struct {
	policy networkpolicy.Policy
	proxyPolicyScope
}

// mcpProxyPolicyContext 是一次 MCP HTTP proxy 授权所需的编译后策略与审计作用域。
type mcpProxyPolicyContext struct {
	policy networkpolicy.MCPProxyPolicy
	proxyPolicyScope
}

// upstreamProxyIdentity 只由已验签的 session-ingress JWT claims 构造，作为
// Code Session 策略查询的可信租户作用域；不接受 relay 在 CONNECT 中提交作用域。
type upstreamProxyIdentity struct {
	codeSessionExternalID string
	organizationUUID      string
	workspaceUUID         string
}

// loadUpstreamProxyPolicyContext 按 Code Session → Environment / Session 的
// 服务端数据库关系解析策略上下文。每次 CONNECT 新鲜读取；任一关系读取失败时
// 调用方必须 fail closed。整条链从已认证的 code session ID 和签名租户 UUID
// 出发，不信任 relay 提交的任何 environment ID 或 allowlist。
func (h *Handler) loadUpstreamProxyPolicyContext(ctx context.Context, identity upstreamProxyIdentity) (upstreamProxyPolicyContext, error) {
	record, err := h.db.GetCodeSessionNetworkPolicyContext(
		ctx,
		identity.codeSessionExternalID,
		identity.organizationUUID,
		identity.workspaceUUID,
	)
	if err != nil {
		return upstreamProxyPolicyContext{}, err
	}
	policy, err := networkpolicy.ParsePolicy(record.EnvironmentConfig, record.AgentSnapshot)
	if err != nil {
		return upstreamProxyPolicyContext{}, err
	}
	return upstreamProxyPolicyContext{
		policy: policy,
		proxyPolicyScope: proxyPolicyScope{
			organizationUUID:      record.OrganizationUUID,
			workspaceUUID:         record.WorkspaceUUID,
			environmentExternalID: record.EnvironmentExternalID,
		},
	}, nil
}

// loadMCPProxyPolicyContext 为 MCP HTTP proxy 编译精确 URL 与 Environment
// host 策略；原始 Agent Snapshot 不会越过该加载边界。
func (h *Handler) loadMCPProxyPolicyContext(ctx context.Context, identity upstreamProxyIdentity) (mcpProxyPolicyContext, error) {
	record, err := h.db.GetCodeSessionNetworkPolicyContext(
		ctx,
		identity.codeSessionExternalID,
		identity.organizationUUID,
		identity.workspaceUUID,
	)
	if err != nil {
		return mcpProxyPolicyContext{}, err
	}
	policy, err := networkpolicy.ParseMCPProxyPolicy(record.EnvironmentConfig, record.AgentSnapshot)
	if err != nil {
		return mcpProxyPolicyContext{}, err
	}
	return mcpProxyPolicyContext{
		policy: policy,
		proxyPolicyScope: proxyPolicyScope{
			organizationUUID:      record.OrganizationUUID,
			workspaceUUID:         record.WorkspaceUUID,
			environmentExternalID: record.EnvironmentExternalID,
		},
	}, nil
}

// authorizeUpstreamProxyTarget 在 CONNECT 凭证校验之后、DNS 解析/拨号之前执行
// Environment networking 策略。拒绝时只向 relay 返回通用 framed 403，reason 与
// 维度标识只进服务端审计日志；不记录 credential、query、header 或 body。
func (h *Handler) authorizeUpstreamProxyTarget(ctx context.Context, identity upstreamProxyIdentity, target string) bool {
	policyContext, err := h.loadUpstreamPolicyContext(ctx, identity)
	attrs := []any{
		"event", "upstream_proxy_policy",
		"organization_uuid", identity.organizationUUID,
		"workspace_uuid", identity.workspaceUUID,
		"code_session_id", identity.codeSessionExternalID,
	}
	if err != nil {
		attrs = append(attrs,
			"reason", string(networkpolicy.ReasonPolicyUnavailable),
			"host", normalizedTargetHost(target),
			"error", err.Error(),
		)
		h.logger.WarnContext(ctx, "upstream proxy policy denied", attrs...)
		return false
	}
	attrs = append(attrs,
		"organization_uuid", policyContext.organizationUUID,
		"workspace_uuid", policyContext.workspaceUUID,
		"environment_id", policyContext.environmentExternalID,
	)
	decision := policyContext.policy.AuthorizeHTTPS(target)
	attrs = append(attrs,
		"reason", string(decision.Reason),
		"host", decision.Host,
	)
	if !decision.Allow {
		h.logger.WarnContext(ctx, "upstream proxy policy denied", attrs...)
		return false
	}
	h.logger.DebugContext(ctx, "upstream proxy policy allowed", attrs...)
	return true
}

// normalizedTargetHost 尽力从 CONNECT target（`host:port`）提取归一化 host，
// 仅用于策略上下文解析失败时的审计日志；解析失败返回空串，不影响 fail-closed。
func normalizedTargetHost(target string) string {
	host, _, err := net.SplitHostPort(target)
	if err != nil {
		return ""
	}
	normalized, err := networkpolicy.NormalizeHost(host)
	if err != nil {
		return ""
	}
	return normalized
}
