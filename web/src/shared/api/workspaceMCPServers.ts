import { consoleApi, type ApiError } from './client';

export type WorkspaceMCPServer = {
  id: string;
  type: 'mcp_server';
  name: string;
  transport_type: 'url';
  url: string;
  status: 'active' | 'archived';
  created_at: string;
  updated_at: string;
  archived_at: string | null;
};

export type WorkspaceMCPServerPage = {
  data: WorkspaceMCPServer[];
  next_page: string | null;
};

export type MCPServerMutation = {
  name: string;
  url: string;
};

type ListWorkspaceMCPServersOptions = {
  search?: string;
  includeArchived?: boolean;
  page?: string;
};

function collectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/mcp_servers`;
}

function resourcePath(orgUuid: string, workspaceId: string, serverId: string) {
  return `${collectionPath(orgUuid, workspaceId)}/${encodeURIComponent(serverId)}`;
}

function workspaceHeaders(workspaceId: string) {
  return { 'X-Workspace-ID': workspaceId };
}

export function listWorkspaceMCPServers(
  orgUuid: string,
  workspaceId: string,
  options: ListWorkspaceMCPServersOptions = {},
) {
  const query = new URLSearchParams();
  if (options.search?.trim()) {
    query.set('search', options.search.trim());
  }
  if (options.includeArchived) {
    query.set('include_archived', 'true');
  }
  if (options.page) {
    query.set('page', options.page);
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : '';
  return consoleApi<WorkspaceMCPServerPage>(`${collectionPath(orgUuid, workspaceId)}${suffix}`, {
    headers: workspaceHeaders(workspaceId),
  });
}

export function getWorkspaceMCPServer(orgUuid: string, workspaceId: string, serverId: string) {
  return consoleApi<WorkspaceMCPServer>(resourcePath(orgUuid, workspaceId, serverId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function createWorkspaceMCPServer(
  orgUuid: string,
  workspaceId: string,
  input: MCPServerMutation,
  csrfToken?: string,
) {
  return consoleApi<WorkspaceMCPServer>(collectionPath(orgUuid, workspaceId), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function updateWorkspaceMCPServer(
  orgUuid: string,
  workspaceId: string,
  serverId: string,
  input: MCPServerMutation,
  csrfToken?: string,
) {
  return consoleApi<WorkspaceMCPServer>(resourcePath(orgUuid, workspaceId, serverId), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function archiveWorkspaceMCPServer(orgUuid: string, workspaceId: string, serverId: string, csrfToken?: string) {
  return consoleApi<WorkspaceMCPServer>(`${resourcePath(orgUuid, workspaceId, serverId)}/archive`, {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
  });
}

export function deleteWorkspaceMCPServer(orgUuid: string, workspaceId: string, serverId: string, csrfToken?: string) {
  return consoleApi<{ id: string; type: 'mcp_server_deleted'; deleted: boolean }>(
    resourcePath(orgUuid, workspaceId, serverId),
    { method: 'DELETE', headers: workspaceHeaders(workspaceId), csrfToken },
  );
}

export function mcpServerErrorMessage(error: unknown) {
  const apiError = error as Partial<ApiError> | undefined;
  if (apiError && typeof apiError.message === 'string' && apiError.message.trim()) {
    return apiError.message;
  }
  return error instanceof Error ? error.message : 'Request failed';
}
