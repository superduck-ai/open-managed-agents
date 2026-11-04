import type { AuthAccount } from '../auth/api';

export function canManageOrganizationLLMProviders(
  account: AuthAccount | null | undefined,
  organizationUuid: string | undefined,
) {
  return Boolean(
    organizationUuid &&
    account?.effectiveOrganizationUuid === organizationUuid &&
    account.permissions?.includes('organization:manage'),
  );
}

export function canManageWorkspaceLLMProviders(account: AuthAccount | null | undefined) {
  return account?.permissions?.includes('workspace:api:resource_manage') ?? false;
}
