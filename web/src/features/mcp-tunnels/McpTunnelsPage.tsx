import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate } from '@tanstack/react-router';
import { AlertCircle, Archive, Cable, Copy, Plus, Search } from 'lucide-react';
import { useEffect, useMemo, useState, type FormEvent, type MouseEvent } from 'react';

import { useAuth } from '../../shared/auth/context';
import { useFormatters, useI18n } from '../../shared/i18n';
import { copyText } from '../../shared/lib/clipboard';
import { cn } from '../../shared/lib/utils';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import {
  CopyIdCell,
  DataTableCell,
  DataTableResourceLink,
  DataTableRow,
  MoreActionsButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '../../shared/ui/data-table-interactions';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { ResourceFilterDropdown, ResourceSearchField } from '../../shared/ui/resource-list-controls';
import { ResourceListState } from '../../shared/ui/resource-list-state';
import { toast } from '../../shared/ui/sonner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { useWorkspace } from '../../shared/workspaces/context';
import { workspaceIdFromPath, workspaceMcpTunnelDetailPath } from '../../shared/workspaces/presentation';
import { archiveMcpTunnel, createMcpTunnel, listMcpTunnels, type McpTunnel } from './api';
import { ConfirmTunnelActionDialog, ConnectionBadge, CreateTunnelDialog, type PendingTunnelAction } from './components';
import { visibleTunnelRefreshInterval } from './config';

type TunnelStatusFilter = 'active' | 'all';
type NavigateToTunnel = (href: string, state?: Record<string, unknown>) => void;

export function McpTunnelsPage() {
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <McpTunnelsContent
      routeWorkspaceId={workspaceIdFromPath(location.pathname)}
      onNavigate={(href, state) => void navigate({ href, state })}
    />
  );
}

