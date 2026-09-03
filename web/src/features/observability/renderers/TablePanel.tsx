import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table';
import { useMemo, useState } from 'react';
import { useFormatters, useI18n } from '../../../shared/i18n';
import { cn } from '../../../shared/lib/utils';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import { PRESS_SCALE_CLASS } from '../chrome';
import { formatTableCell } from '../format';
import type { ObservabilityPanelOptions, TablePanelData } from '../types';

const NUMERIC_FORMATS = new Set(['number', 'duration_ms', 'percent', 'tokens']);

function toSortableNumber(value: unknown) {
  const numeric = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(numeric) ? numeric : Number.NEGATIVE_INFINITY;
}

export function TablePanel({ options, data }: { options?: ObservabilityPanelOptions; data: TablePanelData }) {
  const { msg } = useI18n();
  const formatters = useFormatters();
  const columnsMeta = options?.columns;
  const rows = data.rows ?? [];
  const [sorting, setSorting] = useState<Array<{ id: string; desc: boolean }>>([]);
  const columns = useMemo(() => {
    const helper = createColumnHelper<Record<string, unknown>>();
    return (columnsMeta ?? []).map((column) =>
      helper.accessor((row) => row[column.key], {
        id: column.key,
        header: msg(column.label_key, column.key),
        enableSorting: column.sortable !== false,
        // 后端把数值列以字符串返回时按字典序排会乱序（"9" > "10"），数值格式列强制按数值比较。
        sortingFn: NUMERIC_FORMATS.has(column.format ?? '')
          ? (left, right, columnId) => {
              const a = toSortableNumber(left.getValue(columnId));
              const b = toSortableNumber(right.getValue(columnId));
              return a === b ? 0 : a < b ? -1 : 1;
            }
          : 'alphanumeric',
        cell: (info) => formatTableCell(info.getValue(), column, formatters),
      }),
    );
  }, [columnsMeta, formatters, msg]);
  // TanStack Table returns callback-heavy instance methods; this table instance stays local to the panel.
  // eslint-disable-next-line react-hooks/incompatible-library
  const table = useReactTable({
    data: rows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  });

  if (!columnsMeta?.length) {
    return <FallbackRows rows={rows} />;
  }

  return (
    <Table className="text-left">
      <TableHeader>
        {table.getHeaderGroups().map((headerGroup) => (
          <TableRow key={headerGroup.id} className="hover:bg-transparent">
            {headerGroup.headers.map((header) => (
              <TableHead key={header.id}>
                {header.isPlaceholder ? null : header.column.getCanSort() ? (
                  <button
                    type="button"
                    className={cn('inline-flex items-center gap-1', PRESS_SCALE_CLASS)}
                    onClick={header.column.getToggleSortingHandler()}
                  >
                    {flexRender(header.column.columnDef.header, header.getContext())}
                  </button>
                ) : (
                  flexRender(header.column.columnDef.header, header.getContext())
                )}
              </TableHead>
            ))}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <TableRow key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <TableCell key={cell.id}>{flexRender(cell.column.columnDef.cell, cell.getContext())}</TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function FallbackRows({ rows }: { rows: Array<Record<string, unknown>> }) {
  const keys = Object.keys(rows[0] ?? {});
  return (
    <Table className="text-left">
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          {keys.map((key) => (
            <TableHead key={key}>{key}</TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row, index) => (
          <TableRow key={index}>
            {keys.map((key) => (
              <TableCell key={key}>{String(row[key] ?? '—')}</TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
