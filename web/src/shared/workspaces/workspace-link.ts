import { z } from 'zod';
import type { ApiError } from '../api/client';
import { PlatformAdminClient } from '../api/platform-admin';
import { fetchBootstrap } from '../auth/api';
import { returnToFromSearch } from '../auth/redirects';
import { listConsoleWorkspaces } from './api';
import { workspaceIdFromPath } from './presentation';

const workspaceScopeSchema = z.object({ workspace_id: z.string().min(1), organization_uuid: z.string().min(1) });

export function startupWorkspaceId(location: Pick<Location, 'pathname' | 'search'>) {
  const pathname =
    location.pathname === '/login'
      ? new URL(returnToFromSearch(location.search), 'https://console.local').pathname
      : location.pathname;
  return workspaceIdFromPath(pathname);
}

export async function loadWorkspaceLink(
  client: PlatformAdminClient,
  workspaceId: string,
  accountUuid: string,
  signal: AbortSignal,
) {
  const resolved = workspaceScopeSchema.parse(
    await client.request<unknown>(`/workspaces/${encodeURIComponent(workspaceId)}/scope`, { signal }),
  );
  signal.throwIfAborted();
  if (resolved.workspace_id !== workspaceId) throw new Error('Workspace scope does not match the requested workspace');
  const orgUuid = resolved.organization_uuid;
  const bootstrap = await fetchBootstrap({ organizationUuid: orgUuid });
  signal.throwIfAborted();
  if (bootstrap.account?.uuid !== accountUuid || bootstrap.organization_access?.organization.uuid !== orgUuid)
    throw workspaceLinkError(403);
  const workspaces = await listConsoleWorkspaces(orgUuid);
  signal.throwIfAborted();
  const selected = workspaces.find((workspace) => workspace.id === workspaceId && !workspace.archived_at);
  if (!selected) throw workspaceLinkError(404);
  return {
    orgUuid,
    workspaces,
    activeWorkspaceId: selected.id,
    account: bootstrap.account,
    access: bootstrap.organization_access,
    csrfToken: bootstrap.csrf_token,
  };
}

export function workspaceLinkError(status: 403 | 404): ApiError {
  return {
    status,
    code: 'workspace_link_unavailable',
    message: status === 403 ? 'Workspace access denied' : 'Workspace unavailable',
  };
}
