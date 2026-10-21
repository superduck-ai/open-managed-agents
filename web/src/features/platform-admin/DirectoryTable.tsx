import { useI18n } from '../../shared/i18n';
import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table';
import { Button } from '../../shared/ui/button';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../shared/ui/table';
import { usePlatformAdmin } from './context';
import type { Page } from './types';

export function DirectoryTable<T extends { uuid: string }>({
  path,
  columns,
  label,
}: {
  path: string;
  columns: ColumnDef<T>[];
  label: string;
}) {
  const { msg } = useI18n();

  const { client } = usePlatformAdmin();
  const [cursors, setCursors] = useState(['']);
  const cursor = cursors.at(-1) ?? '';
  const query = useQuery({
    queryKey: ['platform-admin', 'directory', path, cursor],
    queryFn: ({ signal }) =>
      client.request<Page<T>>(`${path}${path.includes('?') ? '&' : '?'}cursor=${encodeURIComponent(cursor)}`, {
        signal,
      }),
  });
  const table = useReactTable({ data: query.data?.data ?? [], columns, getCoreRowModel: getCoreRowModel() });
  if (query.isPending) return <p role="status">{msg('platformAdmin.loadingLabel', 'Loading {label}…', { label })}</p>;
  if (query.isError)
    return (
      <div role="alert">
        {msg('platformAdmin.couldNotLoadLabel', 'Could not load {label}.', { label })}
        <Button variant="outline" onClick={() => void query.refetch()}>
          {msg('platformAdmin.retry', 'Retry')}
        </Button>
      </div>
    );
  return (
    <div className="space-y-3">
      <Table>
        <TableHeader>
          {table.getHeaderGroups().map((group) => (
            <TableRow key={group.id}>
              {group.headers.map((header) => (
                <TableHead key={header.id}>{flexRender(header.column.columnDef.header, header.getContext())}</TableHead>
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
        <p className="py-6 text-center text-muted-foreground">
          {msg('platformAdmin.noLabelYet', 'No {label} yet.', { label })}
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
    </div>
  );
}
