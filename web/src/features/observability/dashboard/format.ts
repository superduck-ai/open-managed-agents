import type { PanelUnit } from '../api';

export function formatDuration(ms: number | null | undefined, locale: string) {
  if (ms == null || !Number.isFinite(ms)) return '—';
  if (ms < 1000) return `${Math.round(ms).toLocaleString(locale)} ms`;
  if (ms < 60_000) return `${(ms / 1000).toLocaleString(locale, { maximumFractionDigits: 1 })} s`;
  return `${(ms / 60_000).toLocaleString(locale, { maximumFractionDigits: 1 })} min`;
}

function compact(value: number, locale: string) {
  if (Math.abs(value) < 10_000) return Math.round(value).toLocaleString(locale);
  return new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(value);
}

export function formatValue(unit: PanelUnit | undefined, value: number | string | null | undefined, locale: string) {
  if (value == null || value === '') return '—';
  if (typeof value === 'string') return value;
  switch (unit) {
    case 'duration_ms':
      return formatDuration(value, locale);
    case 'percent':
      return `${value.toLocaleString(locale, { maximumFractionDigits: 1 })}%`;
    case 'version':
      return `v${value}`;
    case 'count':
    case 'tokens':
      return compact(value, locale);
    default:
      return value.toLocaleString(locale, { maximumFractionDigits: 1 });
  }
}

export function changeRatio(value: number | null | undefined, previous: number | null | undefined) {
  if (value == null || previous == null || previous === 0) return null;
  return (value - previous) / previous;
}

export function bucketLabel(bucket: number, intervalMs: number, locale: string, timeZone?: string) {
  const options: Intl.DateTimeFormatOptions =
    intervalMs < 86_400_000
      ? { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }
      : { month: 'numeric', day: 'numeric' };
  return new Date(bucket).toLocaleString(locale, { ...options, timeZone });
}

export function axisLabel(bucket: number, intervalMs: number, spanMs: number, locale: string, timeZone?: string) {
  if (intervalMs < 86_400_000 && spanMs <= 86_400_000)
    return new Date(bucket).toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', timeZone });
  return bucketLabel(bucket, intervalMs, locale, timeZone);
}
