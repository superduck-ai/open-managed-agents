import type { AuthAccount } from '../auth/api';

export function canManageLLMProviders(account: AuthAccount | null | undefined, organizationUuid: string | undefined) {
  return Boolean(organizationUuid && account?.effectiveOrganizationUuid === organizationUuid);
}

export function canDeleteLLMProviders(account: AuthAccount | null | undefined, organizationUuid: string | undefined) {
  return (
    canManageLLMProviders(account, organizationUuid) && Boolean(account?.permissions?.includes('organization:manage'))
  );
}
