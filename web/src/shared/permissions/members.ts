import type { AuthAccount } from '../auth/api';

type AccountWithPermissions = AuthAccount & {
  permissions?: string[];
  account_permissions?: Array<string | { permission?: string; name?: string }>;
};

export function canManageMembers(account: AuthAccount | null | undefined) {
  if (!account) {
    return false;
  }

  const permissions = permissionNames(account as AccountWithPermissions);
  if (account.permissions) return permissions.has('members:manage');
  if (permissions.has('members:manage')) {
    return true;
  }

  return account.memberships?.some((membership) => membership.role === 'admin') ?? false;
}

function permissionNames(account: AccountWithPermissions) {
  const names = new Set<string>();
  for (const permission of account.permissions ?? []) {
    names.add(permission);
  }
  for (const permission of account.account_permissions ?? []) {
    if (typeof permission === 'string') {
      names.add(permission);
      continue;
    }
    if (permission.permission) {
      names.add(permission.permission);
    }
    if (permission.name) {
      names.add(permission.name);
    }
  }
  return names;
}
