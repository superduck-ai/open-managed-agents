import type { AuthAccount, OrganizationAccess } from '../auth/api';
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
  access?: OrganizationAccess | null,
): AuthAccount | null {
  if (!account) return null;
  const memberships = account.memberships?.filter((item) => item.organization?.uuid === orgUuid) ?? [];
  return {
    ...account,
    effectiveOrganizationUuid: orgUuid,
    permissions:
      access?.source === 'platform_admin' && access.organization.uuid === orgUuid
        ? (account.permissions ?? [])
        : scopePermissions(memberships[0]?.role, workspace?.effective_role),
    memberships,
  };
}

export function scopePermissions(organizationRole?: string, workspaceRole?: string) {
  if (workspaceRole !== 'workspace_user' && workspaceRole !== 'workspace_admin') return [];
  const permissions = ['workspaces:view', 'members:view', 'workspace:members:view'];
  if (organizationRole === 'admin')
    permissions.push('members:manage', 'organization:manage', 'organization:manage_settings', 'workspaces:manage');
  if (workspaceRole === 'workspace_admin') permissions.push('workspace:members:manage');
  permissions.push('api:view', 'api:manage', 'workspace:api:resource_manage', 'workbench:view');
  if (organizationRole === 'admin')
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
  } catch {}
}

export function isBusinessQuery(query: { queryKey: readonly unknown[] }) {
  return !['auth', 'invitations'].includes(String(query.queryKey[0]));
}

export function scopeAccountForSwitch(
  refresh: boolean,
  refreshed: AuthAccount | null | undefined,
  known: AuthAccount | null | undefined,
  current: AuthAccount | null,
) {
  if (refresh) return refreshed ?? null;
  return known === undefined ? current : known;
}

export function workspacePreference(
  accountUuid: string,
  orgUuid: string,
  explicit: string | undefined,
  initial: string | undefined,
  navigate: boolean,
) {
  return explicit ?? (!navigate && initial ? initial : readPreference(accountUuid, orgUuid));
}
