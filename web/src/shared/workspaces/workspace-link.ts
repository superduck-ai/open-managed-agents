import { consoleApi, type ApiError } from '../api/client';
import type { BootstrapResponse } from '../auth/api';
import { returnToFromSearch } from '../auth/redirects';
import { workspaceIdFromPath } from './presentation';

export function startupWorkspaceId(location: Pick<Location, 'pathname' | 'search'>) {
  const pathname =
    location.pathname === '/login'
      ? new URL(returnToFromSearch(location.search), 'https://console.local').pathname
      : location.pathname;
  return workspaceIdFromPath(pathname);
}

export function fetchWorkspaceBootstrap(workspaceId: string, signal: AbortSignal) {
  return consoleApi<BootstrapResponse>(`/api/bootstrap?target_workspace_id=${encodeURIComponent(workspaceId)}`, {
    context: {},
    signal,
  });
}

export function workspaceLinkError(status: 403 | 404): ApiError {
  return {
    status,
    code: 'workspace_link_unavailable',
    message: status === 403 ? 'Workspace access denied' : 'Workspace unavailable',
  };
}
