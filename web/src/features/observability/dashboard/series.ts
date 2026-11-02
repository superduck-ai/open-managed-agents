import type { SeriesData } from '../api';

export type Row = Record<string, number | null>;

const RESERVED_COLORS: Record<string, string> = {
  succeeded: 'var(--chart-2)',
  failed: 'var(--destructive)',
  other: 'var(--muted-foreground)',
};

const ORDERED_KEYS = ['sessions', 'input', 'output', 'cache_read', 'cache_write', 'p50', 'p95', 'tokens'];

export function seriesColor(key: string) {
  if (RESERVED_COLORS[key]) return RESERVED_COLORS[key];
  const ordered = ORDERED_KEYS.indexOf(key);
  const slot = ordered >= 0 ? ordered : [...key].reduce((hash, char) => (hash * 31 + char.charCodeAt(0)) >>> 0, 7);
  return `var(--chart-${(slot % 5) + 1})`;
}

export function chartRows(data: SeriesData[]): Row[] {
  return (data[0]?.points ?? []).map((point, index) => {
    const row: Row = { t: point.t };
    data.forEach((series, position) => {
      row[`s${position}`] = series.points[index]?.v ?? null;
    });
    return row;
  });
}

export function toggleSeries(hidden: Set<string>, keys: string[], key: string, additive: boolean) {
  if (additive) {
    const next = new Set(hidden);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    return next.size === keys.length ? new Set<string>() : next;
  }
  const isolated = !hidden.has(key) && hidden.size === keys.length - 1;
  return isolated ? new Set<string>() : new Set(keys.filter((current) => current !== key));
}
