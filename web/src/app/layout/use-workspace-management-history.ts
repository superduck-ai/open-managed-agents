import { z } from 'zod';
import { normalizeReturnTo } from '../../shared/auth/redirects';
import { useEffect } from 'react';
import { quickstartModelSetupReturn } from '../../shared/workspaces/model-setup-navigation';
import { workspaceIdFromPath, workspaceSwitchPath } from '../../shared/workspaces/presentation';
import { workspaceManagementNavigation } from './navigation';

const availablePages = new Set(
  workspaceManagementNavigation.children.filter((item) => !item.hidden).map((item) => item.href),
);

export function useWorkspaceManagementHistory(
  accountId: string | undefined,
  organizationId: string | undefined,
  workspaceId: string,
  currentPage: string | undefined,
) {
  const key =
    accountId && organizationId && workspaceId
      ? `oma.workspace-management.last-page:${JSON.stringify([accountId, organizationId, workspaceId])}`
      : null;

  useEffect(() => {
    if (!key || !currentPage || !availablePages.has(currentPage)) return;
    try {
      window.localStorage.setItem(key, currentPage);
    } catch {}
  }, [key, currentPage]);

  try {
    const page = key ? window.localStorage.getItem(key) : null;
    return page && availablePages.has(page) ? page : '/llm-models';
  } catch {
    return '/llm-models';
  }
}

const managementOriginSchema = z.object({ href: z.string(), workspaceId: z.string() });

function managementOriginKey(accountId: string | undefined, organizationId: string | undefined) {
  return accountId && organizationId
    ? `oma.workspace-management.return:${JSON.stringify([accountId, organizationId])}`
    : null;
}

export function readWorkspaceManagementOrigin(accountId: string | undefined, organizationId: string | undefined) {
  const key = managementOriginKey(accountId, organizationId);
  try {
    const stored = managementOriginSchema.safeParse(
      JSON.parse(key ? (window.sessionStorage.getItem(key) ?? 'null') : 'null'),
    );
    if (stored.success && normalizeReturnTo(stored.data.href) === stored.data.href) return stored.data;
  } catch {}
  return null;
}

export function saveWorkspaceManagementOrigin(
  accountId: string | undefined,
  organizationId: string | undefined,
  origin: z.infer<typeof managementOriginSchema>,
) {
  const key = managementOriginKey(accountId, organizationId);
  if (!key) return;
  try {
    window.sessionStorage.setItem(key, JSON.stringify(origin));
  } catch {}
}

export function initialWorkspaceManagementOrigin(
  accountId: string | undefined,
  organizationId: string | undefined,
  workspaceId: string,
  currentPath: string,
  managementPage: boolean,
) {
  const setupReturn = managementPage ? quickstartModelSetupReturn(window.location.search, workspaceId) : null;
  return setupReturn
    ? { href: setupReturn, workspaceId }
    : (readWorkspaceManagementOrigin(accountId, organizationId) ?? {
        href: managementPage ? '/dashboard' : currentPath,
        workspaceId: workspaceIdFromPath(currentPath) || workspaceId,
      });
}

export function workspaceManagementReturnHref(origin: z.infer<typeof managementOriginSchema>, workspaceId: string) {
  if (origin.workspaceId === workspaceId) return origin.href;
  const url = new URL(origin.href, window.location.origin);
  const search = /^(?:\/quickstart|\/workspaces\/[^/]+\/agent-quickstart)\/?$/.test(url.pathname) ? url.search : '';
  return workspaceSwitchPath(url.pathname, workspaceId) + search + url.hash;
}
