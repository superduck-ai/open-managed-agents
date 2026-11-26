import { Activity, AlertCircle, Box, Info, KeyRound, Plus, Search, Settings, Trash2, Webhook } from 'lucide-react';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useI18n } from '../../shared/i18n';
import { useFormatters } from '../../shared/i18n/formatters';
import { CreateWorkspaceDialog } from '../../shared/workspaces/CreateWorkspaceDialog';
import { ArchiveWorkspaceDialog } from '../../shared/workspaces/ArchiveWorkspaceDialog';
import { EditWorkspaceDialog } from '../../shared/workspaces/EditWorkspaceDialog';
import { archiveConsoleWorkspace, listConsoleWorkspaces, updateConsoleWorkspace } from '../../shared/workspaces/api';
import {
  buildCreateWorkspaceInput,
  workspaceApiKeysPath,
  workspaceTelemetryPath,
  workspaceWebhooksPath,
  workspaceColor,
} from '../../shared/workspaces/presentation';
import { useWorkspace } from '../../shared/workspaces/context';
import type { Workspace } from '../../shared/workspaces/api';
import { workspaceRuntimeBadgeLabel } from '../../shared/workspaces/runtime';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  MoreActionsButton,
  dataTableActionsColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableIdColumnClassName,
  dataTableNumericColumnClassName,
  dataTablePrimaryColumnClassName,
  dataTableTimeColumnClassName,
} from '../../shared/ui/data-table-interactions';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import { ResourceListState } from '../../shared/ui/resource-list-state';
import { ResourcePageHeader } from '../../shared/ui/resource-page-header';
import { ResourceStatusBadge } from '../../shared/ui/resource-status-badge';
import { RelativeTimeText } from '../../shared/ui/resource-time-cell';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Input } from '../../shared/ui/input';
import { Skeleton } from '../../shared/ui/skeleton';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../shared/ui/tooltip';
import { toast } from '../../shared/ui/sonner';

