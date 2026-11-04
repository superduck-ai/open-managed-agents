import type { AuthAccount } from '../auth/api';

export function canManageLLMProviders(account: AuthAccount | null | undefined, organizationUuid: string | undefined) {
  return Boolean(
    organizationUuid &&
    account?.effectiveOrganizationUuid === organizationUuid &&
    account.permissions?.includes('organization:manage'),
  );
}
