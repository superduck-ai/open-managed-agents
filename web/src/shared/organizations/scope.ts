import type { AuthAccount } from '../auth/api';
import type { Workspace } from '../workspaces/api';

export function fallbackOrganization(account: AuthAccount | null, preferred?: string) {
  const memberships = account?.memberships ?? [];
  return [preferred, account?.default_organization_uuid, ...memberships.map((item) => item.organization?.uuid)].find(
    (id) => id && memberships.some((item) => item.organization?.uuid === id),
  );
}

export function scopedAccount(
  account: AuthAccount | null,
  orgUuid?: string,
  workspace?: Workspace,
): AuthAccount | null {
  if (!account) return null;
  const memberships = account.memberships?.filter((item) => item.organization?.uuid === orgUuid) ?? [];
  // bootstrap 的组织权限不能带入另一个组织；成员角色来自当前 membership。
  return { ...account, permissions: scopePermissions(memberships[0]?.role, workspace?.effective_role), memberships };
}

// 与 #339 WorkspaceAccess.Permissions 合同一致；后端仍负责最终授权。
export function scopePermissions(organizationRole?: string, workspaceRole?: string) {
  if (!workspaceRole) return [];
  const permissions = ['workspaces:view'];
  if (organizationRole === 'admin')
    permissions.push(
      'members:view',
      'members:manage',
      'organization:manage',
      'organization:manage_settings',
      'workspaces:manage',
    );
  if (workspaceRole === 'workspace_admin') permissions.push('workspace:members:manage');
  if (['workspace_admin', 'workspace_developer', 'workspace_restricted_developer'].includes(workspaceRole))
    permissions.push('api:view', 'api:manage', 'workspace:api:resource_manage');
  if (workspaceRole !== 'workspace_billing') permissions.push('workbench:view');
  if (organizationRole === 'admin' || organizationRole === 'billing')
    permissions.push('billing:view', 'billing:manage', 'cost:view', 'usage:view', 'invoices:view');
  return permissions;
}

export function chooseWorkspace(workspaces: Workspace[], preferred: string) {
  return (
    workspaces.find((workspace) => workspace.id === preferred) ?? workspaces.find((workspace) => workspace.is_default)
  );
}

export function readPreference(accountUuid: string, orgUuid?: string) {
  try {
    return orgUuid
      ? (window.localStorage.getItem(`oma.workspace.${accountUuid}.${orgUuid}`) ?? '')
      : (window.sessionStorage.getItem(`oma.organization.${accountUuid}`) ?? '');
  } catch {
    return '';
  }
}

export function savePreference(accountUuid: string, orgUuid: string, workspaceId: string) {
  try {
    window.sessionStorage.setItem(`oma.organization.${accountUuid}`, orgUuid);
    window.localStorage.setItem(`oma.workspace.${accountUuid}.${orgUuid}`, workspaceId);
  } catch {
    /* 浏览器禁用存储时，本次会话仍可切换。 */
  }
}

// 除账号级查询外全部取消，覆盖现在及之后新增的业务 prefix。
export function isBusinessQuery(query: { queryKey: readonly unknown[] }) {
  return !['auth', 'invitations'].includes(String(query.queryKey[0]));
}