export function WorkspacesSettingsPage() {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const {
    canManageWorkspaces,
    orgUuid,
    workspaces,
    activeWorkspaceId,
    createWorkspace,
    error: workspaceError,
    isLoading: workspaceLoading,
    refreshWorkspaces,
  } = useWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<'active' | 'archived'>('active');
  const archivedQuery = useQuery({
    queryKey: ['console', 'archived-workspaces', orgUuid],
    queryFn: () => listConsoleWorkspaces(orgUuid ?? '', true),
    enabled: Boolean(orgUuid) && !workspaceLoading && statusFilter === 'archived',
    retry: false,
  });
  const error = workspaceError ?? (statusFilter === 'archived' ? archivedQuery.error : null);
  const isLoading = workspaceLoading || (statusFilter === 'archived' && archivedQuery.isLoading);
  const [editingWorkspace, setEditingWorkspace] = useState<Workspace | null>(null);
  const [archivingWorkspace, setArchivingWorkspace] = useState<Workspace | null>(null);
  const [archivePending, setArchivePending] = useState(false);
  const [archiveError, setArchiveError] = useState('');
  const statusOptions = [
    { value: 'active', label: msg('settings.workspaces.statusActive', 'Active') },
    { value: 'archived', label: msg('settings.workspaces.statusArchived', 'Archived') },
  ];

  const handleCreate = async (name: string, displayColor: string, runtimeProfileId?: string) => {
    await createWorkspace(buildCreateWorkspaceInput(name, displayColor, runtimeProfileId));
  };

  const handleUpdate = async (name: string, displayColor: string) => {
    if (!editingWorkspace || !orgUuid) {
      return;
    }
    await updateConsoleWorkspace(orgUuid, editingWorkspace.id, { name, display_color: displayColor });
    await refreshWorkspaces();
    setEditingWorkspace(null);
    toast.success(msg('workspace.update.success', 'Workspace updated.'));
  };

  const handleArchive = async () => {
    if (!archivingWorkspace || !orgUuid) {
      return;
    }
    setArchivePending(true);
    setArchiveError('');
    try {
      await archiveConsoleWorkspace(orgUuid, archivingWorkspace.id);
      setArchivingWorkspace(null);
      await refreshWorkspaces();
      await archivedQuery.refetch();
      toast.success(msg('workspace.archive.success', 'Workspace archived.'));
    } catch (archiveError) {
      setArchiveError(
        archiveError &&
          typeof archiveError === 'object' &&
          'message' in archiveError &&
          typeof archiveError.message === 'string'
          ? archiveError.message
          : msg('workspace.archive.error', 'Failed to archive workspace.'),
      );
    } finally {
      setArchivePending(false);
    }
  };

  const handleRetry = async () => {
    setRetrying(true);
    try {
      await refreshWorkspaces();
      if (statusFilter === 'archived') await archivedQuery.refetch();
    } finally {
      setRetrying(false);
    }
  };

  const keyword = search.trim().toLowerCase();
  const visibleWorkspaces = filterWorkspaces(
    statusFilter === 'archived' ? (archivedQuery.data ?? []) : workspaces,
    keyword,
    statusFilter,
  );

  return (
    <TooltipProvider>
      <section className="w-full">
        <ResourcePageHeader
          title={msg('nav.workspaces', 'Workspaces')}
          titleAdornment={
            <>
              <Badge variant="secondary" className="min-w-5 rounded-full px-1.5">
                {workspaces.length}
              </Badge>
              <Tooltip>
                <TooltipTrigger className="cursor-help text-muted-foreground">
                  <Info className="size-4" aria-hidden />
                  <span className="sr-only">
                    {msg(
                      'settings.workspaces.overview',
                      'Workspaces are collaborative spaces where teams can separate API resources by use case.',
                    )}
                  </span>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  {msg(
                    'settings.workspaces.overview',
                    'Workspaces are collaborative spaces where teams can separate API resources by use case.',
                  )}
                </TooltipContent>
              </Tooltip>
            </>
          }
          actions={
            orgUuid && canManageWorkspaces ? (
              <CreateWorkspaceDialog
                open={createOpen}
                onOpenChange={setCreateOpen}
                onCreate={handleCreate}
                trigger={
                  <Button type="button">
                    <Plus className="size-4" aria-hidden />
                    {msg('workspace.create.title', 'Create workspace')}
                  </Button>
                }
              />
            ) : null
          }
        />
        <div className="space-y-4">
          {!orgUuid ? (
            <Alert>
              <AlertCircle className="size-4" aria-hidden />
              <AlertDescription>
                {msg(
                  'settings.workspaces.noOrganization',
                  'No organization is available for workspace management in this session.',
                )}
              </AlertDescription>
            </Alert>
          ) : error ? (
            <Alert variant="destructive" className="[overflow-wrap:anywhere]">
              <AlertCircle className="size-4" aria-hidden />
              <AlertTitle>{msg('settings.workspaces.loadError', 'Workspaces could not be loaded.')}</AlertTitle>
              <AlertDescription className="gap-3">
                <p>{readableError(error)}</p>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={retrying}
                  onClick={() => void handleRetry()}
                >
                  {retrying ? msg('common.loading', 'Loading...') : msg('common.retry', 'Retry')}
                </Button>
              </AlertDescription>
            </Alert>
          ) : isLoading ? (
            <div aria-label={msg('workspace.loading', 'Loading workspaces...')} className="space-y-3">
              {Array.from({ length: 3 }).map((_, index) => (
                <div
                  key={index}
                  className="grid gap-3 rounded-lg border-[0.5px] border-border p-4 md:grid-cols-[1.5fr_1fr_auto]"
                >
                  <div className="flex items-center gap-3">
                    <Skeleton className="size-9 rounded-md" />
                    <div className="space-y-2">
                      <Skeleton className="h-4 w-32" />
                      <Skeleton className="h-3 w-24" />
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Skeleton className="h-4 w-16" />
                    <Skeleton className="h-3 w-28" />
                  </div>
                  <div className="flex gap-2">
                    <Skeleton className="h-7 w-20" />
                    <Skeleton className="h-7 w-20" />
                  </div>
                </div>
              ))}
            </div>
          ) : workspaces.length > 0 ? (
            <>
              <div className="flex flex-wrap items-center gap-3">
                <div className="relative w-full max-w-[320px]">
                  <Search
                    className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
                    aria-hidden
                  />
                  <Input
                    type="search"
                    value={search}
                    onChange={(event) => setSearch(event.target.value)}
                    placeholder={msg('settings.workspaces.searchPlaceholder', 'Search workspaces')}
                    aria-label={msg('settings.workspaces.searchPlaceholder', 'Search workspaces')}
                    className="h-9 pl-9"
                  />
                </div>
                <Select
                  value={statusFilter}
                  items={statusOptions}
                  onValueChange={(next) => {
                    if (next === 'active' || next === 'archived') {
                      setStatusFilter(next);
                    }
                  }}
                >
                  <SelectTrigger aria-label={msg('settings.workspaces.statusFilter', 'Status')} className="w-[150px]">
                    <span className="text-muted-foreground">{msg('settings.workspaces.statusFilter', 'Status')}</span>
                    <SelectValue className="font-medium text-foreground">
                      <span>{statusOptions.find((option) => option.value === statusFilter)?.label}</span>
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectItem value="active" label={msg('settings.workspaces.statusActive', 'Active')}>
                      {msg('settings.workspaces.statusActive', 'Active')}
                    </SelectItem>
                    <SelectItem value="archived" label={msg('settings.workspaces.statusArchived', 'Archived')}>
                      {msg('settings.workspaces.statusArchived', 'Archived')}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <section className="min-w-0">
                {visibleWorkspaces.length === 0 ? (
                  <ResourceListState
                    icon={Search}
                    title={msg('settings.workspaces.noMatches', 'No workspaces match the current search or filter.')}
                  />
                ) : (
                  <Table aria-label={msg('nav.workspaces', 'Workspaces')} className={dataTableClassName}>
                    <TableHeader>
                      <TableRow className={dataTableHeaderRowClassName}>
                        <TableHead className={`${dataTableHeaderCellClassName} ${dataTablePrimaryColumnClassName}`}>
                          {msg('settings.workspaces.workspace', 'Workspace')}
                        </TableHead>
                        <TableHead className={`${dataTableHeaderCellClassName} ${dataTableIdColumnClassName}`}>
                          ID
                        </TableHead>
                        <TableHead className={`${dataTableHeaderCellClassName} ${dataTableTimeColumnClassName}`}>
                          {msg('settings.workspaces.created', 'Created')}
                        </TableHead>
                        <TableHead className={`${dataTableHeaderCellClassName} ${dataTableNumericColumnClassName}`}>
                          {msg('settings.workspaces.apiKeysCount', 'API keys')}
                        </TableHead>
                        <TableHead className={`${dataTableHeaderCellClassName} ${dataTableActionsColumnClassName}`}>
                          <span className="sr-only">{msg('common.actions', 'Actions')}</span>
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {visibleWorkspaces.map((workspace) => (
                        <DataTableRow key={workspace.id}>
                          <DataTableCell edge="start" className={`${dataTablePrimaryColumnClassName} py-2`}>
                            <div className="flex min-w-0 max-w-[340px] items-center gap-3">
                              <span className="grid size-8 shrink-0 place-items-center rounded-md border-[0.5px] border-border bg-muted/40">
                                <Box className="size-4" style={{ color: workspaceColor(workspace) }} aria-hidden />
                              </span>
                              <div className="flex min-w-0 flex-wrap items-center gap-2">
                                <span className="truncate font-medium text-foreground" title={workspace.name}>
                                  {workspace.name}
                                </span>
                                {workspace.is_default ? (
                                  <Tooltip>
                                    <TooltipTrigger className="cursor-help text-muted-foreground">
                                      <Info className="size-3.5" aria-hidden />
                                      <span className="sr-only">
                                        {msg(
                                          'settings.workspaces.defaultLocked',
                                          'The default workspace is not editable and cannot be removed',
                                        )}
                                      </span>
                                    </TooltipTrigger>
                                    <TooltipContent>
                                      {msg(
                                        'settings.workspaces.defaultLocked',
                                        'The default workspace is not editable and cannot be removed',
                                      )}
                                    </TooltipContent>
                                  </Tooltip>
                                ) : null}
                                {workspace.id === activeWorkspaceId ? (
                                  <ResourceStatusBadge tone="info">
                                    {msg('settings.workspaces.current', 'Current')}
                                  </ResourceStatusBadge>
                                ) : null}
                                {workspaceRuntimeBadgeLabel(workspace.runtime) ? (
                                  <Badge variant="outline">{workspaceRuntimeBadgeLabel(workspace.runtime)}</Badge>
                                ) : null}
                              </div>
                            </div>
                          </DataTableCell>
                          <DataTableCell className={dataTableIdColumnClassName}>
                            <CopyIdCell
                              value={workspace.id}
                              ariaLabel={msg('common.copyValue', 'Copy {value}', { value: workspace.id })}
                            />
                          </DataTableCell>
                          <DataTableCell className={dataTableTimeColumnClassName}>
                            <RelativeTimeText value={workspace.created_at} emptyLabel="–" />
                          </DataTableCell>
                          <DataTableCell className={`${dataTableNumericColumnClassName} text-sm text-foreground`}>
                            {formatters.number(workspace.api_keys_count ?? 0)}
                          </DataTableCell>
                          <DataTableCell edge="end" className={dataTableActionsColumnClassName}>
                            <div className="flex justify-end">
                              <DropdownMenu>
                                <DropdownMenuTrigger
                                  render={
                                    <MoreActionsButton
                                      label={msg('settings.workspaces.rowActions', 'Workspace actions')}
                                    />
                                  }
                                />
                                <DropdownMenuContent align="end" className="w-48">
                                  {canManageWorkspaces && !workspace.is_default && !workspace.archived_at ? (
                                    <DropdownMenuItem onClick={() => setEditingWorkspace(workspace)}>
                                      <Settings className="size-4" aria-hidden />
                                      <span>{msg('settings.workspaces.editDetails', 'Edit details')}</span>
                                    </DropdownMenuItem>
                                  ) : null}
                                  <DropdownMenuItem
                                    onClick={() => window.location.assign(workspaceApiKeysPath(workspace.id))}
                                  >
                                    <KeyRound className="size-4" aria-hidden />
                                    <span>{msg('settings.workspaces.manageApiKeys', 'Manage API keys')}</span>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    onClick={() => window.location.assign(workspaceWebhooksPath(workspace.id))}
                                  >
                                    <Webhook className="size-4" aria-hidden />
                                    <span>{msg('nav.webhooks', 'Webhooks')}</span>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem
                                    onClick={() => window.location.assign(workspaceTelemetryPath(workspace.id))}
                                  >
                                    <Activity className="size-4" aria-hidden />
                                    <span>{msg('nav.telemetry', 'OpenTelemetry')}</span>
                                  </DropdownMenuItem>
                                  {canManageWorkspaces && !workspace.is_default && !workspace.archived_at ? (
                                    <DropdownMenuItem
                                      variant="destructive"
                                      onClick={() => {
                                        setArchiveError('');
                                        setArchivingWorkspace(workspace);
                                      }}
                                    >
                                      <Trash2 className="size-4" aria-hidden />
                                      <span>{msg('settings.workspaces.archiveWorkspace', 'Archive workspace')}</span>
                                    </DropdownMenuItem>
                                  ) : null}
                                </DropdownMenuContent>
                              </DropdownMenu>
                            </div>
                          </DataTableCell>
                        </DataTableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </section>
            </>
          ) : (
            <Alert>
              <AlertCircle className="size-4" aria-hidden />
              <AlertDescription>
                {msg('settings.workspaces.empty', 'No workspaces are available for this organization yet.')}
              </AlertDescription>
            </Alert>
          )}
        </div>

        <EditWorkspaceDialog
          open={Boolean(editingWorkspace)}
          workspace={editingWorkspace}
          onClose={() => setEditingWorkspace(null)}
          onSave={handleUpdate}
        />
        <ArchiveWorkspaceDialog
          open={Boolean(archivingWorkspace)}
          workspaceName={archivingWorkspace?.name ?? ''}
          pending={archivePending}
          error={archiveError}
          onClose={() => {
            setArchiveError('');
            setArchivingWorkspace(null);
          }}
          onConfirm={() => void handleArchive()}
        />
      </section>
    </TooltipProvider>
  );
}

export function filterWorkspaces(workspaces: Workspace[], keyword: string, status: 'active' | 'archived'): Workspace[] {
  return workspaces.filter((workspace) => {
    if ((status === 'archived') !== Boolean(workspace.archived_at)) {
      return false;
    }
    if (!keyword) {
      return true;
    }
    return workspace.name.toLowerCase().includes(keyword) || workspace.id.toLowerCase().includes(keyword);
  });
}

function readableError(error: unknown) {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return 'Open Managed Agents could not reach the workspace directory.';
}
