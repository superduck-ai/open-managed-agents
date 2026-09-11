package api

import (
	"net/http"
	"strings"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
)

// platformActionAllowed 将现有用户态入口映射到角色能力，空间可见不代表动作可执行。
func platformActionAllowed(r *http.Request, access auth.WorkspaceAccess) bool {
	path := strings.TrimRight(r.URL.Path, "/")
	if strings.HasPrefix(path, "/v1/organizations") {
		// 组织资源按 chi 匹配后的具体路由检查组织权限或目标空间成员管理权限。
		return true
	}
	if strings.HasPrefix(path, "/v1/") {
		if strings.HasPrefix(path, "/v1/sessions") || (strings.HasPrefix(path, "/v1/files/") && strings.HasSuffix(path, "/content")) {
			return access.ViewTraces()
		}
		return access.Develop()
	}
	if strings.Contains(path, "/api_keys") || strings.Contains(path, "/api_key_count") {
		if strings.Contains(path, "/workspaces/") {
			// 工作区 Key 接口由资源层针对路径中的目标空间检查权限。
			return true
		}
		return access.Develop()
	}
	if strings.Contains(path, "/members") || strings.Contains(path, "/invites") || strings.Contains(path, "/invitations") {
		return access.ManageOrganization()
	}
	if strings.Contains(path, "/billing") || strings.Contains(path, "/cost_report") {
		return access.Billing()
	}
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/workspaces") {
		return access.ManageOrganization()
	}
	if strings.Contains(path, "/workbench") {
		return access.Workbench()
	}
	if strings.HasPrefix(path, "/web-api/sessions/") {
		return access.ViewTraces()
	}
	if strings.Contains(path, "/proxy/") || strings.Contains(path, "/mcp/") || strings.HasSuffix(path, "/upload_b64") {
		return access.Develop()
	}
	if strings.Contains(path, "/files/") || strings.Contains(path, "/observability/") {
		return access.ViewTraces()
	}
	return true
}
