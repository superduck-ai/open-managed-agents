import type { OrganizationAccess } from '../auth/api';

export type PlatformScope = {
  accountUuid: string;
  targetOrganizationUuid: string;
  targetWorkspaceId?: string;
  returnOrganizationUuid?: string;
  returnWorkspaceId?: string;
};

const storageKey = 'oma.platform-organization';

export function readPlatformScope(accountUuid: string): PlatformScope | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? 'null');
    if (!stored || typeof stored !== 'object') return;
    const value = stored as Partial<PlatformScope>;
    if (value.accountUuid !== accountUuid || typeof value.targetOrganizationUuid !== 'string') return;
    if (
      ['targetWorkspaceId', 'returnOrganizationUuid', 'returnWorkspaceId'].some((key) => {
        const field = value[key as keyof PlatformScope];
        return field !== undefined && typeof field !== 'string';
      })
    )
      return;
    return value as PlatformScope;
  } catch {
    return;
  }
}

export function savePlatformScope(scope: PlatformScope) {
  window.sessionStorage.setItem(storageKey, JSON.stringify(scope));
}

export function clearPlatformScope() {
  window.sessionStorage.removeItem(storageKey);
}

export function isPlatformOrganizationAccess(access: OrganizationAccess | null | undefined, organizationUuid: string) {
  return access?.organization.uuid === organizationUuid && access.source === 'platform_admin';
}
