import type { DashboardPanel } from '../api';

export type Cell = string | number | null;

export interface InspectTable {
  columns: string[];
  rows: Cell[][];
}

export interface InspectLabels {
  time: string;
  name: string;
  value: string;
  formatTime: (ms: number) => string;
  series: (key: string) => string;
  column: (key: string) => string;
  field: (key: string) => string;
  bar: (name: string) => string;
}

function keyValueRows(entries: [string, number | null | undefined][], field: (key: string) => string): Cell[][] {
  return entries.filter(([, value]) => value !== undefined).map(([key, value]) => [field(key), value ?? null]);
}

export function panelTable(panel: DashboardPanel, labels: InspectLabels): InspectTable {
  switch (panel.render) {
    case 'series':
      return {
        columns: [labels.time, ...panel.data.map((series) => labels.series(series.key))],
        rows: (panel.data[0]?.points ?? []).map((point, index) => [
          labels.formatTime(point.t),
          ...panel.data.map((series) => series.points[index]?.v ?? null),
        ]),
      };
    case 'bars':
      return { columns: [labels.name, labels.value], rows: panel.data.map((bar) => [labels.bar(bar.name), bar.value]) };
    case 'stat':
      return {
        columns: [labels.name, labels.value],
        rows: keyValueRows(
          [
            ['value', panel.data.value],
            ['previous', panel.data.previous],
            ['p95', panel.data.secondary],
          ],
          labels.field,
        ),
      };
    case 'percentiles':
      return {
        columns: [labels.name, labels.value],
        rows: keyValueRows(
          [
            ['p50', panel.data.p50],
            ['p90', panel.data.p90],
            ['p95', panel.data.p95],
          ],
          labels.field,
        ),
      };
    default: {
      const columns = panel.columns ?? [];
      return {
        columns: columns.map((column) => labels.column(column.key)),
        rows: panel.data.rows.map((row) => columns.map((column) => row[column.key] ?? null)),
      };
    }
  }
}

function csvCell(cell: Cell) {
  const text = cell == null ? '' : String(cell);
  return /[",\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

export function tableCsv(table: InspectTable) {
  return [table.columns, ...table.rows].map((row) => row.map(csvCell).join(',')).join('\n');
}
