import { Archive, ArrowUpRight, BookOpen, ChevronLeft, ChevronRight, Plus, Trash2, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { CopyIdCell, DataTableCell, DataTableRow } from '../../../shared/ui/data-table-interactions';
import { ResourceFilterDropdown, ResourceSearchField } from '../../../shared/ui/resource-list-controls';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import { Skeleton } from '../../../shared/ui/skeleton';
import type { EnvironmentApiResponse, PageCursor } from '../types';
import { handleInternalLinkClick, managedEntityDetailHref } from '../utils';
import { environmentErrorMessage } from '../resources/environment-model';
import { EnvironmentActions, type EnvironmentActionRequest } from './actions';
import { EnvironmentHosting, EnvironmentScope } from './common';
import { useEnvironments } from './data';
import { environmentDocs } from './fields';

export function EnvironmentList({
  workspaceId,
  selectedId,
  onPreview,
  onAction,
  listHref,
}: {
  workspaceId: string;
  selectedId?: string;
  onPreview: (id: string, ids: string[]) => void;
  onAction: (request: EnvironmentActionRequest) => void;
  listHref: string;
}) {
  const { msg } = useI18n();
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('all');
  const [filterOpen, setFilterOpen] = useState(false);
  const [pages, setPages] = useState<PageCursor[]>([null]);
  const [selection, setSelection] = useState<Record<string, boolean>>({});
  const query = useEnvironments(workspaceId, pages.at(-1) ?? null, Boolean(search.trim()) || status !== 'all');
  const entities = useMemo(
    () =>
      (query.data?.data ?? []).filter((entity) => {
        const archived = Boolean(entity.archived_at || entity.state === 'archived');
        return (
          (status === 'all' || (status === 'archived') === archived) &&
          (!search.trim() ||
            entity.id === search.trim() ||
            entity.name.toLowerCase().includes(search.trim().toLowerCase()))
        );
      }),
    [query.data, search, status],
  );
  const columns = useMemo<ColumnDef<EnvironmentApiResponse>[]>(() => [{ accessorKey: 'id' }], []);
  const table = useReactTable({
    data: entities,
    columns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => row.id,
    enableRowSelection: true,
    state: { rowSelection: selection },
    onRowSelectionChange: setSelection,
  });
  const selected = table.getSelectedRowModel().rows.map((row) => row.original);
  const options = [
    { value: 'all', label: msg('common.all', 'All') },
    { value: 'active', label: msg('common.active', 'Active') },
    { value: 'archived', label: msg('common.archived', 'Archived') },
  ];
  const createHref = `${listHref}/new`;
  return (
    <section className="px-0 lg:px-6">
      <div className="mb-8 flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[22px] font-medium tracking-tight">
            {msg('managedAgents.environments.title', 'Environments')}
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            {msg(
              'managedAgents.environments.description',
              'Configuration template for containers, such as sessions or code execution.',
            )}
          </p>
        </div>
        <div className="flex gap-2">
          <ButtonLink href={createHref} onClick={(event) => handleInternalLinkClick(event, createHref)}>
            <Plus className="size-4" />
            {msg('managedAgents.environments.create', 'Create environment')}
          </ButtonLink>
          <ButtonLink
            variant="outline"
            size="icon"
            href={environmentDocs}
            target="_blank"
            rel="noreferrer"
            aria-label={msg('common.documentation', 'Documentation')}
          >
            <BookOpen className="size-4" strokeWidth={1.5} />
          </ButtonLink>
        </div>
      </div>
      <div className="mb-4 flex flex-wrap gap-2">
        <ResourceSearchField
          id="environments-search"
          value={search}
          placeholder={msg('managedAgents.common.searchByNameOrId', 'Search by name or exact ID')}
          onChange={(value) => {
            setSearch(value);
            setSelection({});
          }}
        />
        <ResourceFilterDropdown
          label={msg('common.status', 'Status')}
          valueLabel={options.find((option) => option.value === status)!.label}
          options={options}
          value={status}
          menu="status"
          open={filterOpen}
          menuWidthClass="w-40"
          onOpenChange={(open) => setFilterOpen(Boolean(open))}
          onSelect={(value) => {
            setStatus(value);
            setSelection({});
            setFilterOpen(false);
          }}
        />
      </div>
      {query.error ? (
        <Alert variant="destructive">
          <AlertDescription>
            {environmentErrorMessage(query.error, 'list', msg)}{' '}
            <Button variant="ghost" size="sm" onClick={() => void query.refetch()}>
              {msg('common.retry', 'Retry')}
            </Button>
          </AlertDescription>
        </Alert>
      ) : query.isPending ? (
        <Skeleton className="h-40 w-full" />
      ) : (
        <>
          <Table className="border-separate border-spacing-y-px text-sm">
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-10 px-3">
                  <Checkbox
                    aria-label={msg('environmentPage.selectAll', 'Select all environments')}
                    checked={table.getIsAllRowsSelected()}
                    indeterminate={table.getIsSomeRowsSelected()}
                    onCheckedChange={(checked) => table.toggleAllRowsSelected(checked)}
                    disabled={!entities.length}
                  />
                </TableHead>
                {[
                  'ID',
                  msg('common.name', 'Name'),
                  msg('common.type', 'Type'),
                  msg('environmentPage.scope', 'Scope'),
                  msg('environmentPage.updatedAt', 'Updated at'),
                  msg('environmentPage.archivedAt', 'Archived at'),
                ].map((label) => (
                  <TableHead key={label} className="h-10 px-3 text-xs font-normal text-muted-foreground">
                    {label}
                  </TableHead>
                ))}
                <TableHead className="w-28">
                  <span className="sr-only">{msg('common.actions', 'Actions')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
                <EnvironmentRow
                  key={row.id}
                  entity={row.original}
                  workspaceId={workspaceId}
                  selected={row.getIsSelected()}
                  previewed={selectedId === row.id}
                  onChecked={(checked) => row.toggleSelected(checked)}
                  onPreview={() =>
                    onPreview(
                      row.id,
                      entities.map((entity) => entity.id),
                    )
                  }
                  onAction={(request) => onAction({ ...request, onCompleted: () => setSelection({}) })}
                />
              ))}
            </TableBody>
          </Table>
          {!entities.length ? (
            <div className="py-20 text-center text-sm text-muted-foreground">
              {search || status !== 'all'
                ? msg('environmentPage.noResults', 'No environments found.')
                : msg('managedAgents.environments.emptyTitle', 'No environments yet')}
            </div>
          ) : null}
          <div className="mt-4 flex gap-2">
            <Button
              variant="outline"
              size="icon"
              aria-label={msg('common.previousPage', 'Previous page')}
              disabled={pages.length === 1 || query.isFetching || Boolean(search.trim()) || status !== 'all'}
              onClick={() => {
                setPages(pages.slice(0, -1));
                setSelection({});
              }}
            >
              <ChevronLeft className="size-4" />
            </Button>
            <Button
              variant="outline"
              size="icon"
              aria-label={msg('common.nextPage', 'Next page')}
              disabled={!query.data?.has_more || !query.data?.next_page || query.isFetching}
              onClick={() => {
                setPages([...pages, query.data?.next_page ?? null]);
                setSelection({});
              }}
            >
              <ChevronRight className="size-4" />
            </Button>
          </div>
        </>
      )}
      {selected.length ? (
        <div className="fixed bottom-6 left-1/2 z-20 flex -translate-x-1/2 items-center gap-2 rounded-xl border border-border bg-background p-1.5 pl-3 text-sm shadow-lg md:left-[calc(50%+var(--sidebar-width,256px)/2)]">
          <span>{msg('environmentPage.selected', '{count} selected', { count: selected.length })}</span>
          <Button
            variant="ghost"
            size="icon-xs"
            aria-label={msg('environmentPage.clearSelection', 'Clear selection')}
            onClick={() => setSelection({})}
          >
            <X className="size-3.5" />
          </Button>
          <span className="mx-1 h-5 w-px bg-border" />
          {selected.some((entity) => !entity.archived_at && entity.state !== 'archived') ? (
            <Button
              variant="ghost"
              size="sm"
              onClick={() =>
                onAction({
                  action: 'archive',
                  onCompleted: () => setSelection({}),
                  entities: selected.filter((entity) => !entity.archived_at && entity.state !== 'archived'),
                })
              }
            >
              <Archive className="size-4" strokeWidth={1.5} />
              {msg('common.archive', 'Archive')}
            </Button>
          ) : null}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onAction({ action: 'delete', entities: selected, onCompleted: () => setSelection({}) })}
          >
            <Trash2 className="size-4" strokeWidth={1.5} />
            {msg('common.delete', 'Delete')}
          </Button>
        </div>
      ) : null}
    </section>
  );
}