export function McpTunnelsContent({
  routeWorkspaceId,
  onNavigate,
}: {
  routeWorkspaceId?: string;
  onNavigate?: NavigateToTunnel;
}) {
  const { msg } = useI18n();
  const queryClient = useQueryClient();
  const { csrfToken } = useAuth();
  const { orgUuid, activeWorkspaceId, selectWorkspace } = useWorkspace();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<TunnelStatusFilter>('active');
  const [filterOpen, setFilterOpen] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [displayName, setDisplayName] = useState('');
  const [pendingAction, setPendingAction] = useState<PendingTunnelAction | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const includeArchived = statusFilter === 'all';

  useEffect(() => {
    if (routeWorkspaceId && routeWorkspaceId !== activeWorkspaceId) {
      selectWorkspace(routeWorkspaceId);
    }
  }, [activeWorkspaceId, routeWorkspaceId, selectWorkspace]);

  const tunnelsQuery = useQuery({
    queryKey: ['console-mcp-tunnels', orgUuid, workspaceId, includeArchived],
    queryFn: () => listMcpTunnels(orgUuid ?? '', workspaceId, includeArchived),
    enabled: Boolean(orgUuid && workspaceId),
    refetchInterval: visibleTunnelRefreshInterval,
    refetchIntervalInBackground: false,
  });

  const refreshTunnels = async () => {
    await queryClient.invalidateQueries({ queryKey: ['console-mcp-tunnels', orgUuid, workspaceId] });
  };

  const handleCreate = async (event: FormEvent) => {
    event.preventDefault();
    if (!orgUuid) return;
    setBusyAction('create');
    setActionError(null);
    try {
      const tunnel = await createMcpTunnel(orgUuid, workspaceId, displayName.trim(), csrfToken);
      setCreateOpen(false);
      setDisplayName('');
      await refreshTunnels();
      toast.success(msg('mcpTunnels.toast.created', 'MCP tunnel created'));
      onNavigate?.(workspaceMcpTunnelDetailPath(workspaceId, tunnel.id), { mcpTunnelCreated: true });
    } catch (error) {
      setActionError(errorMessage(error, msg('mcpTunnels.error.generic', 'Something went wrong. Please try again.')));
    } finally {
      setBusyAction(null);
    }
  };

  const handleArchive = async () => {
    if (!orgUuid || pendingAction?.type !== 'archive') return;
    const tunnel = pendingAction.tunnel;
    setBusyAction(`archive:${tunnel.id}`);
    setActionError(null);
    try {
      await archiveMcpTunnel(orgUuid, workspaceId, tunnel.id, csrfToken);
      toast.success(msg('mcpTunnels.toast.archived', 'MCP tunnel archived'));
      setPendingAction(null);
      await refreshTunnels();
    } catch (error) {
      setActionError(errorMessage(error, msg('mcpTunnels.error.generic', 'Something went wrong. Please try again.')));
    } finally {
      setBusyAction(null);
    }
  };

  const filteredTunnels = useMemo(() => filterTunnels(tunnelsQuery.data ?? [], search), [search, tunnelsQuery.data]);
  const hasFilters = Boolean(search.trim()) || statusFilter !== 'active';
  const navigateToTunnel = onNavigate
    ? (tunnel: McpTunnel) => onNavigate(workspaceMcpTunnelDetailPath(workspaceId, tunnel.id))
    : undefined;

  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5 flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[28px] font-semibold leading-tight text-foreground">
            {msg('mcpTunnels.title', 'MCP tunnels')}
          </h1>
          <p className="mt-2 text-[15px] leading-5 text-muted-foreground">
            {msg(
              'mcpTunnels.description',
              'Connect local MCP servers to managed agent sessions without exposing the local server directly.',
            )}
          </p>
        </div>
        <Button type="button" className="h-9 shrink-0" onClick={() => setCreateOpen(true)} disabled={!orgUuid}>
          <Plus className="size-4" aria-hidden />
          {msg('mcpTunnels.newTunnel', 'New tunnel')}
        </Button>
      </header>

      <div className="mb-7 flex flex-wrap items-center gap-2">
        <ResourceSearchField
          id="mcp-tunnel-search"
          value={search}
          placeholder={msg('mcpTunnels.searchPlaceholder', 'Search by name or exact ID')}
          onChange={setSearch}
        />
        <ResourceFilterDropdown
          label={msg('common.status', 'Status')}
          valueLabel={statusFilter === 'active' ? msg('common.active', 'Active') : msg('common.all', 'All')}
          options={[
            { value: 'active', label: msg('common.active', 'Active') },
            { value: 'all', label: msg('common.all', 'All') },
          ]}
          value={statusFilter}
          menu="status"
          open={filterOpen}
          menuWidthClass="w-[230px]"
          onOpenChange={(menu) => setFilterOpen(Boolean(menu))}
          onSelect={setStatusFilter}
        />
      </div>

      {actionError ? (
        <Alert variant="destructive" className="mb-3">
          <AlertCircle aria-hidden />
          <AlertTitle>{msg('mcpTunnels.error.actionFailed', 'Action failed')}</AlertTitle>
          <AlertDescription>{actionError}</AlertDescription>
        </Alert>
      ) : null}

      {tunnelsQuery.isError ? (
        <ResourceListState
          icon={AlertCircle}
          title={msg('mcpTunnels.error.loadFailed', 'Could not load MCP tunnels')}
          body={errorMessage(
            tunnelsQuery.error,
            msg('mcpTunnels.error.generic', 'Something went wrong. Please try again.'),
          )}
          actionLabel={msg('common.retry', 'Retry')}
          onAction={() => void tunnelsQuery.refetch()}
        />
      ) : (
        <div className="overflow-visible">
          <TunnelTable
            tunnels={filteredTunnels}
            workspaceId={workspaceId}
            loading={tunnelsQuery.isPending}
            busyAction={busyAction}
            onOpen={navigateToTunnel}
            onArchive={(tunnel) => setPendingAction({ type: 'archive', tunnel })}
          />
          {tunnelsQuery.isSuccess && !filteredTunnels.length ? (
            <ResourceListState
              icon={hasFilters ? Search : Cable}
              title={
                hasFilters
                  ? msg('mcpTunnels.empty.filteredTitle', 'No matching MCP tunnels')
                  : msg('mcpTunnels.empty.title', 'No MCP tunnels yet')
              }
              body={
                hasFilters
                  ? msg('mcpTunnels.empty.filteredBody', 'Try a different search or reset the filters.')
                  : msg(
                      'mcpTunnels.empty.body',
                      'Create a tunnel to connect a local MCP server to managed agent sessions.',
                    )
              }
              actionLabel={
                hasFilters ? msg('mcpTunnels.resetFilters', 'Reset filters') : msg('mcpTunnels.newTunnel', 'New tunnel')
              }
              onAction={() => {
                if (hasFilters) {
                  setSearch('');
                  setStatusFilter('active');
                } else {
                  setCreateOpen(true);
                }
              }}
            />
          ) : null}
        </div>
      )}

      <CreateTunnelDialog
        open={createOpen}
        displayName={displayName}
        busy={busyAction === 'create'}
        onOpenChange={(open) => {
          setCreateOpen(open);
          if (!open) setDisplayName('');
        }}
        onDisplayNameChange={setDisplayName}
        onSubmit={handleCreate}
      />
      <ConfirmTunnelActionDialog
        action={pendingAction}
        busy={Boolean(pendingAction && busyAction === `archive:${pendingAction.tunnel.id}`)}
        onClose={() => setPendingAction(null)}
        onConfirm={() => void handleArchive()}
      />
    </section>
  );
}

