import { AlertCircle, Box, Info, KeyRound, MoreVertical, Plus, Search, Settings, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { useI18n } from '../../shared/i18n';
import { useFormatters } from '../../shared/i18n/formatters';
import { CreateWorkspaceDialog } from '../../shared/workspaces/CreateWorkspaceDialog';
import { ArchiveWorkspaceDialog } from '../../shared/workspaces/ArchiveWorkspaceDialog';
import { EditWorkspaceDialog } from '../../shared/workspaces/EditWorkspaceDialog';
import { archiveConsoleWorkspace, updateConsoleWorkspace } from '../../shared/workspaces/api';
import { buildCreateWorkspaceInput, workspaceApiKeysPath, workspaceColor } from '../../shared/workspaces/presentation';
import { useWorkspace } from '../../shared/workspaces/context';
import type { Workspace } from '../../shared/workspaces/api';
import { Alert, AlertDescription, AlertTitle } from '../../shared/ui/alert';
import {
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  DataTableCell,
  DataTableRow,
} from '../../shared/ui/data-table-interactions';
import { Badge } from '../../shared/ui/badge';
import { Button } from '../../shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '../../shared/ui/dropdown-menu';
import { Input } from '../../shared/ui/input';
import { Skeleton } from '../../shared/ui/skeleton';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../../shared/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
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
    error,
    isLoading,
    refreshWorkspaces,
  } = useWorkspace();
  const [createOpen, setCreateOpen] = useState(false);
  const [retrying, setRetrying] = useState(false);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<'active' | 'archived'>('active');
  const [editingWorkspace, setEditingWorkspace] = useState<Workspace | null>(null);
  const [archivingWorkspace, setArchivingWorkspace] = useState<Workspace | null>(null);
  const [archivePending, setArchivePending] = useState(false);
  const [archiveError, setArchiveError] = useState('');
  const statusOptions = [
    { value: 'active', label: msg('settings.workspaces.statusActive', 'Active') },
    { value: 'archived', label: msg('settings.workspaces.statusArchived', 'Archived') },
  ];

  const handleCreate = async (name: string, displayColor: string) => {
    await createWorkspace(buildCreateWorkspaceInput(name, displayColor));
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
    } finally {
      setRetrying(false);
    }
  };

  const keyword = search.trim().toLowerCase();
  const visibleWorkspaces = filterWorkspaces(workspaces, keyword, statusFilter);

  return (
    <TooltipProvider>
      <section className="w-full">
        <div className="mb-6 flex min-h-9 items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-2">
            <h1 className="flex min-w-0 items-center gap-2 text-xl font-semibold tracking-normal text-foreground">
              <span>{msg('nav.workspaces', 'Workspaces')}</span>
              <Badge variant="secondary" className="min-w-5 rounded-full px-1.5">
                {workspaces.length}
              </Badge>
            </h1>
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
          </div>
          {orgUuid && canManageWorkspaces ? (
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
          ) : null}
        </div>
        <p className="mb-4 text-sm text-muted-foreground">
          {msg(
            'settings.workspaces.description',
            'Review workspace-specific API keys and create new workspaces from one settings view.',
          )}
        </p>
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
            <Alert variant="destructive">
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
                  className="grid gap-3 rounded-lg border border-border p-4 md:grid-cols-[1.5fr_1fr_auto]"
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
                <Table aria-label={msg('nav.workspaces', 'Workspaces')} className={dataTableClassName}>
                  <colgroup>
                    <col className="w-[28%]" />
                    <col className="w-[26%]" />
                    <col className="w-[20%]" />
                    <col className="w-[12%]" />
                    <col className="w-[14%]" />
                  </colgroup>
                  <TableHeader>
                    <TableRow className={dataTableHeaderRowClassName}>
                      <TableHead className={dataTableHeaderCellClassName}>
                        {msg('settings.workspaces.workspace', 'Workspace')}
                      </TableHead>
                      <TableHead className={dataTableHeaderCellClassName}>ID</TableHead>
                      <TableHead className={dataTableHeaderCellClassName}>
                        {msg('settings.workspaces.created', 'Created')}
                      </TableHead>
                      <TableHead className={dataTableHeaderCellClassName}>
                        {msg('settings.workspaces.apiKeysCount', 'API keys')}
                      </TableHead>
                      <TableHead className={dataTableHeaderCellClassName}>
                        <span className="sr-only">{msg('common.actions', 'Actions')}</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {visibleWorkspaces.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} className="h-24 px-3 py-6 text-sm text-muted-foreground">
                          {msg('settings.workspaces.noMatches', 'No workspaces match the current search or filter.')}
                        </TableCell>
                      </TableRow>
                    ) : null}
                    {visibleWorkspaces.map((workspace) => (
                      <DataTableRow key={workspace.id}>
                        <DataTableCell edge="start" className="min-w-0">
                          <div className="flex min-w-0 items-start gap-3">
                            <span className="grid size-9 shrink-0 place-items-center rounded-md border border-border bg-muted/40">
                              <Box className="size-4" style={{ color: workspaceColor(workspace) }} aria-hidden />
                            </span>
                            <div className="min-w-0">
                              <div className="flex flex-wrap items-center gap-2">
                                <span className="truncate font-medium text-foreground">{workspace.name}</span>
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
                                  <Badge variant="secondary">{msg('settings.workspaces.current', 'Current')}</Badge>
                                ) : null}
                              </div>
                            </div>
                          </div>
                        </DataTableCell>
                        <DataTableCell className="min-w-0">
                          <span className="block truncate font-mono text-xs text-muted-foreground">{workspace.id}</span>
                        </DataTableCell>
                        <DataTableCell className="text-sm text-foreground">
                          {workspace.created_at
                            ? formatters.date(workspace.created_at, { dateStyle: 'medium', timeStyle: 'short' })
                            : '–'}
                        </DataTableCell>
                        <DataTableCell className="text-sm text-foreground">
                          {formatters.number(workspace.api_keys_count ?? 0)}
                        </DataTableCell>
                        <DataTableCell edge="end" className="text-right">
                          <div className="flex justify-end">
                            <DropdownMenu>
                              <DropdownMenuTrigger
                                render={
                                  <Button
                                    variant="ghost"
                                    size="icon"
                                    className="text-muted-foreground"
                                    aria-label={msg('settings.workspaces.rowActions', 'Workspace actions')}
                                  />
                                }
                              >
                                <MoreVertical className="size-4" aria-hidden />
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end" className="w-48">
                                {canManageWorkspaces && !workspace.is_default ? (
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
                                {canManageWorkspaces && !workspace.is_default ? (
                                  <DropdownMenuItem
                                    variant="destructive"
                                    onClick={() => setArchivingWorkspace(workspace)}
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
          onClose={() => setArchivingWorkspace(null)}
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
