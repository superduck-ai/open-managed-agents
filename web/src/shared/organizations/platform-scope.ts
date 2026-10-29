import type { OrganizationAccess } from '../auth/api';

export type PlatformScope = {
  accountUuid: string;
  targetOrganizationUuid: string;
  targetWorkspaceId?: string;
};

const storageKey = 'oma.platform-organization';

export function readPlatformScope(accountUuid: string): PlatformScope | undefined {
  try {
    const stored: unknown = JSON.parse(window.sessionStorage.getItem(storageKey) ?? 'null');
    if (!stored || typeof stored !== 'object') return;
    const value = stored as Partial<PlatformScope>;
    if (value.accountUuid !== accountUuid || typeof value.targetOrganizationUuid !== 'string') return;
    if (value.targetWorkspaceId !== undefined && typeof value.targetWorkspaceId !== 'string') return;
    return value as PlatformScope;
  } catch {
    return;
  }
}

export function savePlatformScope(scope: PlatformScope) {
  try {
    window.sessionStorage.setItem(storageKey, JSON.stringify(scope));
  } catch {
    return;
  }
}

export function clearPlatformScope() {
  try {
    window.sessionStorage.removeItem(storageKey);
  } catch {
    return;
  }
}

export function isPlatformOrganizationAccess(access: OrganizationAccess | null | undefined, organizationUuid: string) {
  return access?.organization.uuid === organizationUuid && access.source === 'platform_admin';
}
