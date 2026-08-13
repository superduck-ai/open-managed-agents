import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useAuth } from '../../shared/auth/context';
import {
  archiveWorkspaceMCPServer,
  createWorkspaceMCPServer,
  deleteWorkspaceMCPServer,
  getWorkspaceMCPServer,
  listWorkspaceMCPServers,
  mcpServerErrorMessage,
  updateWorkspaceMCPServer,
  type MCPServerMutation,
  type WorkspaceMCPServer,
} from '../../shared/api/workspaceMCPServers';
import { useWorkspace } from '../../shared/workspaces/context';
import { type MCPServerDestructiveTarget, type MCPServerPanelTarget } from './MCPServerDialogs';

export type MCPServerScope = 'active' | 'all';

export function useMCPServerWorkspace() {
  const { orgUuid, activeWorkspace, activeWorkspaceId, workspaces } = useWorkspace();
  const routeWorkspaceId = workspaceIdFromPath();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  return {
    orgUuid,
    workspaceId,
    workspaceReady: !routeWorkspaceId || routeWorkspaceId === activeWorkspaceId,
    workspaceName: workspaces.find((workspace) => workspace.id === workspaceId)?.name ?? activeWorkspace.name,
  };
}

export function useMCPServerData({
  orgUuid,
  workspaceId,
  workspaceReady,
  detailServerId,
}: {
  orgUuid?: string;
  workspaceId: string;
  workspaceReady: boolean;
  detailServerId?: string;
}) {
  const [search, setSearch] = useState('');
  const [scope, setScope] = useState<MCPServerScope>('active');
  const [pageIndex, setPageIndex] = useState(0);
  const [pageTokens, setPageTokens] = useState<Array<string | undefined>>([undefined]);
  const listQuery = useQuery({
    queryKey: ['workspace-mcp-servers', orgUuid ?? '', workspaceId, search.trim(), scope, pageTokens[pageIndex] ?? ''],
    queryFn: () =>
      listWorkspaceMCPServers(orgUuid ?? '', workspaceId, {
        search,
        includeArchived: scope === 'all',
        page: pageTokens[pageIndex],
      }),
    enabled: Boolean(orgUuid && workspaceId && workspaceReady),
    retry: false,
  });
  const detailQuery = useQuery({
    queryKey: ['workspace-mcp-server', orgUuid ?? '', workspaceId, detailServerId ?? ''],
    queryFn: () => getWorkspaceMCPServer(orgUuid ?? '', workspaceId, detailServerId ?? ''),
    enabled: Boolean(orgUuid && workspaceId && workspaceReady && detailServerId),
    retry: false,
  });

  useEffect(() => {
    setPageIndex(0);
    setPageTokens([undefined]);
  }, [orgUuid, scope, search, workspaceId]);

  const nextPage = () => {
    const nextPageToken = listQuery.data?.next_page;
    if (!nextPageToken) {
      return;
    }
    setPageTokens((current) => [...current.slice(0, pageIndex + 1), nextPageToken]);
    setPageIndex((current) => current + 1);
  };

  return {
    search,
    setSearch,
    scope,
    setScope,
    pageIndex,
    setPageIndex,
    listQuery,
    detailQuery,
    nextPage,
  };
}

