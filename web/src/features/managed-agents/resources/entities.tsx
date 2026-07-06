import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '../../../shared/ui/dropdown-menu';
import { toast } from '../../../shared/ui/sonner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { useWorkspace } from '../../../shared/workspaces/context';
import clsx from 'clsx';
import { Archive, ChevronLeft, ChevronRight, Copy, MoreVertical, Pencil, Play, Plus, X } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { agentDetailStatusValues } from '../agents/model';
import { archiveManagedEntity, createManagedEntity, deleteManagedEntity, listAgents, listManagedEntities, pauseDeployment, runDeployment, unpauseDeployment, updateManagedEntity } from '../api';
import { AgentFilterDropdown, AgentSelectionCheckbox, ConfirmEntityDialog, EmptyState, ManagedErrorAlert, ManagedSearchField } from '../components/common';
import { entityActionLabel, entityKindLabel, managedColumnLabel, managedMessage, managedToastMessage, resourceCreateLabel, resourceDescription, resourceSearchPlaceholder, resourceTitle } from '../labels';
import { type AgentDetailCreatedFilter, type AgentDetailStatusFilter, type AgentStatusFilter, type DeploymentApiResponse, type EntityOption, type ManagedEntityApiResponse, type ManagedEntityFormValues, type ManagedEntityListFilters, type ManagedEntitySection, type PageCursor, type ResourceConfig, type SessionApiResponse } from '../types';
import { compactEntityId, copyText, errorMessage, managedEntityDetailHref } from '../utils';
import { ManagedEntityDialog } from './dialogs';
import { cellsForEntity, columnWidth, entityAgentId, entityAgentLabel, entityDisplayName, entityStatusLabel } from './model';

type ManagedFilterMenu = 'agent' | 'created' | 'deployment' | 'status';
type DeploymentStatusFilter = NonNullable<ManagedEntityListFilters['status']>;

function defaultGenericStatusFilter(section: ManagedEntitySection): AgentStatusFilter {
  switch (section) {
    case 'environments':
    case 'credential-vaults':
      return 'all';
    case 'sessions':
    case 'deployments':
    case 'memory-stores':
      return 'active';
  }
}

