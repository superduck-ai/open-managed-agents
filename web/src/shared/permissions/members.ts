import type { AuthAccount } from "../auth/api";

type AccountWithPermissions = AuthAccount & {
  permissions?: string[];
  account_permissions?: Array<string | { permission?: string; name?: string }>;
};

const memberManagementRoles = new Set(["admin", "owner", "primary_owner", "membership_admin"]);

export function canManageMembers(account: AuthAccount | null | undefined) {
  if (!account) {
    return false;
  }

  const permissions = permissionNames(account as AccountWithPermissions);
  if (permissions.has("members:manage") || permissions.has("membership_admins:manage")) {
    return true;
  }

  return account.memberships?.some((membership) => memberManagementRoles.has(normalizeRole(membership.role))) ?? false;
}

function permissionNames(account: AccountWithPermissions) {
  const names = new Set<string>();
  for (const permission of account.permissions ?? []) {
    names.add(permission);
  }
  for (const permission of account.account_permissions ?? []) {
    if (typeof permission === "string") {
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

function normalizeRole(role: string | undefined) {
  return (role ?? "").trim().toLowerCase();
}
