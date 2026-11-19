import { documentationHref } from '../../../shared/documentation';
import { Archive, ArrowUpRight, BookOpen, Plus, Trash2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Button, ButtonLink } from '../../../shared/ui/button';
import { Checkbox } from '../../../shared/ui/checkbox';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import {
  CopyIdCell,
  DataTableCell,
  DataTableResourceLink,
  DataTableRow,
  dataTableActionsColumnClassName,
  dataTableCheckboxColumnClassName,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
  dataTableIdColumnClassName,
  dataTableMetaColumnClassName,
  dataTablePrimaryColumnClassName,
  dataTableTimeColumnClassName,
} from '../../../shared/ui/data-table-interactions';
import { ResourceBulkActionBar } from '../../../shared/ui/resource-bulk-action-bar';
import { ResourcePageHeader } from '../../../shared/ui/resource-page-header';
import { ResourceListPagination } from '../../../shared/ui/resource-list-pagination';
import { ResourceFilterDropdown, ResourceSearchField } from '../../../shared/ui/resource-list-controls';
import { ResourceStatusBadge } from '../../../shared/ui/resource-status-badge';
import { TableSkeletonRows } from '../../../shared/ui/resource-table';
import { RelativeTimeText } from '../../../shared/ui/resource-time-cell';
import { Alert, AlertDescription } from '../../../shared/ui/alert';
import type { EnvironmentApiResponse, PageCursor } from '../types';
import { handleInternalLinkClick, managedEntityDetailHref } from '../utils';
import { environmentErrorMessage } from '../resources/environment-model';
import { EnvironmentActions, type EnvironmentActionRequest } from './actions';
import { EnvironmentHosting, EnvironmentScope } from './common';
import { useEnvironments } from './data';

