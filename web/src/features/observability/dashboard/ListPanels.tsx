import { useI18n } from '../../../shared/i18n';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../../shared/ui/table';
import type { BarData, DashboardColumn, PanelUnit, TableRowData } from '../api';
import { formatValue } from './format';
import { reasonLabel, usePanelTitle } from './labels';
import { EmptyPanel, PanelFrame } from './PanelFrame';
import { COLUMN_TEXT } from './panelText';

type Msg = ReturnType<typeof useI18n>['msg'];

export function BarsPanel({ id, unit, data }: { id: string; unit?: PanelUnit; data: BarData[] }) {
  const { locale, msg } = useI18n();
  const largest = Math.max(...data.map((bar) => bar.value), 0);
  const name = (value: string) => (id === 'end_reasons' ? reasonLabel(value, msg) : value);
  return (
    <PanelFrame id={id}>
      {data.length ? (
        <ul className="flex flex-col gap-2">
          {data.map((bar) => (
            <li
              key={bar.name}
              className="grid grid-cols-[minmax(0,10rem)_minmax(0,1fr)_auto] items-center gap-3 text-sm"
            >
              <span className="truncate" title={bar.name}>
                {name(bar.name)}
              </span>
              <span className="h-2.5 rounded-sm bg-muted">
                <span
                  className="block h-full rounded-sm bg-[var(--chart-1)]"
                  style={{ width: `${largest ? (bar.value / largest) * 100 : 0}%` }}
                />
              </span>
              <span className="tabular-nums text-muted-foreground">{formatValue(unit, bar.value, locale)}</span>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyPanel className="min-h-24" />
      )}
    </PanelFrame>
  );
}

function columnHeader(column: DashboardColumn, msg: Msg) {
  const text = COLUMN_TEXT[column.key] ?? column.key;
  return msg(`observability.column.${column.key}`, text);
}

export function TablePanel({ id, columns, rows }: { id: string; columns: DashboardColumn[]; rows: TableRowData[] }) {
  const { locale, msg } = useI18n();
  const title = usePanelTitle(id);
  return (
    <PanelFrame id={id}>
      {rows.length ? (
        <div className="overflow-x-auto">
          <Table aria-label={title}>
            <TableHeader>
              <TableRow>
                {columns.map((column) => (
                  <TableHead key={column.key} className={column.unit === 'text' ? undefined : 'text-right'}>
                    {columnHeader(column, msg)}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, index) => (
                <TableRow key={index}>
                  {columns.map((column) => (
                    <TableCell
                      key={column.key}
                      className={column.unit === 'text' ? 'max-w-56 truncate' : 'text-right tabular-nums'}
                    >
                      {formatValue(column.unit, row[column.key], locale)}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        <EmptyPanel className="min-h-24" />
      )}
    </PanelFrame>
  );
}
