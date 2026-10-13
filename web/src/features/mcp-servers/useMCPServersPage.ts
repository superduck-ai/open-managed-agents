import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { useAuth } from '../../shared/auth/context';
import {
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
import {
  type MCPServerDestructiveTarget,
  type MCPServerDetailTarget,
  type MCPServerEditorTarget,
} from './MCPServerDialogs';

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
  const paginationKey = `${orgUuid ?? ''}\u0000${workspaceId}\u0000${search}`;
  const [pagination, setPagination] = useState<MCPServerPagination>(() => newMCPServerPagination(paginationKey));
  const currentPagination = pagination.key === paginationKey ? pagination : newMCPServerPagination(paginationKey);
  const pageIndex = currentPagination.pageIndex;
  const pageTokens = currentPagination.pageTokens;
  const listQuery = useQuery({
    queryKey: ['workspace-mcp-servers', orgUuid ?? '', workspaceId, search.trim(), pageTokens[pageIndex] ?? ''],
    queryFn: () =>
      listWorkspaceMCPServers(orgUuid ?? '', workspaceId, {
        search,
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

  const setPageIndex = (next: number | ((current: number) => number)) => {
    setPagination((current) => {
      const active = current.key === paginationKey ? current : newMCPServerPagination(paginationKey);
      return { ...active, pageIndex: typeof next === 'function' ? next(active.pageIndex) : next };
    });
  };

  const nextPage = () => {
    const nextPageToken = listQuery.data?.next_page;
    if (!nextPageToken) {
      return;
    }
    setPagination((current) => {
      const active = current.key === paginationKey ? current : newMCPServerPagination(paginationKey);
      return {
        key: paginationKey,
        pageIndex: active.pageIndex + 1,
        pageTokens: [...active.pageTokens.slice(0, active.pageIndex + 1), nextPageToken],
      };
    });
  };

  return {
    search,
    setSearch,
    pageIndex,
    setPageIndex,
    listQuery,
    detailQuery,
    nextPage,
  };
}

type MCPServerPagination = {
  key: string;
  pageIndex: number;
  pageTokens: Array<string | undefined>;
};

function newMCPServerPagination(key: string): MCPServerPagination {
  return { key, pageIndex: 0, pageTokens: [undefined] };
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
  const [panel, setPanel] = useState<MCPServerDetailTarget | null>(() =>
    initialServerId ? { serverId: initialServerId } : null,
  );
  const [editor, setEditor] = useState<MCPServerEditorTarget | null>(() =>
    initialCreateOpen ? { mode: 'create' } : null,
  );
  const [destructive, setDestructive] = useState<MCPServerDestructiveTarget | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [isActing, setIsActing] = useState(false);

  useEffect(() => {
    const handlePopState = () => {
      const routeState = mcpServerRouteStateFromPath();
      setPanel(routeState.panel);
      setEditor(routeState.editor);
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  const invalidateList = () =>
    queryClient.invalidateQueries({ queryKey: ['workspace-mcp-servers', orgUuid ?? '', workspaceId] });

  const closePanel = () => {
    setPanel(null);
    replacePageURL(mcpServersIndexHref(workspaceId));
  };

  const closeEditor = () => {
    setEditor(null);
    if (window.location.pathname === mcpServerCreateHref(workspaceId)) {
      replacePageURL(mcpServersIndexHref(workspaceId));
    }
  };

  const submitEditor = async (input: MCPServerMutation) => {
    if (!orgUuid || !editor) {
      return;
    }
    const saved =
      editor.mode === 'create'
        ? await createWorkspaceMCPServer(orgUuid, workspaceId, input, csrfToken)
        : await updateWorkspaceMCPServer(orgUuid, workspaceId, editor.server.id, input, csrfToken);
    queryClient.setQueryData(['workspace-mcp-server', orgUuid, workspaceId, saved.id], saved);
    await invalidateList();
    setEditor(null);
    setPanel({ serverId: saved.id });
    replacePageURL(mcpServerDetailHref(workspaceId, saved.id));
  };

  const confirmDestructive = async () => {
    if (!orgUuid || !destructive || isActing) {
      return;
    }
    setIsActing(true);
    setActionError(null);
    try {
      await deleteWorkspaceMCPServer(orgUuid, workspaceId, destructive.server.id, csrfToken);
      queryClient.removeQueries({
        queryKey: ['workspace-mcp-server', orgUuid, workspaceId, destructive.server.id],
        exact: true,
      });
      await invalidateList();
      if (panel?.serverId === destructive.server.id) {
        closePanel();
      }
      setDestructive(null);
    } catch (error) {
      setActionError(mcpServerErrorMessage(error));
    } finally {
      setIsActing(false);
    }
  };

  const openDestructive = (server: WorkspaceMCPServer) => {
    setActionError(null);
    setDestructive({ server });
  };

  const openCreate = () => {
    setPanel(null);
    setEditor({ mode: 'create' });
    pushPageURL(mcpServerCreateHref(workspaceId));
  };

  const openDetail = (serverId: string) => {
    setPanel({ serverId });
    pushPageURL(mcpServerDetailHref(workspaceId, serverId));
  };

  const openEdit = (server: WorkspaceMCPServer) => {
    setEditor({ mode: 'edit', server });
  };

  return {
    panel,
    editor,
    selectedServerId: panel?.serverId ?? null,
    detailServerId: panel?.serverId,
    destructive,
    actionError,
    isActing,
    closePanel,
    closeEditor,
    submitEditor,
    confirmDestructive,
    openCreate,
    openDetail,
    openEdit,
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

function mcpServerRouteStateFromPath(): {
  panel: MCPServerDetailTarget | null;
  editor: MCPServerEditorTarget | null;
} {
  const match = window.location.pathname.match(/\/mcp-servers\/([^/]+)$/);
  if (!match) {
    return { panel: null, editor: null };
  }
  if (match[1] === 'new') {
    return { panel: null, editor: { mode: 'create' } };
  }
  return { panel: { serverId: decodeURIComponent(match[1]) }, editor: null };
}