export function ManagedEntitiesPage({ config }: { config: ResourceConfig & { section: ManagedEntitySection } }) {
  const { msg } = useI18n();
  const { activeWorkspaceId } = useWorkspace();
  const [search, setSearch] = useState('');
  const [entities, setEntities] = useState<ManagedEntityApiResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [dialogState, setDialogState] = useState<{ mode: 'create' | 'edit'; entity?: ManagedEntityApiResponse } | null>(null);
  const [confirmState, setConfirmState] = useState<{ action: 'archive' | 'delete'; entity: ManagedEntityApiResponse } | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [selectedEntityIds, setSelectedEntityIds] = useState<Set<string>>(() => new Set());
  const [deploymentAgentOptions, setDeploymentAgentOptions] = useState<EntityOption[]>([]);
  const [createdFilter, setCreatedFilter] = useState<AgentDetailCreatedFilter>('all_time');
  const [deploymentAgentFilter, setDeploymentAgentFilter] = useState('');
  const [deploymentStatusFilter, setDeploymentStatusFilter] = useState<DeploymentStatusFilter>('all');
  const [genericStatusFilter, setGenericStatusFilter] = useState<AgentStatusFilter>(defaultGenericStatusFilter(config.section));
  const [openFilterMenu, setOpenFilterMenu] = useState<ManagedFilterMenu | null>(null);
  const [sessionAgentFilter, setSessionAgentFilter] = useState('');
  const [sessionDeploymentFilter, setSessionDeploymentFilter] = useState('');
  const [sessionStatusFilter, setSessionStatusFilter] = useState<AgentDetailStatusFilter>('active');
  const [entityPageState, setEntityPageState] = useState<{
    workspaceId: string;
    section: ManagedEntitySection;
    cursor: PageCursor;
    history: PageCursor[];
    nextPage: PageCursor;
  }>({
    workspaceId: '',
    section: config.section,
    cursor: null,
    history: [],
    nextPage: null
  });
  const entityPageCursor =
    entityPageState.workspaceId === activeWorkspaceId && entityPageState.section === config.section ? entityPageState.cursor : null;
  const entityPageHistory =
    entityPageState.workspaceId === activeWorkspaceId && entityPageState.section === config.section ? entityPageState.history : [];
  const entityNextPage =
    entityPageState.workspaceId === activeWorkspaceId && entityPageState.section === config.section ? entityPageState.nextPage : null;
  const title = resourceTitle(config, msg);
  const description = resourceDescription(config, msg);
  const createLabel = resourceCreateLabel(config, msg);
  const searchPlaceholder = resourceSearchPlaceholder(config, msg);
  const managedEntityListFilters = useMemo<ManagedEntityListFilters | undefined>(() => {
    switch (config.section) {
      case 'deployments':
        return {
          agentId: deploymentAgentFilter || undefined,
          status: deploymentStatusFilter
        };
      case 'sessions':
        return {
          agentId: sessionAgentFilter || undefined,
          created: createdFilter,
          deploymentId: sessionDeploymentFilter || undefined,
          includeArchived: sessionStatusFilter === 'all' || sessionStatusFilter === 'terminated',
          statuses: agentDetailStatusValues(sessionStatusFilter)
        };
      case 'environments':
      case 'credential-vaults':
        return {
          includeArchived: genericStatusFilter === 'all'
        };
      case 'memory-stores':
        return {
          created: createdFilter,
          includeArchived: genericStatusFilter === 'all'
        };
    }
  }, [
    config.section,
    createdFilter,
    deploymentAgentFilter,
    deploymentStatusFilter,
    genericStatusFilter,
    sessionAgentFilter,
    sessionDeploymentFilter,
    sessionStatusFilter
  ]);
  const createdFilterOptions = useMemo(
    () => [
      { value: 'all_time' as const, label: msg('managedAgents.filters.allTime', 'All time') },
      { value: 'today' as const, label: msg('managedAgents.filters.today', 'Today') },
      { value: 'last_hour' as const, label: msg('managedAgents.filters.lastHour', 'Last hour') },
      { value: 'last_day' as const, label: msg('managedAgents.filters.lastDay', 'Last day') },
      { value: 'last_7_days' as const, label: msg('managedAgents.filters.last7Days', 'Last 7 days') },
      { value: 'last_30_days' as const, label: msg('managedAgents.filters.last30Days', 'Last 30 days') }
    ],
    [msg]
  );
  const sessionStatusFilterOptions = useMemo(
    () => [
      { value: 'all' as const, label: msg('common.all', 'All') },
      { value: 'active' as const, label: msg('managedAgents.sessions.statusActive', 'Active') },
      { value: 'running' as const, label: msg('managedAgents.sessions.statusRunning', 'Running') },
      { value: 'idle' as const, label: msg('managedAgents.sessions.statusIdle', 'Idle') },
      { value: 'rescheduling' as const, label: msg('managedAgents.sessions.statusRescheduling', 'Rescheduling') },
      { value: 'terminated' as const, label: msg('managedAgents.sessions.statusTerminated', 'Terminated') }
    ],
    [msg]
  );
  const genericStatusFilterOptions = useMemo(
    () => [
      { value: 'all' as const, label: msg('common.all', 'All') },
      { value: 'active' as const, label: msg('common.active', 'Active') }
    ],
    [msg]
  );
  const deploymentAgentFilterOptions = useMemo(() => {
    const options: Array<{ value: string; label: string }> = [{ value: '', label: msg('common.all', 'All') }];
    const seen = new Set(options.map((option) => option.value));
    for (const option of deploymentAgentOptions) {
      if (seen.has(option.id)) {
        continue;
      }
      seen.add(option.id);
      options.push({ value: option.id, label: option.label });
    }
    if (deploymentAgentFilter && !seen.has(deploymentAgentFilter)) {
      options.push({ value: deploymentAgentFilter, label: deploymentAgentFilter });
    }
    return options;
  }, [deploymentAgentFilter, deploymentAgentOptions, msg]);
  const deploymentStatusFilterOptions = useMemo(
    () => [
      { value: 'all' as const, label: msg('common.all', 'All') },
      { value: 'active' as const, label: msg('common.active', 'Active') },
      { value: 'paused' as const, label: msg('managedAgents.filters.paused', 'Paused') }
    ],
    [msg]
  );
  const sessionAgentFilterOptions = useMemo(() => {
    const options: Array<{ value: string; label: string }> = [{ value: '', label: msg('common.all', 'All') }];
    const seen = new Set(options.map((option) => option.value));
    for (const option of deploymentAgentOptions) {
      if (!option.id || seen.has(option.id)) {
        continue;
      }
      seen.add(option.id);
      options.push({ value: option.id, label: option.label });
    }
    if (sessionAgentFilter && !seen.has(sessionAgentFilter)) {
      options.push({ value: sessionAgentFilter, label: sessionAgentFilter });
    }
    return options;
  }, [deploymentAgentOptions, msg, sessionAgentFilter]);
  const sessionDeploymentFilterOptions = useMemo(() => {
    const options: Array<{ value: string; label: string }> = [{ value: '', label: msg('common.all', 'All') }];
    const seen = new Set(options.map((option) => option.value));
    for (const entity of entities) {
      if (entity.type !== 'session') {
        continue;
      }
      const deploymentId = (entity as SessionApiResponse).deployment_id ?? '';
      if (!deploymentId || seen.has(deploymentId)) {
        continue;
      }
      seen.add(deploymentId);
      options.push({ value: deploymentId, label: compactEntityId(deploymentId) });
    }
    if (sessionDeploymentFilter && !seen.has(sessionDeploymentFilter)) {
      options.push({ value: sessionDeploymentFilter, label: compactEntityId(sessionDeploymentFilter) });
    }
    return options;
  }, [entities, msg, sessionDeploymentFilter]);
  const createdFilterValueLabel =
    createdFilterOptions.find((option) => option.value === createdFilter)?.label ?? msg('managedAgents.filters.allTime', 'All time');
  const deploymentAgentValueLabel =
    deploymentAgentFilterOptions.find((option) => option.value === deploymentAgentFilter)?.label ?? msg('common.all', 'All');
  const deploymentStatusValueLabel =
    deploymentStatusFilterOptions.find((option) => option.value === deploymentStatusFilter)?.label ?? msg('common.all', 'All');
  const genericStatusValueLabel =
    genericStatusFilterOptions.find((option) => option.value === genericStatusFilter)?.label ?? msg('common.all', 'All');
  const sessionAgentValueLabel =
    sessionAgentFilterOptions.find((option) => option.value === sessionAgentFilter)?.label ?? msg('common.all', 'All');
  const sessionDeploymentValueLabel =
    sessionDeploymentFilterOptions.find((option) => option.value === sessionDeploymentFilter)?.label ?? msg('common.all', 'All');
  const sessionStatusValueLabel =
    sessionStatusFilterOptions.find((option) => option.value === sessionStatusFilter)?.label ??
    msg('managedAgents.sessions.statusActive', 'Active');

  useEffect(() => {
    setCreatedFilter('all_time');
    setDeploymentAgentOptions([]);
    setDeploymentAgentFilter('');
    setDeploymentStatusFilter('all');
    setGenericStatusFilter(defaultGenericStatusFilter(config.section));
    setOpenFilterMenu(null);
    setSessionAgentFilter('');
    setSessionDeploymentFilter('');
    setSessionStatusFilter('active');
  }, [activeWorkspaceId, config.section]);

  useEffect(() => {
    if (config.section !== 'deployments' && config.section !== 'sessions') {
      return;
    }

    let active = true;

    void listAgents(activeWorkspaceId)
      .then((page) => {
        if (!active) {
          return;
        }
        setDeploymentAgentOptions(
          (page.data ?? []).map((agent) => ({
            id: agent.id,
            label: agent.name || agent.id,
            secondary: agent.id
          }))
        );
      })
      .catch(() => {
        if (active) {
          setDeploymentAgentOptions([]);
        }
      });

    return () => {
      active = false;
    };
  }, [activeWorkspaceId, config.section]);

  useEffect(() => {
    let active = true;
    const pageCursor = entityPageCursor;

    void (async () => {
      await Promise.resolve();
      if (!active) {
        return;
      }

      setLoading(true);
      setLoadError(null);
      try {
        const page = await listManagedEntities(config.section, activeWorkspaceId, pageCursor, managedEntityListFilters);
        if (active) {
          setEntities(page.data ?? []);
          setEntityPageState((current) => ({
            workspaceId: activeWorkspaceId,
            section: config.section,
            cursor: pageCursor,
            history:
              current.workspaceId === activeWorkspaceId && current.section === config.section ? current.history : [],
            nextPage: page.next_page ?? null
          }));
          setLoading(false);
        }
      } catch (error) {
        if (active) {
          setEntities([]);
          setLoadError(errorMessage(error));
          setEntityPageState((current) => ({
            workspaceId: activeWorkspaceId,
            section: config.section,
            cursor: pageCursor,
            history:
              current.workspaceId === activeWorkspaceId && current.section === config.section ? current.history : [],
            nextPage: null
          }));
          setLoading(false);
        }
      }
    })();

    return () => {
      active = false;
    };
  }, [activeWorkspaceId, config.section, entityPageCursor, managedEntityListFilters, refreshKey]);

  useEffect(() => {
    setSelectedEntityIds(new Set());
  }, [activeWorkspaceId, config.section]);

  const visibleEntities = useMemo(() => {
    const normalized = search.trim().toLowerCase();
    return entities.filter((entity) => {
      if (config.section === 'deployments') {
        const deployment = entity as DeploymentApiResponse;
        if (deploymentAgentFilter && entityAgentId(entity) !== deploymentAgentFilter) {
          return false;
        }
        if (deploymentStatusFilter === 'active' && (entity.archived_at || deployment.status !== 'active')) {
          return false;
        }
        if (deploymentStatusFilter === 'paused' && (entity.archived_at || deployment.status !== 'paused')) {
          return false;
        }
      }
      if (!normalized) {
        return true;
      }
      return [entity.id, entityDisplayName(config.section, entity), entityStatusLabel(entity), entityAgentLabel(entity)]
        .join(' ')
        .toLowerCase()
        .includes(normalized);
    });
  }, [config.section, deploymentAgentFilter, deploymentStatusFilter, entities, search]);
  const visibleEntityIds = useMemo(() => visibleEntities.map((entity) => entity.id), [visibleEntities]);
  const hasSelectionColumn = config.columns.some((column) => !column);
  const allVisibleEntitiesSelected = hasSelectionColumn && visibleEntityIds.length > 0 && visibleEntityIds.every((id) => selectedEntityIds.has(id));
  const someVisibleEntitiesSelected = hasSelectionColumn && visibleEntityIds.some((id) => selectedEntityIds.has(id));

  useEffect(() => {
    setSelectedEntityIds((current) => {
      const entityIds = new Set(entities.map((entity) => entity.id));
      const next = new Set([...current].filter((id) => entityIds.has(id)));
      return next.size === current.size ? current : next;
    });
  }, [entities]);

  const toggleAllVisibleEntities = () => {
    setSelectedEntityIds((current) => {
      const next = new Set(current);
      if (visibleEntityIds.every((id) => next.has(id))) {
        visibleEntityIds.forEach((id) => next.delete(id));
      } else {
        visibleEntityIds.forEach((id) => next.add(id));
      }
      return next;
    });
  };

  const toggleEntitySelection = (id: string) => {
    setSelectedEntityIds((current) => {
      const next = new Set(current);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const resetEntityPage = () => {
    setEntityPageState({
      workspaceId: activeWorkspaceId,
      section: config.section,
      cursor: null,
      history: [],
      nextPage: null
    });
  };

  const reload = (resetPage = false) => {
    if (resetPage) {
      resetEntityPage();
    }
    setRefreshKey((value) => value + 1);
  };

  const handleDeploymentAgentFilterChange = (value: string) => {
    setDeploymentAgentFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const handleDeploymentStatusFilterChange = (value: DeploymentStatusFilter) => {
    setDeploymentStatusFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const handleCreatedFilterChange = (value: AgentDetailCreatedFilter) => {
    setCreatedFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const handleGenericStatusFilterChange = (value: AgentStatusFilter) => {
    setGenericStatusFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const handleSessionAgentFilterChange = (value: string) => {
    setSessionAgentFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const handleSessionDeploymentFilterChange = (value: string) => {
    setSessionDeploymentFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const handleSessionStatusFilterChange = (value: AgentDetailStatusFilter) => {
    setSessionStatusFilter(value);
    setOpenFilterMenu(null);
    resetEntityPage();
  };

  const renderManagedFilter = (filter: string) => {
    switch (filter) {
      case 'Created  All time':
        return (
          <AgentFilterDropdown
            key={`${config.section}-created`}
            label={msg('managedAgents.filters.created', 'Created')}
            valueLabel={createdFilterValueLabel}
            options={createdFilterOptions}
            value={createdFilter}
            menu="created"
            open={openFilterMenu === 'created'}
            menuWidthClass="w-[220px]"
            onOpenChange={setOpenFilterMenu}
            onSelect={handleCreatedFilterChange}
          />
        );
      case 'Agent  All':
        return (
          <AgentFilterDropdown
            key={`${config.section}-agent`}
            label={msg('managedAgents.common.agent', 'Agent')}
            valueLabel={config.section === 'sessions' ? sessionAgentValueLabel : deploymentAgentValueLabel}
            options={config.section === 'sessions' ? sessionAgentFilterOptions : deploymentAgentFilterOptions}
            value={config.section === 'sessions' ? sessionAgentFilter : deploymentAgentFilter}
            menu="agent"
            open={openFilterMenu === 'agent'}
            menuWidthClass={config.section === 'sessions' ? 'w-[240px]' : 'w-[280px]'}
            onOpenChange={setOpenFilterMenu}
            onSelect={config.section === 'sessions' ? handleSessionAgentFilterChange : handleDeploymentAgentFilterChange}
          />
        );
      case 'Deployment  All':
        return (
          <AgentFilterDropdown
            key={`${config.section}-deployment`}
            label={msg('managedAgents.deployments.kind', 'Deployment')}
            valueLabel={sessionDeploymentValueLabel}
            options={sessionDeploymentFilterOptions}
            value={sessionDeploymentFilter}
            menu="deployment"
            open={openFilterMenu === 'deployment'}
            menuWidthClass="w-[240px]"
            onOpenChange={setOpenFilterMenu}
            onSelect={handleSessionDeploymentFilterChange}
          />
        );
      case 'Status  Active':
        if (config.section === 'sessions') {
          return (
            <AgentFilterDropdown
              key={`${config.section}-status`}
              label={msg('managedAgents.filters.status', 'Status')}
              valueLabel={sessionStatusValueLabel}
              options={sessionStatusFilterOptions}
              value={sessionStatusFilter}
              menu="status"
              open={openFilterMenu === 'status'}
              menuWidthClass="w-[220px]"
              onOpenChange={setOpenFilterMenu}
              onSelect={handleSessionStatusFilterChange}
            />
          );
        }
        return (
          <AgentFilterDropdown
            key={`${config.section}-status`}
            label={msg('managedAgents.filters.status', 'Status')}
            valueLabel={genericStatusValueLabel}
            options={genericStatusFilterOptions}
            value={genericStatusFilter}
            menu="status"
            open={openFilterMenu === 'status'}
            menuWidthClass="w-[220px]"
            onOpenChange={setOpenFilterMenu}
            onSelect={handleGenericStatusFilterChange}
          />
        );
      case 'Status  All':
        if (config.section === 'deployments') {
          return (
            <AgentFilterDropdown
              key={`${config.section}-status`}
              label={msg('managedAgents.filters.status', 'Status')}
              valueLabel={deploymentStatusValueLabel}
              options={deploymentStatusFilterOptions}
              value={deploymentStatusFilter}
              menu="status"
              open={openFilterMenu === 'status'}
              menuWidthClass="w-[220px]"
              onOpenChange={setOpenFilterMenu}
              onSelect={handleDeploymentStatusFilterChange}
            />
          );
        }
        return (
          <AgentFilterDropdown
            key={`${config.section}-status`}
            label={msg('managedAgents.filters.status', 'Status')}
            valueLabel={genericStatusValueLabel}
            options={genericStatusFilterOptions}
            value={genericStatusFilter}
            menu="status"
            open={openFilterMenu === 'status'}
            menuWidthClass="w-[220px]"
            onOpenChange={setOpenFilterMenu}
            onSelect={handleGenericStatusFilterChange}
          />
        );
      default:
        return null;
    }
  };

  const goToNextEntityPage = () => {
    if (!entityNextPage) {
      return;
    }
    setEntityPageState({
      workspaceId: activeWorkspaceId,
      section: config.section,
      cursor: entityNextPage,
      history: [...entityPageHistory, entityPageCursor],
      nextPage: null
    });
  };

  const goToPreviousEntityPage = () => {
    if (!entityPageHistory.length) {
      return;
    }
    setEntityPageState({
      workspaceId: activeWorkspaceId,
      section: config.section,
      cursor: entityPageHistory[entityPageHistory.length - 1],
      history: entityPageHistory.slice(0, -1),
      nextPage: null
    });
  };

  const handleSubmitEntity = async (values: ManagedEntityFormValues, entity?: ManagedEntityApiResponse) => {
    setMutationError(null);
    if (entity) {
      const updated = await updateManagedEntity(config.section, entity.id, values, activeWorkspaceId);
      setEntities((current) => current.map((item) => (item.id === updated.id ? updated : item)));
      toast.success(managedToastMessage(config.section, 'updated', msg));
      return;
    }
    const created = await createManagedEntity(config.section, values, activeWorkspaceId);
    setEntities((current) => [created, ...current.filter((item) => item.id !== created.id)]);
    toast.success(managedToastMessage(config.section, 'created', msg));
  };

  const handleConfirm = async () => {
    if (!confirmState) {
      return;
    }
    const { action, entity } = confirmState;
    setBusyAction(`${action}:${entity.id}`);
    setMutationError(null);
    try {
      if (action === 'archive') {
        await archiveManagedEntity(config.section, entity.id, activeWorkspaceId);
      } else {
        await deleteManagedEntity(config.section, entity.id, activeWorkspaceId);
      }
      if (action === 'archive' && config.section === 'deployments') {
        const archivedAt = new Date().toISOString();
        setEntities((current) =>
          current.map((item) => (item.id === entity.id ? { ...item, archived_at: archivedAt, updated_at: archivedAt } : item))
        );
      } else {
        setEntities((current) => current.filter((item) => item.id !== entity.id));
      }
      toast.success(managedToastMessage(config.section, action === 'archive' ? 'archived' : 'deleted', msg));
      setConfirmState(null);
    } catch (error) {
      setMutationError(errorMessage(error));
    } finally {
      setBusyAction(null);
    }
  };

  const handleDeploymentAction = async (action: 'run' | 'pause' | 'unpause', entity: ManagedEntityApiResponse) => {
    setBusyAction(`${action}:${entity.id}`);
    setMutationError(null);
    try {
      if (action === 'run') {
        await runDeployment(entity.id, activeWorkspaceId);
        toast.success(msg('managedAgents.deployments.toastRunStarted', 'Deployment run started'));
      } else {
        const updated = action === 'pause' ? await pauseDeployment(entity.id, activeWorkspaceId) : await unpauseDeployment(entity.id, activeWorkspaceId);
        setEntities((current) => current.map((item) => (item.id === updated.id ? updated : item)));
        toast.success(action === 'pause' ? msg('managedAgents.deployments.toastPaused', 'Deployment paused') : msg('managedAgents.deployments.toastUnpaused', 'Deployment unpaused'));
      }
    } catch (error) {
      setMutationError(errorMessage(error));
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <section className="relative min-h-[calc(100vh-48px)] text-foreground">
      <header className="mb-5 flex items-start justify-between gap-6">
        <div>
          <h1 className="text-[28px] font-semibold leading-tight text-foreground">{title}</h1>
          <p className="mt-2 max-w-[760px] text-[15px] leading-5 text-muted-foreground">{description}</p>
        </div>
        {createLabel ? (
          <Button
            type="button"
            className="h-9 shrink-0"
            onClick={() => setDialogState({ mode: 'create' })}
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
          onChange={setSearch}
        />
        {config.filters.map((filter) => renderManagedFilter(filter))}
      </div>

      {loadError ? <ManagedErrorAlert className="mb-3">{loadError}</ManagedErrorAlert> : null}
      {mutationError ? <ManagedErrorAlert className="mb-3">{mutationError}</ManagedErrorAlert> : null}

      <div className="overflow-visible">
        <Table className="table-fixed border-separate border-spacing-y-px text-left">
          <TableHeader>
            <TableRow className="border-0 text-muted-foreground hover:bg-transparent">
              {config.columns.map((column) => (
                <TableHead key={column || 'select'} className={clsx('h-10 px-3 text-muted-foreground', columnWidth(config.section, column))}>
                  {column ? (
                    managedColumnLabel(column, msg)
                  ) : (
                    <AgentSelectionCheckbox
                      checked={allVisibleEntitiesSelected}
                      indeterminate={!allVisibleEntitiesSelected && someVisibleEntitiesSelected}
                      disabled={!visibleEntityIds.length || loading}
                      label={msg('managedAgents.common.selectAllRows', 'Select all rows')}
                      onClick={toggleAllVisibleEntities}
                    />
                  )}
                </TableHead>
              ))}
              <TableHead className="h-10 w-[48px] px-2 text-muted-foreground" aria-label={managedColumnLabel('Actions', msg)} />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow className="border-0 hover:bg-transparent">
                <TableCell colSpan={config.columns.length + 1} className="h-[280px] text-center text-sm text-muted-foreground">
                  {managedMessage(msg, config.section, 'loading', `Loading ${config.title.toLowerCase()}...`)}
                </TableCell>
              </TableRow>
            ) : (
              visibleEntities.map((entity) => (
                <ManagedEntityRow
                  key={entity.id}
                  config={config}
                  entity={entity}
                  workspaceId={activeWorkspaceId}
                  busyAction={busyAction}
                  selected={selectedEntityIds.has(entity.id)}
                  onCopy={() => void copyText(entity.id)}
                  onToggleSelect={() => toggleEntitySelection(entity.id)}
                  onEdit={() => setDialogState({ mode: 'edit', entity })}
                  onArchive={() => setConfirmState({ action: 'archive', entity })}
                  onDelete={() => setConfirmState({ action: 'delete', entity })}
                  onRunDeployment={() => void handleDeploymentAction('run', entity)}
                  onPauseDeployment={() => void handleDeploymentAction('pause', entity)}
                  onUnpauseDeployment={() => void handleDeploymentAction('unpause', entity)}
                />
              ))
            )}
          </TableBody>
        </Table>

        {!loading && !visibleEntities.length ? <EmptyState config={config} /> : null}
      </div>

      <div className="mt-9 flex items-center gap-2">
        <Button
          type="button"
          disabled={!entityPageHistory.length || loading}
          variant="outline"
          size="icon-lg"
          aria-label={msg('pagination.previousPage', 'Previous page')}
          onClick={goToPreviousEntityPage}
        >
          <ChevronLeft className="size-4" aria-hidden />
        </Button>
        <Button
          type="button"
          disabled={!entityNextPage || loading}
          variant="outline"
          size="icon-lg"
          aria-label={msg('pagination.nextPage', 'Next page')}
          onClick={goToNextEntityPage}
        >
          <ChevronRight className="size-4" aria-hidden />
        </Button>
      </div>

      {dialogState ? (
        <ManagedEntityDialog
          section={config.section}
          title={dialogState.mode === 'create' ? createLabel || msg('managedAgents.common.createEntity', 'Create {label}', { label: entityKindLabel(config.section, msg) }) : msg('managedAgents.common.editEntity', 'Edit {label}', { label: entityKindLabel(config.section, msg) })}
          entity={dialogState.entity}
          workspaceId={activeWorkspaceId}
          onClose={() => setDialogState(null)}
          onSubmit={async (values) => {
            const resetPage = dialogState.mode === 'create';
            await handleSubmitEntity(values, dialogState.entity);
            setDialogState(null);
            reload(resetPage);
          }}
        />
      ) : null}

      {confirmState ? (
        <ConfirmEntityDialog
          action={confirmState.action}
          section={config.section}
          entity={confirmState.entity}
          busy={busyAction === `${confirmState.action}:${confirmState.entity.id}`}
          onCancel={() => setConfirmState(null)}
          onConfirm={() => void handleConfirm()}
        />
      ) : null}
    </section>
  );
}

export function ManagedEntityRow({
  config,
  entity,
  workspaceId,
  busyAction,
  selected,
  onCopy,
  onToggleSelect,
  onEdit,
  onArchive,
  onDelete,
  onRunDeployment,
  onPauseDeployment,
  onUnpauseDeployment
}: {
  config: ResourceConfig & { section: ManagedEntitySection };
  entity: ManagedEntityApiResponse;
  workspaceId: string;
  busyAction: string | null;
  selected: boolean;
  onCopy: () => void;
  onToggleSelect: () => void;
  onEdit: () => void;
  onArchive: () => void;
  onDelete: () => void;
  onRunDeployment: () => void;
  onPauseDeployment: () => void;
  onUnpauseDeployment: () => void;
}) {
  const { msg } = useI18n();
  const cells = cellsForEntity(config.section, entity);
  const archived = Boolean(entity.archived_at);
  const busy = Boolean(busyAction?.endsWith(`:${entity.id}`));
  const deployment = config.section === 'deployments' ? (entity as DeploymentApiResponse) : null;
  const paused = deployment?.status === 'paused';

  return (
    <TableRow className={clsx('group border-0 text-foreground', selected ? 'bg-secondary' : 'hover:bg-accent')}>
      {config.columns.map((column, index) => {
        const content =
          column === 'Name' ? (
            <a
              href={managedEntityDetailHref(workspaceId, config.section, entity.id)}
              className="truncate text-foreground underline-offset-4 hover:underline"
            >
              {cells[column]}
            </a>
          ) : column ? (
            cells[column]
          ) : (
            <AgentSelectionCheckbox
              checked={selected}
              label={msg('managedAgents.common.selectRow', 'Select {name}', { name: entityDisplayName(config.section, entity) })}
              onClick={onToggleSelect}
            />
          );
        return (
          <TableCell
            key={column || 'select'}
            className={clsx(
              'h-[45px] truncate px-3 align-middle',
              index === 0 && 'rounded-l-lg'
            )}
          >
            {content}
          </TableCell>
        );
      })}
      <TableCell className="h-[45px] rounded-r-lg px-2">
        <div className="flex justify-end">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={msg('managedAgents.common.moreActions', 'More actions')}
                  disabled={busy}
                  className="text-foreground hover:bg-secondary disabled:cursor-wait disabled:text-muted-foreground/70"
                />
              }
            >
              <MoreVertical className="size-4" aria-hidden />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-[188px]">
              <DropdownMenuItem onClick={onCopy}>
                <Copy className="size-4" aria-hidden />
                {msg('common.copyId', 'Copy ID')}
              </DropdownMenuItem>
              <DropdownMenuItem disabled={archived} onClick={onEdit}>
                <Pencil className="size-4" aria-hidden />
                {entityActionLabel('edit', config.section, msg)}
              </DropdownMenuItem>
              {config.section === 'deployments' ? (
                <>
                  <DropdownMenuItem disabled={archived || paused} onClick={onRunDeployment}>
                    <Play className="size-4" aria-hidden />
                    {msg('managedAgents.deployments.runDeployment', 'Run deployment')}
                  </DropdownMenuItem>
                  <DropdownMenuItem disabled={archived} onClick={paused ? onUnpauseDeployment : onPauseDeployment}>
                    {paused ? <Play className="size-4" aria-hidden /> : <Archive className="size-4" aria-hidden />}
                    {paused ? msg('managedAgents.deployments.unpauseDeployment', 'Unpause deployment') : msg('managedAgents.deployments.pauseDeployment', 'Pause deployment')}
                  </DropdownMenuItem>
                </>
              ) : null}
              <DropdownMenuItem disabled={archived} onClick={onArchive}>
                <Archive className="size-4" aria-hidden />
                {entityActionLabel('archive', config.section, msg)}
              </DropdownMenuItem>
              {config.section !== 'deployments' ? (
                <DropdownMenuItem variant="destructive" onClick={onDelete}>
                  <X className="size-4" aria-hidden />
                  {entityActionLabel('delete', config.section, msg)}
                </DropdownMenuItem>
              ) : null}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TableCell>
    </TableRow>
  );
}
