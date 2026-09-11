package auth

// WorkspaceAccess 是当前请求的授权结果，不写入长期登录会话。
type WorkspaceAccess struct {
	OrganizationRole string
	Role             string
	Source           string
}

func (a WorkspaceAccess) ManageOrganization() bool { return a.OrganizationRole == "admin" }
func (a WorkspaceAccess) ManageMembers() bool      { return a.Role == "workspace_admin" }
func (a WorkspaceAccess) Develop() bool {
	return a.Role == "workspace_admin" || a.Role == "workspace_developer" || a.Role == "workspace_restricted_developer"
}
func (a WorkspaceAccess) ViewTraces() bool {
	return a.Role == "workspace_admin" || a.Role == "workspace_developer"
}
func (a WorkspaceAccess) Workbench() bool { return a.Role != "" && a.Role != "workspace_billing" }
func (a WorkspaceAccess) Billing() bool {
	return a.OrganizationRole == "admin" || a.OrganizationRole == "billing"
}

func (a WorkspaceAccess) Permissions() []string {
	if a.Role == "" {
		return []string{}
	}
	permissions := []string{"workspaces:view"}
	if a.ManageOrganization() {
		permissions = append(permissions, "members:view", "members:manage", "organization:manage", "organization:manage_settings", "workspaces:manage")
	}
	if a.ManageMembers() {
		permissions = append(permissions, "workspace:members:manage")
	}
	if a.Develop() {
		permissions = append(permissions, "api:view", "api:manage", "workspace:api:resource_manage")
	}
	if a.Workbench() {
		permissions = append(permissions, "workbench:view")
	}
	if a.Billing() {
		permissions = append(permissions, "billing:view", "billing:manage", "cost:view", "usage:view", "invoices:view")
	}
	return permissions
}
