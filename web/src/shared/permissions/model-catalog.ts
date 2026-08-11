import type { AuthAccount } from '../auth/api';

export function canRefreshModelCatalog(account: AuthAccount | null | undefined, orgUuid?: string) {
  if (!account) {
    return false;
  }
  const normalizedOrgUuid = orgUuid?.trim();
  return (
    account.memberships?.some(
      (membership) =>
        membership.role?.trim().toLowerCase() === 'admin' &&
        (!normalizedOrgUuid || !membership.organization?.uuid || membership.organization.uuid === normalizedOrgUuid),
    ) ?? false
  );
}