function EnvironmentRow({
  entity,
  workspaceId,
  selected,
  previewed,
  onChecked,
  onPreview,
  onAction,
}: {
  entity: EnvironmentApiResponse;
  workspaceId: string;
  selected: boolean;
  previewed: boolean;
  onChecked: (checked: boolean) => void;
  onPreview: () => void;
  onAction: (request: EnvironmentActionRequest) => void;
}) {
  const { msg } = useI18n();
  const format = useFormatters();
  const href = managedEntityDetailHref(workspaceId, 'environments', entity.id);
  return (
    <DataTableRow clickable selected={selected || previewed} className="group" onClick={onPreview}>
      <DataTableCell edge="start" className="h-11" onClick={(event) => event.stopPropagation()}>
        <Checkbox
          aria-label={msg('environmentPage.select', 'Select {name}', { name: entity.name })}
          checked={selected}
          onCheckedChange={onChecked}
        />
      </DataTableCell>
      <DataTableCell className="h-11">
        <CopyIdCell
          value={entity.id}
          ariaLabel={msg('common.copyValue', 'Copy {value}', { value: entity.id })}
          textClassName="text-[13px]"
          stopPropagation
        />
      </DataTableCell>
      <DataTableCell className="max-w-80 truncate">
        <a
          href={href}
          className="rounded-sm underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-ring focus-visible:underline"
          title={entity.name}
          onClick={(event) => {
            event.stopPropagation();
            handleInternalLinkClick(event, href);
          }}
        >
          {entity.name}
        </a>
      </DataTableCell>
      <DataTableCell>
        <span className="rounded bg-muted px-1.5 py-0.5 text-xs">
          <EnvironmentHosting entity={entity} />
        </span>
      </DataTableCell>
      <DataTableCell>
        <EnvironmentScope scope={entity.scope} />
      </DataTableCell>
      <DataTableCell className="whitespace-nowrap text-muted-foreground">
        {format.date(entity.updated_at, { month: 'short', day: 'numeric' })}
      </DataTableCell>
      <DataTableCell className="whitespace-nowrap text-muted-foreground">
        {entity.archived_at ? format.date(entity.archived_at, { month: 'short', day: 'numeric' }) : ''}
      </DataTableCell>
      <DataTableCell edge="end" onClick={(event) => event.stopPropagation()}>
        <div className="flex items-center justify-end gap-1">
          <ButtonLink
            href={href}
            variant="outline"
            size="xs"
            className="opacity-0 group-hover:opacity-100 focus:opacity-100"
            onClick={(event) => handleInternalLinkClick(event, href)}
          >
            {msg('common.open', 'Open')}
            <ArrowUpRight className="size-3" />
          </ButtonLink>
          <EnvironmentActions entity={entity} onAction={onAction} />
        </div>
      </DataTableCell>
    </DataTableRow>
  );
}
