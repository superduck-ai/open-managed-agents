import { useEffect } from 'react';
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
