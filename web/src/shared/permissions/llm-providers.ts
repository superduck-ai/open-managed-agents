import type { AuthAccount } from '../auth/api';

export function canManageLLMProviders(account: AuthAccount | null | undefined, organizationUuid: string | undefined) {
  if (!account || !organizationUuid) {
    return false;
  }

  return (
    account.memberships?.some(
      (membership) => membership.organization?.uuid === organizationUuid && membership.role?.toLowerCase() === 'admin',
    ) ?? false
  );
}