function TunnelTable({
  tunnels,
  workspaceId,
  loading,
  busyAction,
  onOpen,
  onArchive,
}: {
  tunnels: McpTunnel[];
  workspaceId: string;
  loading: boolean;
  busyAction: string | null;
  onOpen?: (tunnel: McpTunnel) => void;
  onArchive: (tunnel: McpTunnel) => void;
}) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  return (
    <Table className={dataTableClassName}>
      <TableHeader>
        <TableRow className={dataTableHeaderRowClassName}>
          <TableHead className={cn(dataTableHeaderCellClassName, 'w-[185px]')}>{msg('common.id', 'ID')}</TableHead>
          <TableHead className={cn(dataTableHeaderCellClassName, 'w-auto')}>{msg('common.name', 'Name')}</TableHead>
          <TableHead className={cn(dataTableHeaderCellClassName, 'w-[150px]')}>
            {msg('mcpTunnels.column.connection', 'Connection')}
          </TableHead>
          <TableHead className={cn(dataTableHeaderCellClassName, 'w-[90px]')}>
            {msg('mcpTunnels.column.channels', 'Channels')}
          </TableHead>
          <TableHead className={cn(dataTableHeaderCellClassName, 'w-[140px]')}>
            {msg('common.created', 'Created')}
          </TableHead>
          <TableHead
            className={cn(dataTableHeaderCellClassName, 'w-[48px] px-2')}
            aria-label={msg('common.actions', 'Actions')}
          />
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableRow className="border-0 hover:bg-transparent">
            <TableCell colSpan={6} className="h-[280px] text-center text-sm text-muted-foreground">
              {msg('mcpTunnels.loading', 'Loading MCP tunnels...')}
            </TableCell>
          </TableRow>
        ) : (
          tunnels.map((tunnel) => {
            const detailHref = workspaceMcpTunnelDetailPath(workspaceId, tunnel.id);
            return (
              <DataTableRow key={tunnel.id} className={cn(tunnel.archived_at && 'opacity-65')}>
                <DataTableCell edge="start">
                  <CopyIdCell
                    value={tunnel.id}
                    ariaLabel={msg('mcpTunnels.copyTunnelId', 'Copy tunnel ID {id}', { id: tunnel.id })}
                    className="gap-1.5"
                  >
                    <DataTableResourceLink
                      href={detailHref}
                      className="truncate font-mono text-xs font-medium text-foreground"
                      onClick={(event) => handleTunnelLinkClick(event, tunnel, onOpen)}
                    >
                      {tunnel.id}
                    </DataTableResourceLink>
                  </CopyIdCell>
                </DataTableCell>
                <DataTableCell className="truncate text-foreground">
                  <span className="inline-flex max-w-full items-center gap-2">
                    <DataTableResourceLink
                      href={detailHref}
                      className="truncate text-foreground"
                      onClick={(event) => handleTunnelLinkClick(event, tunnel, onOpen)}
                    >
                      {tunnel.display_name || tunnel.id}
                    </DataTableResourceLink>
                    {tunnel.archived_at ? (
                      <Badge variant="secondary">{msg('common.archived', 'Archived')}</Badge>
                    ) : null}
                  </span>
                </DataTableCell>
                <DataTableCell>
                  <ConnectionBadge state={tunnel.connection.state} />
                </DataTableCell>
                <DataTableCell className="text-muted-foreground">{tunnel.connection.channels.length}</DataTableCell>
                <DataTableCell className="truncate text-muted-foreground">
                  {formatTunnelCreatedAt(tunnel.created_at, formatters.date)}
                </DataTableCell>
                <DataTableCell edge="end" className="px-2">
                  <TunnelListActions
                    tunnel={tunnel}
                    busy={Boolean(busyAction?.endsWith(tunnel.id))}
                    onArchive={onArchive}
                  />
                </DataTableCell>
              </DataTableRow>
            );
          })
        )}
      </TableBody>
    </Table>
  );
}

function handleTunnelLinkClick(
  event: MouseEvent<HTMLAnchorElement>,
  tunnel: McpTunnel,
  onOpen?: (tunnel: McpTunnel) => void,
) {
  if (
    !onOpen ||
    event.defaultPrevented ||
    event.button !== 0 ||
    event.currentTarget.target ||
    event.metaKey ||
    event.altKey ||
    event.ctrlKey ||
    event.shiftKey
  ) {
    return;
  }
  event.preventDefault();
  onOpen(tunnel);
}

function TunnelListActions({
  tunnel,
  busy,
  onArchive,
}: {
  tunnel: McpTunnel;
  busy: boolean;
  onArchive: (tunnel: McpTunnel) => void;
}) {
  const { msg } = useI18n();
  const name = tunnel.display_name || tunnel.id;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <MoreActionsButton label={msg('mcpTunnels.actionsFor', 'Actions for {name}', { name })} disabled={busy} />
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuItem onClick={() => void copyText(tunnel.id)}>
          <Copy /> {msg('mcpTunnels.action.copyId', 'Copy tunnel ID')}
        </DropdownMenuItem>
        <DropdownMenuItem onClick={() => void copyText(tunnel.mcp_url)}>
          <Copy /> {msg('mcpTunnels.action.copyMcpUrl', 'Copy MCP URL')}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          variant="destructive"
          disabled={Boolean(tunnel.archived_at)}
          onClick={() => onArchive(tunnel)}
        >
          <Archive /> {msg('common.archive', 'Archive')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function filterTunnels(tunnels: McpTunnel[], search: string) {
  const value = search.trim().toLowerCase();
  if (!value) return tunnels;
  return tunnels.filter(
    (tunnel) => tunnel.id.toLowerCase() === value || (tunnel.display_name ?? '').toLowerCase().includes(value),
  );
}

function formatTunnelCreatedAt(value: string, formatDate: (value: Date | number) => string) {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : formatDate(date);
}

function errorMessage(error: unknown, fallback: string) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message || fallback;
  }
  return fallback;
}
