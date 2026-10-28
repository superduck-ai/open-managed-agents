import type { AuthAccount } from '../auth/api';

export function canManageWorkspaceMembers(account: AuthAccount | null | undefined) {
  return account?.permissions?.includes('workspace:members:manage') ?? false;
}

export function canManageMembers(account: AuthAccount | null | undefined, orgUuid?: string) {
  return Boolean(
    account?.permissions?.includes('members:manage') && (!orgUuid || account.effectiveOrganizationUuid === orgUuid),
  );
}

export function canViewMembers(account: AuthAccount | null | undefined, orgUuid?: string) {
  return account?.permissions?.includes('members:view') || canManageMembers(account, orgUuid);
}

export function canViewWorkspaceMembers(account: AuthAccount | null | undefined) {
  return account?.permissions?.includes('workspace:members:view') || canManageWorkspaceMembers(account);
}
