package auth

// WorkspaceAccess 是当前请求的授权结果，不写入长期登录会话。
type WorkspaceAccess struct {
	OrganizationRole string
	Role             string
	Source           string
}

func (a WorkspaceAccess) ManageOrganization() bool { return a.OrganizationRole == "admin" }
func (a WorkspaceAccess) ManageMembers() bool      { return a.Role == "workspace_admin" }

func (a WorkspaceAccess) UseResources() bool {
	return a.Role == "workspace_user" || a.Role == "workspace_admin"
}

func (a WorkspaceAccess) Permissions() []string {
	if !a.UseResources() {
		return []string{}
	}
	permissions := []string{"workspaces:view"}
	if a.ManageOrganization() {
		permissions = append(permissions, "members:view", "members:manage", "organization:manage", "organization:manage_settings", "workspaces:manage")
	}
	if a.ManageMembers() {
		permissions = append(permissions, "workspace:members:manage")
	}
	permissions = append(permissions, "api:view", "api:manage", "workspace:api:resource_manage", "workbench:view")
	if a.ManageOrganization() {
		permissions = append(permissions, "billing:view", "billing:manage", "cost:view", "usage:view", "invoices:view")
	}
	return permissions
}
