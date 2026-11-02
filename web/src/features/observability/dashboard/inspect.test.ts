import { expect, test } from 'bun:test';
import type { DashboardPanel } from '../api';
import { panelTable, tableCsv } from './inspect';

const labels = {
  time: 'Time',
  name: 'Name',
  value: 'Value',
  formatTime: (ms: number) => `t${ms}`,
  series: (key: string) => key.toUpperCase(),
  column: (key: string) => `#${key}`,
  field: (key: string) => `field:${key}`,
  bar: (name: string) => `bar:${name}`,
};

test('omits stat fields the panel does not report, labels names and quotes CSV cells that need it', () => {
  const stat = { id: 'x', span: 3, render: 'stat', data: { value: 3, previous: null } } as DashboardPanel;
  expect(panelTable(stat, labels).rows).toEqual([
    ['field:value', 3],
    ['field:previous', null],
  ]);
  const bars = { id: 'b', span: 6, render: 'bars', data: [{ name: 'a,"b"', value: 2 }] } as DashboardPanel;
  expect(tableCsv(panelTable(bars, labels))).toBe('Name,Value\n"bar:a,""b""",2');
});

test('flattens series into one row per bucket and tables by declared columns', () => {
  const series = {
    id: 's',
    span: 6,
    render: 'series',
    data: [
      {
        key: 'ok',
        points: [
          { t: 1, v: 2 },
          { t: 2, v: null },
        ],
      },
      {
        key: 'bad',
        points: [
          { t: 1, v: 0 },
          { t: 2, v: 5 },
        ],
      },
    ],
  } as DashboardPanel;
  expect(panelTable(series, labels)).toEqual({
    columns: ['Time', 'OK', 'BAD'],
    rows: [
      ['t1', 2, 0],
      ['t2', null, 5],
    ],
  });
  const table = {
    id: 't',
    span: 12,
    render: 'table',
    columns: [{ key: 'model', unit: 'text' }],
    data: { rows: [{ model: 'm', extra: 1 }] },
  } as DashboardPanel;
  expect(tableCsv(panelTable(table, labels))).toBe('#model\nm');
});