export function useMCPServerActions({
  orgUuid,
  workspaceId,
  initialCreateOpen,
  initialServerId,
}: {
  orgUuid?: string;
  workspaceId: string;
  initialCreateOpen: boolean;
  initialServerId?: string;
}) {
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  const [panel, setPanel] = useState<MCPServerPanelTarget | null>(() =>
    initialCreateOpen ? { mode: 'create' } : initialServerId ? { mode: 'detail', serverId: initialServerId } : null,
  );
  const [destructive, setDestructive] = useState<MCPServerDestructiveTarget | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [isActing, setIsActing] = useState(false);

  useEffect(() => {
    const handlePopState = () => setPanel(panelTargetFromPath());
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  const invalidateList = () =>
    queryClient.invalidateQueries({ queryKey: ['workspace-mcp-servers', orgUuid ?? '', workspaceId] });

  const closePanel = () => {
    setPanel(null);
    replacePageURL(mcpServersIndexHref(workspaceId));
  };

  const submitPanel = async (input: MCPServerMutation) => {
    if (!orgUuid || !panel || panel.mode === 'detail') {
      return;
    }
    const saved =
      panel.mode === 'create'
        ? await createWorkspaceMCPServer(orgUuid, workspaceId, input, csrfToken)
        : await updateWorkspaceMCPServer(orgUuid, workspaceId, panel.server.id, input, csrfToken);
    queryClient.setQueryData(['workspace-mcp-server', orgUuid, workspaceId, saved.id], saved);
    await invalidateList();
    setPanel({ mode: 'detail', serverId: saved.id });
    replacePageURL(mcpServerDetailHref(workspaceId, saved.id));
  };

  const confirmDestructive = async () => {
    if (!orgUuid || !destructive || isActing) {
      return;
    }
    setIsActing(true);
    setActionError(null);
    try {
      if (destructive.action === 'archive') {
        const archived = await archiveWorkspaceMCPServer(orgUuid, workspaceId, destructive.server.id, csrfToken);
        queryClient.setQueryData(['workspace-mcp-server', orgUuid, workspaceId, archived.id], archived);
        await invalidateList();
      } else {
        await deleteWorkspaceMCPServer(orgUuid, workspaceId, destructive.server.id, csrfToken);
        queryClient.removeQueries({
          queryKey: ['workspace-mcp-server', orgUuid, workspaceId, destructive.server.id],
          exact: true,
        });
        await invalidateList();
      }
      if (destructive.action === 'delete' && selectedServerId(panel) === destructive.server.id) {
        closePanel();
      }
      setDestructive(null);
    } catch (error) {
      setActionError(mcpServerErrorMessage(error));
    } finally {
      setIsActing(false);
    }
  };

  const openDestructive = (action: MCPServerDestructiveTarget['action'], server: WorkspaceMCPServer) => {
    setActionError(null);
    setDestructive({ action, server });
  };

  const openCreate = () => {
    setPanel({ mode: 'create' });
    pushPageURL(mcpServerCreateHref(workspaceId));
  };

  const openDetail = (serverId: string) => {
    setPanel({ mode: 'detail', serverId });
    pushPageURL(mcpServerDetailHref(workspaceId, serverId));
  };

  const openEdit = (server: WorkspaceMCPServer) => {
    setPanel({ mode: 'edit', server });
    pushPageURL(mcpServerDetailHref(workspaceId, server.id));
  };

  const showDetail = (serverId: string) => {
    setPanel({ mode: 'detail', serverId });
    replacePageURL(mcpServerDetailHref(workspaceId, serverId));
  };

  return {
    panel,
    selectedServerId: selectedServerId(panel),
    detailServerId: panel?.mode === 'detail' ? panel.serverId : undefined,
    destructive,
    actionError,
    isActing,
    closePanel,
    submitPanel,
    confirmDestructive,
    openCreate,
    openDetail,
    openEdit,
    showDetail,
    openDestructive,
    closeDestructive: () => !isActing && setDestructive(null),
  };
}

export function mcpServersIndexHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/mcp-servers`;
}

export function mcpServerCreateHref(workspaceId: string) {
  return `${mcpServersIndexHref(workspaceId)}/new`;
}

export function mcpServerDetailHref(workspaceId: string, serverId: string) {
  return `${mcpServersIndexHref(workspaceId)}/${encodeURIComponent(serverId)}`;
}

function workspaceIdFromPath() {
  return window.location.pathname.match(/^\/workspaces\/([^/]+)\/mcp-servers(?:\/|$)/)?.[1] ?? '';
}

function replacePageURL(path: string) {
  window.history.replaceState(window.history.state, '', path);
}

function pushPageURL(path: string) {
  if (window.location.pathname !== path) {
    window.history.pushState(window.history.state, '', path);
  }
}

function panelTargetFromPath(): MCPServerPanelTarget | null {
  const match = window.location.pathname.match(/\/mcp-servers\/([^/]+)$/);
  if (!match) {
    return null;
  }
  return match[1] === 'new' ? { mode: 'create' } : { mode: 'detail', serverId: decodeURIComponent(match[1]) };
}

function selectedServerId(panel: MCPServerPanelTarget | null) {
  if (!panel || panel.mode === 'create') {
    return null;
  }
  return panel.mode === 'detail' ? panel.serverId : panel.server.id;
}
