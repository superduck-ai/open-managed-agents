import { consoleApi, type ApiError } from './client';

export type WorkspaceMCPServer = {
  id: string;
  type: 'mcp_server';
  name: string;
  transport_type: 'url';
  url: string;
  created_at: string;
  updated_at: string;
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

export function deleteWorkspaceMCPServer(orgUuid: string, workspaceId: string, serverId: string, csrfToken?: string) {
  return consoleApi<{ id: string; type: 'mcp_server_deleted'; deleted: boolean }>(
    resourcePath(orgUuid, workspaceId, serverId),
    { method: 'DELETE', headers: workspaceHeaders(workspaceId), csrfToken },
  );
}

export function mcpServerErrorMessage(error: unknown, duplicateMessage?: string) {
  const apiError = error as Partial<ApiError> | undefined;
  if (apiError?.code === 'conflict' && duplicateMessage) {
    return duplicateMessage;
  }
  if (apiError && typeof apiError.message === 'string' && apiError.message.trim()) {
    return apiError.message;
  }
  return error instanceof Error ? error.message : 'Request failed';
}
