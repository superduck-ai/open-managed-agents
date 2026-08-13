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
import { type MCPServerDestructiveTarget, type MCPServerEditorTarget } from './MCPServerDialogs';

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
  initialServerId,
}: {
  orgUuid?: string;
  workspaceId: string;
  workspaceReady: boolean;
  initialServerId?: string;
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
    queryKey: ['workspace-mcp-server', orgUuid ?? '', workspaceId, initialServerId ?? ''],
    queryFn: () => getWorkspaceMCPServer(orgUuid ?? '', workspaceId, initialServerId ?? ''),
    enabled: Boolean(orgUuid && workspaceId && workspaceReady && initialServerId),
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
  detailServer,
}: {
  orgUuid?: string;
  workspaceId: string;
  initialCreateOpen: boolean;
  initialServerId?: string;
  detailServer?: WorkspaceMCPServer;
}) {
  const { csrfToken } = useAuth();
  const queryClient = useQueryClient();
  const [editor, setEditor] = useState<MCPServerEditorTarget | null>(initialCreateOpen ? { mode: 'create' } : null);
  const [destructive, setDestructive] = useState<MCPServerDestructiveTarget | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [isActing, setIsActing] = useState(false);

  useEffect(() => {
    if (initialServerId && detailServer) {
      setEditor({ mode: 'edit', server: detailServer });
    }
  }, [detailServer, initialServerId]);

  const invalidate = async (serverId?: string) => {
    const promises = [
      queryClient.invalidateQueries({ queryKey: ['workspace-mcp-servers', orgUuid ?? '', workspaceId] }),
    ];
    if (serverId) {
      promises.push(
        queryClient.invalidateQueries({ queryKey: ['workspace-mcp-server', orgUuid ?? '', workspaceId, serverId] }),
      );
    }
    await Promise.all(promises);
  };

  const closeEditor = () => {
    setEditor(null);
    if (initialCreateOpen || initialServerId) {
      replacePageURL(mcpServersIndexHref(workspaceId));
    }
  };

  const submitEditor = async (input: MCPServerMutation) => {
    if (!orgUuid || !editor) {
      return;
    }
    if (editor.mode === 'create') {
      const created = await createWorkspaceMCPServer(orgUuid, workspaceId, input, csrfToken);
      await invalidate(created.id);
    } else {
      await updateWorkspaceMCPServer(orgUuid, workspaceId, editor.server.id, input, csrfToken);
      await invalidate(editor.server.id);
    }
    closeEditor();
  };

  const confirmDestructive = async () => {
    if (!orgUuid || !destructive || isActing) {
      return;
    }
    setIsActing(true);
    setActionError(null);
    try {
      if (destructive.action === 'archive') {
        await archiveWorkspaceMCPServer(orgUuid, workspaceId, destructive.server.id, csrfToken);
      } else {
        await deleteWorkspaceMCPServer(orgUuid, workspaceId, destructive.server.id, csrfToken);
      }
      await invalidate(destructive.server.id);
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

  return {
    editor,
    setEditor,
    destructive,
    actionError,
    isActing,
    closeEditor,
    submitEditor,
    confirmDestructive,
    openDestructive,
    closeDestructive: () => !isActing && setDestructive(null),
  };
}

export function mcpServersIndexHref(workspaceId: string) {
  return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/mcp-servers`;
}

function workspaceIdFromPath() {
  return window.location.pathname.match(/^\/workspaces\/([^/]+)\/mcp-servers(?:\/|$)/)?.[1] ?? '';
}

function replacePageURL(path: string) {
  window.history.replaceState(window.history.state, '', path);
}
