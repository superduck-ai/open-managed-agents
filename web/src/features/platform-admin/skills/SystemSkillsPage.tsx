import { useCallback, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useSearch } from '@tanstack/react-router';
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { FileArchive, Plus, Search } from 'lucide-react';
import { useI18n } from '@/shared/i18n';
import { Button } from '@/shared/ui/button';
import { Input } from '@/shared/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/shared/ui/select';
import { ResourcePageHeader } from '@/shared/ui/resource-page-header';
import { ResourceListState } from '@/shared/ui/resource-list-state';
import { Table, TableBody, TableHead, TableHeader, TableRow } from '@/shared/ui/table';
import {
  CopyIdCell,
  DataTableCell,
  DataTableRow,
  dataTableClassName,
  dataTableHeaderCellClassName,
  dataTableHeaderRowClassName,
} from '@/shared/ui/data-table-interactions';
import { usePlatformAdmin } from '../context';
import { SystemSkillError, SystemSkillPager, SystemSkillStatus } from './feedback';
import { SystemSkillDrawer } from './SystemSkillDrawer';
import { SystemSkillImportDialog } from './SystemSkillImportDialog';
import { systemSkillsKey, systemSkillPageSize, type SystemSkill, type SystemPage } from './types';

const emptySkills: SystemSkill[] = [];

export function SystemSkillsPage() {
  const { msg, locale } = useI18n();
  const { client } = usePlatformAdmin();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const selected = useSearch({ strict: false }).skill;
  const [upload, setUpload] = useState<{ target?: SystemSkill } | null>(null);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState('');
  const [status, setStatus] = useState('all');
  const statusLabels: Record<string, string> = {
    all: msg('systemSkills.allStatuses', 'All statuses'),
    listed: msg('systemSkills.listed', 'Listed'),
    unlisted: msg('systemSkills.unlisted', 'Unlisted'),
    draft: msg('systemSkills.draft', 'Draft'),
  };
  const [offset, setOffset] = useState(0);
  const query = useQuery({
    queryKey: [...systemSkillsKey, 'list', filter, status, offset],
    queryFn: ({ signal }) =>
      client.request<SystemPage<SystemSkill>>(
        `/skills?${new URLSearchParams({ query: filter, status: status === 'all' ? '' : status, offset: String(offset), limit: String(systemSkillPageSize) })}`,
        { signal },
      ),
  });
  const openSkill = useCallback(
    (id?: string) => {
      void navigate({ to: '/platform-admin/skills', search: { skill: id } });
    },
    [navigate],
  );
  const columns = useMemo<ColumnDef<SystemSkill>[]>(
    () => [
      {
        header: msg('skills.table.id', 'ID'),
        accessorKey: 'id',
        cell: ({ row }) => (
          <CopyIdCell
            value={row.original.id}
            ariaLabel={msg('skills.copyAria', 'Copy {skillId}', { skillId: row.original.id })}
            stopPropagation
          />
        ),
      },
      {
        header: msg('common.name', 'Name'),
        accessorKey: 'display_title',
        cell: ({ row }) => (
          <Button
            variant="link"
            className="h-auto max-w-full justify-start p-0 text-left whitespace-normal break-words font-medium text-foreground"
            onClick={(event) => {
              event.stopPropagation();
              openSkill(row.original.id);
            }}
          >
            {row.original.display_title}
          </Button>
        ),
      },
      { header: msg('systemSkills.status', 'Status'), cell: ({ row }) => <SystemSkillStatus skill={row.original} /> },
      {
        header: msg('systemSkills.defaultVersion', 'Default version'),
        accessorKey: 'default_version',
        cell: ({ row }) => row.original.default_version || '—',
      },
      {
        header: msg('common.updated', 'Updated'),
        cell: ({ row }) => new Date(row.original.updated_at).toLocaleDateString(locale),
      },
    ],
    [msg, locale, openSkill],
  );
  const table = useReactTable({ data: query.data?.data ?? emptySkills, columns, getCoreRowModel: getCoreRowModel() });
  const changed = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: systemSkillsKey }),
      queryClient.invalidateQueries({ queryKey: ['skills'] }),
    ]);
  };
  return (
    <section>
      <ResourcePageHeader
        title={msg('systemSkills.title', 'System skills')}
        description={msg('systemSkills.description', 'Manage skills and versions available to all workspaces.')}
        actions={
          <Button size="lg" onClick={() => setUpload({})}>
            <Plus aria-hidden className="size-4" />
            {msg('systemSkills.import', 'Import skills')}
          </Button>
        }
      />
      <form
        className="mb-5 flex flex-wrap items-center gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          setOffset(0);
          setFilter(search.trim());
        }}
      >
        <Input
          className="w-full sm:w-64"
          aria-label={msg('systemSkills.search', 'Search name or ID')}
          placeholder={msg('systemSkills.search', 'Search name or ID')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Select
          value={status}
          onValueChange={(value) => {
            setStatus(value ?? 'all');
            setOffset(0);
          }}
        >
          <SelectTrigger aria-label={msg('systemSkills.status', 'Status')}>
            <SelectValue>{statusLabels[status]}</SelectValue>
          </SelectTrigger>
          <SelectContent>
            {Object.entries(statusLabels).map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="submit" variant="outline">
          <Search className="size-4" aria-hidden />
          {msg('platformAdmin.search', 'Search')}
        </Button>
      </form>
      {query.isPending ? (
        <p role="status">{msg('common.loading', 'Loading...')}</p>
      ) : query.isError ? (
        <SystemSkillError error={query.error} retry={() => void query.refetch()} />
      ) : query.data.data.length === 0 ? (
        <ResourceListState
          icon={FileArchive}
          title={msg('systemSkills.empty', 'No matching system skills')}
          body={msg('systemSkills.emptyBody', 'Import a ZIP package or change the filters.')}
        />
      ) : (
        <Table className={`${dataTableClassName} min-w-[780px]`}>
          <TableHeader>
            {table.getHeaderGroups().map((group) => (
              <TableRow key={group.id} className={dataTableHeaderRowClassName}>
                {group.headers.map((header) => (
                  <TableHead key={header.id} className={dataTableHeaderCellClassName}>
                    {flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {table.getRowModel().rows.map((row) => (
              <DataTableRow
                key={row.original.id}
                clickable
                selected={selected === row.original.id}
                onClick={() => openSkill(row.original.id)}
              >
                {row.getVisibleCells().map((cell, index) => (
                  <DataTableCell
                    key={cell.id}
                    edge={index === 0 ? 'start' : index === row.getVisibleCells().length - 1 ? 'end' : undefined}
                    className="text-sm break-words whitespace-normal"
                  >
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </DataTableCell>
                ))}
              </DataTableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <SystemSkillPager
        offset={offset}
        more={query.data?.has_more ?? false}
        busy={query.isFetching}
        onOffset={setOffset}
      />
      {selected && (
        <SystemSkillDrawer
          key={selected}
          skillID={selected}
          onClose={() => openSkill()}
          onUpload={(target) => setUpload({ target })}
          onChanged={changed}
        />
      )}
      {upload && (
        <SystemSkillImportDialog
          target={upload.target}
          onClose={() => setUpload(null)}
          onImported={async (id) => {
            await changed();
            if (id) openSkill(id);
          }}
        />
      )}
    </section>
  );
}
