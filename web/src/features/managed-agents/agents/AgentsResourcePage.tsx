import { type ApiError } from '../../../shared/api/client';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button } from '../../../shared/ui/button';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName
} from '../../../shared/ui/data-table-interactions';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '../../../shared/ui/dropdown-menu';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { useWorkspace } from '../../../shared/workspaces/context';
import { Archive, ChevronLeft, ChevronRight, Plus, Search, TriangleAlert } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { agentsListLimit, archiveAgent, createAgent, defaultAgentFilters, exactAgentIdPattern, listAgents, retrieveAgent, searchAgentsByName } from '../api';
import {
  AgentFilterDropdown,
  AgentsEmptyState,
  AgentsListState,
  AgentStatusBadge,
  ConfirmAgentsArchiveDialog,
  CreateResourceDialog,
  EmptyState,
  ManagedErrorAlert,
  ManagedSearchField,
  ManagedWarningAlert
} from '../components/common';
import { createdFilterLabel, createdFilterOptionsFor, managedColumnLabel, resourceCreateLabel, resourceDescription, resourceEmptyAction, resourceSearchPlaceholder, resourceTitle, statusFilterLabel, statusFilterOptionsFor } from '../labels';
import { type AgentApiResponse, type AgentCreatedFilter, type AgentFilterMenu, type AgentLoadMode, type AgentStatusFilter, type CreateAgentInput, type PageCursor, type ResourceConfig } from '../types';
import { agentDetailHref, errorMessage, handleInternalLinkClick } from '../utils';
import { CreateAgentDialog } from './create-dialog';
import {
  agentMatchesClientFilters,
  agentModelName,
  compactAgentId,
  emptyAgents,
  relativeTime,
  rowFromAgent
} from './model';

export { AgentDetailPage } from './detail';
export {
  agentDetailCreatedRange,
  agentDetailStatusValues,
  agentModelName,
  BUILT_IN_AGENT_TOOLSETS,
  compactAgentId,
  relativeTime
} from './model';

