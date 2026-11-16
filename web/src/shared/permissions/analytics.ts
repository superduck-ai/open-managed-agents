import type { AuthAccount } from '../auth/api';

export function canViewAnalytics(
  account: AuthAccount | null | undefined,
  organizationUuid: string | undefined,
  permission: 'usage:view' | 'cost:view',
) {
  return Boolean(account?.effectiveOrganizationUuid === organizationUuid && account?.permissions?.includes(permission));
}
