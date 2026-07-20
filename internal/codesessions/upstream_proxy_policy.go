package codesessions

import (
	"context"
	"log/slog"
	"net"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

// upstreamProxyPolicyContext 是一次 CONNECT 授权所需的策略上下文：
// Policy 供纯策略模块决策，其余字段只用于服务端审计日志。
type upstreamProxyPolicyContext struct {
	policy                networkpolicy.Policy
	organizationID        int64
	workspaceID           int64
	environmentExternalID string
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
		organizationID:        record.OrganizationID,
		workspaceID:           record.WorkspaceID,
		environmentExternalID: record.EnvironmentExternalID,
		policy:                policy,
	}, nil
}

// authorizeUpstreamProxyTarget 在 CONNECT 凭证校验之后、DNS 解析/拨号之前执行
// Environment networking 策略。拒绝时只向 relay 返回通用 framed 403，reason 与
// 维度标识只进服务端审计日志；不记录 credential、query、header 或 body。
func (h *Handler) authorizeUpstreamProxyTarget(ctx context.Context, identity upstreamProxyIdentity, target string) bool {
	policyContext, err := h.loadPolicyContext(ctx, identity)
	attrs := []any{
		"component", "codesessions",
		"event", "upstream_proxy_policy",
		"organization_uuid", identity.organizationUUID,
		"workspace_uuid", identity.workspaceUUID,
		"organization_id", policyContext.organizationID,
		"workspace_id", policyContext.workspaceID,
		"environment_id", policyContext.environmentExternalID,
		"code_session_id", identity.codeSessionExternalID,
	}
	if err != nil {
		attrs = append(attrs,
			"reason", string(networkpolicy.ReasonPolicyUnavailable),
			"host", normalizedTargetHost(target),
			"error", err.Error(),
		)
		slog.WarnContext(ctx, "upstream proxy policy denied", attrs...)
		return false
	}
	decision := policyContext.policy.AuthorizeHTTPS(target)
	attrs = append(attrs,
		"reason", string(decision.Reason),
		"host", decision.Host,
	)
	if !decision.Allow {
		slog.WarnContext(ctx, "upstream proxy policy denied", attrs...)
		return false
	}
	slog.DebugContext(ctx, "upstream proxy policy allowed", attrs...)
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
