import { Download } from 'lucide-react';
import { useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../../../shared/ui/dialog';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import type { DashboardPanel } from '../api';
import { formatAbsolute, type TimeZoneMode } from '../time/range';
import { panelTable, tableCsv, type InspectTable } from './inspect';
import { reasonLabel, usePanelTitle } from './labels';
import { COLUMN_TEXT, SERIES_TEXT } from './panelText';

const FIELD_TEXT: Record<string, string> = { value: 'Current period', previous: 'Previous period' };

function downloadCsv(table: InspectTable, name: string) {
  const url = URL.createObjectURL(new Blob([tableCsv(table)], { type: 'text/csv;charset=utf-8' }));
  const link = document.createElement('a');
  link.href = url;
  link.download = `${name}.csv`;
  link.click();
  URL.revokeObjectURL(url);
}

function InspectBody({ panel, timeZone }: { panel: DashboardPanel; timeZone: TimeZoneMode }) {
  const { msg } = useI18n();
  const title = usePanelTitle(panel.id);
  const table = panelTable(panel, {
    time: msg('observability.inspect.time', 'Time'),
    name: msg('observability.inspect.name', 'Name'),
    value: msg('observability.inspect.value', 'Value'),
    formatTime: (ms) => formatAbsolute(ms, timeZone),
    series: (key) => (SERIES_TEXT[key] ? msg(`observability.series.${key}`, SERIES_TEXT[key]) : key),
    column: (key) => msg(`observability.column.${key}`, COLUMN_TEXT[key] ?? key),
    field: (key) => (FIELD_TEXT[key] ? msg(`observability.inspect.field.${key}`, FIELD_TEXT[key]) : key),
    bar: (name) => (panel.id === 'end_reasons' ? reasonLabel(name, msg) : name),
  });
  return (
    <>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>
          {msg('observability.inspect.description', '{count} rows returned for the current time range.', {
            count: table.rows.length,
          })}
        </DialogDescription>
      </DialogHeader>
      <div className="max-h-[60vh] overflow-auto rounded-md border border-border">
        <Table aria-label={title}>
          <TableHeader>
            <TableRow>
              {table.columns.map((column, index) => (
                <TableHead key={`${column}-${index}`}>{column}</TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {table.rows.map((row, rowIndex) => (
              <TableRow key={rowIndex}>
                {row.map((cell, index) => (
                  <TableCell key={index} className="font-mono text-xs tabular-nums">
                    {cell ?? '—'}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      <Button variant="outline" className="justify-self-end" onClick={() => downloadCsv(table, panel.id)}>
        <Download />
        {msg('observability.inspect.download', 'Download CSV')}
      </Button>
    </>
  );
}

export function InspectDialog({
  panel,
  timeZone,
  onClose,
}: {
  panel: DashboardPanel | null;
  timeZone: TimeZoneMode;
  onClose: () => void;
}) {
  return (
    <Dialog open={!!panel} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-3xl">
        {panel ? <InspectBody panel={panel} timeZone={timeZone} /> : null}
      </DialogContent>
    </Dialog>
  );
}