const EMPTY_ENVIRONMENTS: EnvironmentApiResponse[] = [];

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
  const { locale, msg } = useI18n();
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('all');
  const [filterOpen, setFilterOpen] = useState(false);
  const [pages, setPages] = useState<PageCursor[]>([null]);
  const [selection, setSelection] = useState<Record<string, boolean>>({});
  const query = useEnvironments(workspaceId, pages.at(-1) ?? null, search.trim(), status);
  const entities = query.data?.data ?? EMPTY_ENVIRONMENTS;
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
    <section className="min-w-0 text-foreground">
      <ResourcePageHeader
        title={msg('managedAgents.environments.title', 'Environments')}
        actions={
          <>
            <ButtonLink size="lg" href={createHref} onClick={(event) => handleInternalLinkClick(event, createHref)}>
              <Plus className="size-4" />
              {msg('managedAgents.environments.create', 'Create environment')}
            </ButtonLink>
            <ButtonLink
              variant="outline"
              size="icon-lg"
              href={documentationHref('environments', locale)}
              target="_blank"
              rel="noreferrer"
              aria-label={msg('common.documentation', 'Documentation')}
            >
              <BookOpen className="size-4" strokeWidth={1.5} />
            </ButtonLink>
          </>
        }
      />
      <div className="mb-7 flex flex-wrap items-center gap-2">
        <ResourceSearchField
          id="environments-search"
          value={search}
          placeholder={msg('managedAgents.common.searchByNameOrId', 'Search by name or exact ID')}
          onChange={(value) => {
            setSearch(value);
            setPages([null]);
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
            setPages([null]);
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
      ) : (
        <>
          <Table className={dataTableClassName}>
            <TableHeader>
              <TableRow className={dataTableHeaderRowClassName}>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableCheckboxColumnClassName)}>
                  <Checkbox
                    aria-label={msg('environmentPage.selectAll', 'Select all environments')}
                    checked={table.getIsAllRowsSelected()}
                    indeterminate={table.getIsSomeRowsSelected()}
                    onCheckedChange={(checked) => table.toggleAllRowsSelected(checked)}
                    disabled={!entities.length || query.isPending}
                  />
                </TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableIdColumnClassName)}>ID</TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTablePrimaryColumnClassName)}>
                  {msg('common.name', 'Name')}
                </TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableMetaColumnClassName)}>
                  {msg('common.type', 'Type')}
                </TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableMetaColumnClassName)}>
                  {msg('environmentPage.scope', 'Scope')}
                </TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableTimeColumnClassName)}>
                  {msg('environmentPage.updatedAt', 'Updated at')}
                </TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableTimeColumnClassName)}>
                  {msg('environmentPage.archivedAt', 'Archived at')}
                </TableHead>
                <TableHead className={cn(dataTableHeaderCellClassName, dataTableActionsColumnClassName)}>
                  <span className="sr-only">{msg('common.actions', 'Actions')}</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {query.isPending ? (
                <TableSkeletonRows
                  colCount={8}
                  label={msg('managedAgents.environments.loading', 'Loading environments...')}
                />
              ) : (
                table.getRowModel().rows.map((row) => (
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
                ))
              )}
            </TableBody>
          </Table>
          {!query.isPending && !entities.length ? (
            <div className="py-20 text-center text-sm text-muted-foreground">
              {search || status !== 'all'
                ? msg('environmentPage.noResults', 'No environments found.')
                : msg('managedAgents.environments.emptyTitle', 'No environments yet')}
            </div>
          ) : null}
          {!query.isPending ? (
            <ResourceListPagination
              currentPage={pages.length}
              totalPages={null}
              canPrevious={pages.length > 1 && !query.isFetching}
              canNext={Boolean(query.data?.has_more && query.data?.next_page) && !query.isFetching}
              onPrevious={() => {
                setPages(pages.slice(0, -1));
                setSelection({});
              }}
              onNext={() => {
                setPages([...pages, query.data?.next_page ?? null]);
                setSelection({});
              }}
            />
          ) : null}
        </>
      )}
      <ResourceBulkActionBar
        selectedCount={selected.length}
        selectedLabel={msg('environmentPage.selected', '{count} selected', { count: selected.length })}
        onClear={() => setSelection({})}
      >
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
      </ResourceBulkActionBar>
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
  const href = managedEntityDetailHref(workspaceId, 'environments', entity.id);
  return (
    <DataTableRow clickable selected={selected || previewed} className="group" onClick={onPreview}>
      <DataTableCell
        edge="start"
        className={dataTableCheckboxColumnClassName}
        onClick={(event) => event.stopPropagation()}
      >
        <Checkbox
          aria-label={msg('environmentPage.select', 'Select {name}', { name: entity.name })}
          checked={selected}
          onCheckedChange={onChecked}
        />
      </DataTableCell>
      <DataTableCell className={dataTableIdColumnClassName}>
        <CopyIdCell
          value={entity.id}
          ariaLabel={msg('common.copyValue', 'Copy {value}', { value: entity.id })}
          stopPropagation
        />
      </DataTableCell>
      <DataTableCell className={dataTablePrimaryColumnClassName}>
        <DataTableResourceLink
          href={href}
          className="block max-w-[340px] truncate rounded-sm font-medium text-foreground focus-visible:outline-[0.5px] focus-visible:outline-offset-[-0.5px] focus-visible:outline-ring"
          title={entity.name}
          onClick={(event) => {
            event.stopPropagation();
            handleInternalLinkClick(event, href);
          }}
        >
          {entity.name}
        </DataTableResourceLink>
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <ResourceStatusBadge tone="neutral">
          <EnvironmentHosting entity={entity} />
        </ResourceStatusBadge>
      </DataTableCell>
      <DataTableCell className={dataTableMetaColumnClassName}>
        <EnvironmentScope scope={entity.scope} />
      </DataTableCell>
      <DataTableCell className={dataTableTimeColumnClassName}>
        <RelativeTimeText value={entity.updated_at} />
      </DataTableCell>
      <DataTableCell className={dataTableTimeColumnClassName}>
        <RelativeTimeText value={entity.archived_at} emptyLabel="" />
      </DataTableCell>
      <DataTableCell
        edge="end"
        className={dataTableActionsColumnClassName}
        onClick={(event) => event.stopPropagation()}
      >
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
