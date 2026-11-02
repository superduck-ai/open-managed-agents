import { useI18n } from '../../shared/i18n';
import { useRef, useState } from 'react';
import { Link, useNavigate } from '@tanstack/react-router';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { CreateOrganizationDialog } from './CreateOrganizationDialog';
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { Button } from '../../shared/ui/button';
import { Input } from '../../shared/ui/input';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { usePlatformAdmin } from './context';
import type { Organization, Page } from './types';

export function OrganizationsPage() {
  const { msg } = useI18n();

  const columns: ColumnDef<Organization>[] = [
    {
      accessorKey: 'name',
      header: msg('platformAdmin.organizationName', 'Organization name'),
      cell: ({ row }) => (
        <Link
          className="underline"
          to="/platform-admin/organizations/$organizationUuid"
          params={{ organizationUuid: row.original.uuid }}
        >
          {row.original.name}
        </Link>
      ),
    },
    { accessorKey: 'uuid', header: msg('platformAdmin.organizationUUID', 'Organization UUID') },
    { accessorKey: 'admin_count', header: msg('platformAdmin.administrators', 'Administrators') },
    { accessorKey: 'member_count', header: msg('platformAdmin.members', 'Members') },
    { accessorKey: 'workspace_count', header: msg('platformAdmin.workspaces', 'Workspaces') },
  ];

  const { client } = usePlatformAdmin();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const createButton = useRef<HTMLButtonElement | null>(null);
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<Organization | null>(null);
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState('');
  const [cursors, setCursors] = useState(['']);
  const cursor = cursors.at(-1) ?? '';
  const query = useQuery({
    queryKey: ['platform-admin', 'organizations', filter, cursor],
    queryFn: ({ signal }) =>
      client.request<Page<Organization>>(`/organizations?${new URLSearchParams({ query: filter, cursor })}`, {
        signal,
      }),
  });
  const table = useReactTable({ data: query.data?.data ?? [], columns, getCoreRowModel: getCoreRowModel() });
  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <h1 className="text-2xl font-semibold">{msg('platformAdmin.organizations', 'Organizations')}</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            {msg(
              'platformAdmin.manageAllOrganizationsAndTheirAdministrators',
              'Manage all organizations and their administrators.',
            )}
          </p>
        </div>
        <Button ref={createButton} className="shrink-0" onClick={() => setCreating(true)}>
          {msg('platformAdmin.createOrganization', 'Create organization')}
        </Button>
      </header>
      {created && (
        <p role="status">
          {msg('platformAdmin.createdOrganizationNameUUIDUuid', 'Created organization “{name}”. UUID: {uuid}', {
            name: created.name,
            uuid: created.uuid,
          })}
        </p>
      )}
      {creating && (
        <CreateOrganizationDialog
          finalFocus={createButton}
          open
          onClose={() => setCreating(false)}
          onCreated={(organization) => {
            setCreating(false);
            setCreated(organization);
            setFilter('');
            setSearch('');
            setCursors(['']);
            void queryClient.invalidateQueries({ queryKey: ['platform-admin', 'organizations'] });
            void navigate({
              to: '/platform-admin/organizations/$organizationUuid',
              params: { organizationUuid: organization.uuid },
            });
          }}
        />
      )}
      <form
        className="flex max-w-lg gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          setFilter(search.trim());
          setCursors(['']);
        }}
      >
        <Input
          aria-label={msg('platformAdmin.searchOrganizationNameOrUUID', 'Search organization name or UUID')}
          placeholder={msg('platformAdmin.organizationNameOrUUID', 'Organization name or UUID')}
          value={search}
          onChange={(event) => setSearch(event.target.value)}
        />
        <Button type="submit">{msg('platformAdmin.search', 'Search')}</Button>
      </form>
      {query.isError ? (
        <div role="alert">
          {msg('platformAdmin.couldNotLoadOrganizations', 'Could not load organizations.')}
          <Button variant="outline" onClick={() => void query.refetch()}>
            {msg('platformAdmin.retry', 'Retry')}
          </Button>
        </div>
      ) : query.isPending ? (
        <p role="status">{msg('platformAdmin.loadingOrganizations', 'Loading organizations…')}</p>
      ) : (
        <>
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((group) => (
                <TableRow key={group.id}>
                  {group.headers.map((header) => (
                    <TableHead key={header.id}>
                      {flexRender(header.column.columnDef.header, header.getContext())}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.map((row) => (
                <TableRow key={row.original.uuid}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {!query.data.data.length && (
            <p className="py-8 text-center text-muted-foreground">
              {msg('platformAdmin.noMatchingOrganizations', 'No matching organizations.')}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={cursors.length < 2} onClick={() => setCursors(cursors.slice(0, -1))}>
              {msg('platformAdmin.previous', 'Previous')}
            </Button>
            <Button
              variant="outline"
              disabled={!query.data.next_cursor}
              onClick={() => setCursors([...cursors, query.data.next_cursor])}
            >
              {msg('platformAdmin.next', 'Next')}
            </Button>
          </div>
        </>
      )}
    </section>
  );
}