export function AgentsResourcePage({ config, routeWorkspaceId }: { config: ResourceConfig; routeWorkspaceId?: string }) {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  const workspaceId = routeWorkspaceId || activeWorkspaceId;
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [createdFilter, setCreatedFilter] = useState<AgentCreatedFilter>(defaultAgentFilters.created);
  const [statusFilter, setStatusFilter] = useState<AgentStatusFilter>(defaultAgentFilters.status);
  const [openFilterMenu, setOpenFilterMenu] = useState<AgentFilterMenu | null>(null);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [remoteAgentsState, setRemoteAgentsState] = useState<{
    workspaceId: string;
    requestKey: string;
    mode: AgentLoadMode;
    data: AgentApiResponse[] | null;
    truncated: boolean;
  }>({
    workspaceId: '',
    requestKey: '',
    mode: 'list',
    data: null,
    truncated: false
  });
  const [loadError, setLoadError] = useState<{ mode: 'list' | 'search'; message: string } | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [archiveError, setArchiveError] = useState<string | null>(null);
  const [archivingIds, setArchivingIds] = useState<Set<string>>(() => new Set());
  const [confirmArchiveAgentIds, setConfirmArchiveAgentIds] = useState<string[] | null>(null);
  const previousWorkspaceIdRef = useRef(workspaceId);
  const [agentPageState, setAgentPageState] = useState<{
    workspaceId: string;
    requestKey: string;
    cursor: PageCursor;
    history: PageCursor[];
    nextPage: PageCursor;
    localPage: number;
  }>({
    workspaceId: '',
    requestKey: '',
    cursor: null,
    history: [],
    nextPage: null,
    localPage: 0
  });
  const isAgentsPage = config.section === 'agents';
  const normalizedSearch = debouncedSearch.trim();
  const agentLoadMode: AgentLoadMode = normalizedSearch
    ? exactAgentIdPattern.test(normalizedSearch)
      ? 'retrieve'
      : 'search'
    : 'list';
  const agentListFilters = useMemo(
    () => ({ created: createdFilter, status: statusFilter }),
    [createdFilter, statusFilter]
  );
  const agentRequestKey = `${workspaceId}:${agentLoadMode}:${normalizedSearch}:${createdFilter}:${statusFilter}`;
  const remoteAgents =
    remoteAgentsState.workspaceId === workspaceId && remoteAgentsState.requestKey === agentRequestKey
      ? remoteAgentsState.data
      : null;
  const remoteAgentsTruncated =
    remoteAgentsState.workspaceId === workspaceId && remoteAgentsState.requestKey === agentRequestKey
      ? remoteAgentsState.truncated
      : false;
  const agentPageCursor =
    agentPageState.workspaceId === workspaceId && agentPageState.requestKey === agentRequestKey ? agentPageState.cursor : null;
  const agentPageHistory =
    agentPageState.workspaceId === workspaceId && agentPageState.requestKey === agentRequestKey ? agentPageState.history : [];
  const agentNextPage =
    agentPageState.workspaceId === workspaceId && agentPageState.requestKey === agentRequestKey ? agentPageState.nextPage : null;
  const agentLocalPage =
    agentPageState.workspaceId === workspaceId && agentPageState.requestKey === agentRequestKey ? agentPageState.localPage : 0;
  const agentsFromApi = remoteAgents ?? emptyAgents;
  const agentRowsFromApi = useMemo(() => agentsFromApi.map(rowFromAgent), [agentsFromApi]);
  const rows = isAgentsPage ? agentRowsFromApi : (config.rows ?? []);
  const filteredRows = rows.filter((row) =>
    Object.values(row)
      .map((value) => (typeof value === 'string' ? value : ''))
      .join(' ')
      .toLowerCase()
      .includes(search.toLowerCase())
  );
  const visibleRows = rows.length ? filteredRows : [];
  const visibleAgents = useMemo(
    () => agentsFromApi.filter((agent) => agentMatchesClientFilters(agent, agentListFilters, agentLoadMode !== 'list')),
    [agentListFilters, agentLoadMode, agentsFromApi]
  );
  const displayedAgents =
    agentLoadMode === 'search'
      ? visibleAgents.slice(agentLocalPage * agentsListLimit, (agentLocalPage + 1) * agentsListLimit)
      : visibleAgents;
  const title = resourceTitle(config, msg);
  const description = resourceDescription(config, msg);
  const createLabel = resourceCreateLabel(config, msg);
  const searchPlaceholder = resourceSearchPlaceholder(config, msg);
  const createdOptions = createdFilterOptionsFor(msg);
  const statusOptions = statusFilterOptionsFor(msg);
  const hasActiveAgentFilters = Boolean(search.trim()) || createdFilter !== 'all' || statusFilter !== 'active';
  const searchResultsTruncated = agentLoadMode === 'search' && remoteAgentsTruncated;
  const hasPreviousAgentsPage = agentLoadMode === 'search' ? agentLocalPage > 0 : Boolean(agentPageHistory.length);
  const hasNextAgentsPage =
    agentLoadMode === 'search'
      ? (agentLocalPage + 1) * agentsListLimit < visibleAgents.length
      : Boolean(agentNextPage);

  useEffect(() => {
    if (previousWorkspaceIdRef.current === workspaceId) {
      return;
    }
    previousWorkspaceIdRef.current = workspaceId;
    setRemoteAgentsState({ workspaceId, requestKey: '', mode: 'list', data: null, truncated: false });
    setOpenFilterMenu(null);
    setConfirmArchiveAgentIds(null);
    setArchiveError(null);
    setArchivingIds(new Set());
  }, [workspaceId]);

  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedSearch(search), 300);
    return () => window.clearTimeout(timer);
  }, [search]);

  useEffect(() => {
    if (!isAgentsPage) {
      return;
    }

    let active = true;
    const requestWorkspaceId = workspaceId;
    const pageCursor = agentLoadMode === 'list' ? agentPageCursor : null;
    const requestKey = agentRequestKey;
    setRemoteAgentsState({ workspaceId: requestWorkspaceId, requestKey, mode: agentLoadMode, data: null, truncated: false });
    const request =
      agentLoadMode === 'retrieve'
        ? retrieveAgent(normalizedSearch, requestWorkspaceId)
            .then((agent) => ({ data: [agent], next_page: null, truncated: false }))
            .catch((error: unknown) => {
              const apiError = error as ApiError;
              if (apiError?.status === 404) {
                return { data: [], next_page: null, truncated: false };
              }
              throw error;
            })
        : agentLoadMode === 'search'
          ? searchAgentsByName(requestWorkspaceId, normalizedSearch, agentListFilters)
          : listAgents(requestWorkspaceId, pageCursor, agentListFilters).then((page) => ({ ...page, truncated: false }));

    request
      .then((page) => {
        if (active) {
          setLoadError(null);
          setArchiveError(null);
          setRemoteAgentsState({
            workspaceId: requestWorkspaceId,
            requestKey,
            mode: agentLoadMode,
            data: page.data ?? [],
            truncated: Boolean(page.truncated)
          });
          setAgentPageState((current) => ({
            workspaceId: requestWorkspaceId,
            requestKey,
            cursor: pageCursor,
            history: current.workspaceId === requestWorkspaceId && current.requestKey === requestKey ? current.history : [],
            nextPage: page.next_page ?? null,
            localPage: current.workspaceId === requestWorkspaceId && current.requestKey === requestKey ? current.localPage : 0
          }));
        }
      })
      .catch((error: unknown) => {
        if (active) {
          setLoadError({ mode: agentLoadMode === 'list' ? 'list' : 'search', message: errorMessage(error) });
          setRemoteAgentsState({ workspaceId: requestWorkspaceId, requestKey, mode: agentLoadMode, data: [], truncated: false });
          setAgentPageState((current) => ({
            workspaceId: requestWorkspaceId,
            requestKey,
            cursor: pageCursor,
            history: current.workspaceId === requestWorkspaceId && current.requestKey === requestKey ? current.history : [],
            nextPage: null,
            localPage: 0
          }));
        }
      });

    return () => {
      active = false;
    };
  }, [agentListFilters, agentLoadMode, agentPageCursor, agentRequestKey, isAgentsPage, normalizedSearch, refreshKey, workspaceId]);

  const resetAgentsPage = () => {
    setAgentPageState({
      workspaceId,
      requestKey: agentRequestKey,
      cursor: null,
      history: [],
      nextPage: null,
      localPage: 0
    });
    setArchiveError(null);
  };

  const retryAgentsLoad = () => {
    setLoadError(null);
    setRemoteAgentsState({ workspaceId, requestKey: agentRequestKey, mode: agentLoadMode, data: null, truncated: false });
    setRefreshKey((value) => value + 1);
  };

  const resetAgentFilters = () => {
    setSearch('');
    setDebouncedSearch('');
    setCreatedFilter(defaultAgentFilters.created);
    setStatusFilter(defaultAgentFilters.status);
    setOpenFilterMenu(null);
    setArchiveError(null);
    setAgentPageState({
      workspaceId,
      requestKey: '',
      cursor: null,
      history: [],
      nextPage: null,
      localPage: 0
    });
  };

  const handleSearchChange = (value: string) => {
    setSearch(value);
    setArchiveError(null);
    setAgentPageState({
      workspaceId,
      requestKey: '',
      cursor: null,
      history: [],
      nextPage: null,
      localPage: 0
    });
  };

  const handleCreatedFilterChange = (value: AgentCreatedFilter) => {
    setCreatedFilter(value);
    setOpenFilterMenu(null);
    resetAgentsPage();
  };

  const handleStatusFilterChange = (value: AgentStatusFilter) => {
    setStatusFilter(value);
    setOpenFilterMenu(null);
    resetAgentsPage();
  };

  const handleCreateAgent = async (input: CreateAgentInput) => {
    return createAgent(input, workspaceId);
  };

  const goToNextAgentsPage = () => {
    if (agentLoadMode === 'search') {
      if (!hasNextAgentsPage) {
        return;
      }
      setAgentPageState({
        workspaceId,
        requestKey: agentRequestKey,
        cursor: null,
        history: [],
        nextPage: agentNextPage,
        localPage: agentLocalPage + 1
      });
      setArchiveError(null);
      return;
    }
    if (!agentNextPage) {
      return;
    }
    setAgentPageState({
      workspaceId,
      requestKey: agentRequestKey,
      cursor: agentNextPage,
      history: [...agentPageHistory, agentPageCursor],
      nextPage: null,
      localPage: 0
    });
    setArchiveError(null);
  };

  const goToPreviousAgentsPage = () => {
    if (agentLoadMode === 'search') {
      if (agentLocalPage <= 0) {
        return;
      }
      setAgentPageState({
        workspaceId,
        requestKey: agentRequestKey,
        cursor: null,
        history: [],
        nextPage: agentNextPage,
        localPage: agentLocalPage - 1
      });
      setArchiveError(null);
      return;
    }
    if (!agentPageHistory.length) {
      return;
    }
    setAgentPageState({
      workspaceId,
      requestKey: agentRequestKey,
      cursor: agentPageHistory[agentPageHistory.length - 1],
      history: agentPageHistory.slice(0, -1),
      nextPage: null,
      localPage: 0
    });
    setArchiveError(null);
  };

  const removeArchivedAgents = (ids: string[]) => {
    const archived = new Set(ids);
    const archivedAt = new Date().toISOString();
    setRemoteAgentsState((current) =>
      current.workspaceId === workspaceId && current.data
        ? {
            ...current,
            data:
              statusFilter === 'all'
                ? current.data.map((agent) =>
                    archived.has(agent.id) ? { ...agent, archived_at: agent.archived_at ?? archivedAt, updated_at: archivedAt } : agent
                  )
                : current.data.filter((agent) => !archived.has(agent.id))
          }
        : current
    );
  };

  const handleArchiveAgents = async (agentIds: string[]) => {
    const ids = [...new Set(agentIds)].filter(Boolean);
    if (!ids.length) {
      return;
    }

    setArchiveError(null);
    setArchivingIds((current) => new Set([...current, ...ids]));
    try {
      await Promise.all(ids.map((id) => archiveAgent(id, workspaceId)));
      removeArchivedAgents(ids);
    } catch (error) {
      setArchiveError(errorMessage(error));
    } finally {
      setArchivingIds((current) => {
        const next = new Set(current);
        ids.forEach((id) => next.delete(id));
        return next;
      });
    }
  };

  const requestArchiveAgents = (agentIds: string[]) => {
    const ids = [...new Set(agentIds)].filter((id) => agentsFromApi.some((agent) => agent.id === id));
    if (!ids.length) {
      return;
    }
    setArchiveError(null);
    setConfirmArchiveAgentIds(ids);
  };

  const confirmArchiveAgentsAction = async () => {
    const ids = confirmArchiveAgentIds ?? [];
    if (!ids.length) {
      return;
    }
    await handleArchiveAgents(ids);
    setConfirmArchiveAgentIds(null);
  };

  const renderAgentActionMenu = (agent: AgentApiResponse, archiving: boolean) => (
    <div className="flex justify-end">
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <MoreActionsButton
              label={msg('managedAgents.common.moreActions', 'More actions')}
              disabled={archiving}
              className="disabled:cursor-wait"
            />
          }
        />
        <DropdownMenuContent align="end" className="w-[164px]">
          <DropdownMenuItem disabled={archiving || Boolean(agent.archived_at)} onClick={() => requestArchiveAgents([agent.id])}>
            <Archive className="size-4" aria-hidden />
            {msg('managedAgents.agents.archiveAgent', 'Archive agent')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );

  return (
    <section className="min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5 flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[28px] font-semibold leading-tight text-foreground">{title}</h1>
          <p className="mt-2 text-[15px] leading-5 text-muted-foreground">{description}</p>
        </div>
        {createLabel ? (
          <Button
            type="button"
            className="h-9 shrink-0"
            onClick={() => setDialogOpen(true)}
          >
            <Plus className="size-4" aria-hidden />
            {createLabel}
          </Button>
        ) : null}
      </header>

      <div className="mb-7 flex flex-wrap items-center gap-2">
        <ManagedSearchField
          id={`${config.section}-search`}
          value={search}
          placeholder={searchPlaceholder}
          prefix={config.searchPrefix}
          onChange={handleSearchChange}
        />
        <AgentFilterDropdown
          label={msg('managedAgents.filters.created', 'Created')}
          valueLabel={createdFilterLabel(createdFilter, msg)}
          options={createdOptions}
          value={createdFilter}
          menu="created"
          open={openFilterMenu === 'created'}
          menuWidthClass="w-[380px]"
          onOpenChange={setOpenFilterMenu}
          onSelect={handleCreatedFilterChange}
        />
        <AgentFilterDropdown
          label={msg('managedAgents.filters.status', 'Status')}
          valueLabel={statusFilterLabel(statusFilter, msg)}
          options={statusOptions}
          value={statusFilter}
          menu="status"
          open={openFilterMenu === 'status'}
          menuWidthClass="w-[230px]"
          onOpenChange={setOpenFilterMenu}
          onSelect={handleStatusFilterChange}
        />
      </div>

      <div className="overflow-visible">
        {archiveError ? <ManagedErrorAlert className="mb-3">{archiveError}</ManagedErrorAlert> : null}
        {searchResultsTruncated && visibleAgents.length ? (
          <ManagedWarningAlert className="mb-3">
            {msg('managedAgents.agents.searchTruncated', "Couldn't search every agent. Narrow the search or paste an exact ID.")}
          </ManagedWarningAlert>
        ) : null}

        {loadError && isAgentsPage ? (
          <AgentsListState
            icon={loadError.mode === 'list' ? TriangleAlert : Search}
            title={
              loadError.mode === 'list'
                ? msg('managedAgents.agents.loadFailed', 'Could not load agents')
                : msg('managedAgents.agents.searchFailed', 'Search failed')
            }
            body={loadError.message}
            actionLabel={msg('common.retry', 'Retry')}
            onAction={retryAgentsLoad}
          />
        ) : isAgentsPage ? (
          <Table className={dataTableClassName}>
            <TableHeader>
              <TableRow className={dataTableHeaderRowClassName}>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-[185px]')}>{managedColumnLabel('ID', msg)}</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-auto')}>{managedColumnLabel('Name', msg)}</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-[210px]')}>{managedColumnLabel('Model', msg)}</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-[120px]')}>{managedColumnLabel('Status', msg)}</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-[150px]')}>{managedColumnLabel('Created', msg)}</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-[150px]')}>{managedColumnLabel('Last updated', msg)}</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, 'w-[48px] px-2')} aria-label={managedColumnLabel('Actions', msg)} />
              </TableRow>
            </TableHeader>
            <TableBody>
              {remoteAgents === null ? (
                <TableRow className="border-0 hover:bg-transparent">
                  <TableCell colSpan={7} className="h-[280px] text-center text-sm text-muted-foreground">
                    {msg('managedAgents.agents.loading', 'Loading agents...')}
                  </TableCell>
                </TableRow>
              ) : (
                displayedAgents.map((agent) => {
                  const archiving = archivingIds.has(agent.id);
                  const detailHref = agentDetailHref(workspaceId, agent.id);
                  return (
                    <DataTableRow key={agent.id}>
                      <DataTableCell edge="start">
                        <CopyIdCell
                          value={agent.id}
                          ariaLabel={msg('managedAgents.common.copyIdValue', 'Copy {id}', { id: agent.id })}
                          className="gap-1.5"
                        >
                          <a
                            href={detailHref}
                            className="truncate font-mono text-[13px] text-foreground underline-offset-4 hover:underline"
                            onClick={(event) => handleInternalLinkClick(event, detailHref)}
                          >
                            {compactAgentId(agent.id)}
                          </a>
                        </CopyIdCell>
                      </DataTableCell>
                      <DataTableCell className="truncate text-foreground">
                        <a
                          href={detailHref}
                          className="underline-offset-4 hover:underline"
                          onClick={(event) => handleInternalLinkClick(event, detailHref)}
                        >
                          {agent.name || msg('managedAgents.agents.untitled', 'Untitled agent')}
                        </a>
                      </DataTableCell>
                      <DataTableCell className="truncate font-mono text-[13px] text-muted-foreground">
                        {agentModelName(agent.model)}
                      </DataTableCell>
                      <DataTableCell className="truncate">
                        <AgentStatusBadge archived={Boolean(agent.archived_at)} />
                      </DataTableCell>
                      <DataTableCell className="truncate text-muted-foreground">{relativeTime(agent.created_at)}</DataTableCell>
                      <DataTableCell className="truncate text-muted-foreground">{relativeTime(agent.updated_at)}</DataTableCell>
                      <DataTableCell edge="end" className="px-2">{renderAgentActionMenu(agent, archiving)}</DataTableCell>
                    </DataTableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        ) : (
          <Table className="table-fixed border-collapse text-left">
            <TableHeader>
              <TableRow className="border-border text-muted-foreground hover:bg-transparent">
                {config.columns.map((column) => (
                  <TableHead key={column || 'select'} className="px-3 text-muted-foreground">
                    {column ? managedColumnLabel(column, msg) : <span className="block size-4 rounded border border-border" aria-hidden />}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleRows.map((row, index) => (
                <TableRow key={`${config.section}-${index}`} className="border-border text-foreground">
                  {config.columns.map((column) => (
                    <TableCell key={column || 'select'} className="h-11 truncate px-3">
                      {column ? row[column] : <span className="block size-4 rounded border border-border" aria-hidden />}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}

        {isAgentsPage
          ? !loadError &&
            remoteAgents !== null &&
            !visibleAgents.length && (
              <AgentsEmptyState
                trueEmpty={!hasActiveAgentFilters}
                truncated={searchResultsTruncated}
                trueEmptyActionLabel={resourceEmptyAction(config, msg) ?? msg('managedAgents.agents.emptyAction', 'Get started with agents')}
                onCreate={() => setDialogOpen(true)}
                onReset={resetAgentFilters}
              />
            )
          : !visibleRows.length && <EmptyState config={config} />}
      </div>

      <div className="mt-9 flex items-center gap-2">
        <Button
          type="button"
          disabled={!hasPreviousAgentsPage}
          variant="outline"
          size="icon-lg"
          aria-label={msg('pagination.previousPage', 'Previous page')}
          onClick={goToPreviousAgentsPage}
        >
          <ChevronLeft className="size-4" aria-hidden />
        </Button>
        <Button
          type="button"
          disabled={!hasNextAgentsPage}
          variant="outline"
          size="icon-lg"
          aria-label={msg('pagination.nextPage', 'Next page')}
          onClick={goToNextAgentsPage}
        >
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      </div>

      {dialogOpen && createLabel ? (
        config.section === 'agents' ? (
          <CreateAgentDialog workspaceId={workspaceId} onClose={() => setDialogOpen(false)} onCreate={handleCreateAgent} />
        ) : (
          <CreateResourceDialog title={createLabel} onClose={() => setDialogOpen(false)} />
        )
      ) : null}

      {isAgentsPage && confirmArchiveAgentIds?.length ? (
        <ConfirmAgentsArchiveDialog
          count={confirmArchiveAgentIds.length}
          busy={confirmArchiveAgentIds.some((id) => archivingIds.has(id))}
          onCancel={() => setConfirmArchiveAgentIds(null)}
          onConfirm={() => void confirmArchiveAgentsAction()}
        />
      ) : null}
    </section>
  );
}
