import { useFormatters } from '../../shared/i18n';
import type { ObservabilityPanelColumn } from './types';

type Formatters = ReturnType<typeof useFormatters>;

export function formatDurationMs(ms: number, formatters: Formatters) {
  if (!Number.isFinite(ms)) {
    return '—';
  }
  const abs = Math.abs(ms);
  if (abs < 1000) {
    return `${formatters.number(Math.round(ms))}ms`;
  }
  if (abs < 60_000) {
    return `${formatters.number(ms / 1000, { maximumFractionDigits: 1 })}s`;
  }
  const minutes = Math.floor(abs / 60_000);
  const seconds = Math.round((abs % 60_000) / 1000);
  return `${formatters.number(minutes)}m ${formatters.number(seconds)}s`;
}

export function formatChangePercent(value: number | null | undefined, formatters: Formatters) {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '—';
  }
  const sign = value > 0 ? '+' : '';
  return `${sign}${formatters.number(value, { maximumFractionDigits: 1 })}%`;
}

export function formatStatValue(value: number | null | undefined, unit: string, formatters: Formatters) {
  if (value === null || value === undefined || !Number.isFinite(value)) {
    return '—';
  }
  if (unit === 'usd') {
    return formatters.currency(value, 'USD', { maximumFractionDigits: value >= 10 ? 2 : 4 });
  }
  if (unit === 'duration_ms') {
    return formatDurationMs(value, formatters);
  }
  if (unit === 'percent') {
    return `${formatters.number(value, { maximumFractionDigits: 1 })}%`;
  }
  if (unit === 'tokens') {
    return formatters.number(value, { notation: 'compact', maximumFractionDigits: 1 });
  }
  return formatters.number(value, { maximumFractionDigits: Number.isInteger(value) ? 0 : 1 });
}

export function formatTimeseriesYTick(value: number, formatted: string) {
  if (Number.isFinite(value) && Math.abs(value) < 1e-6) {
    return '';
  }
  return formatted;
}

export function formatTableCell(value: unknown, column: ObservabilityPanelColumn, formatters: Formatters) {
  if (value === null || value === undefined || value === '') {
    return '—';
  }
  const numeric = typeof value === 'number' ? value : Number(value);
  const isNumeric =
    typeof value === 'number' || (typeof value === 'string' && value.trim() !== '' && Number.isFinite(numeric));
  if (isNumeric) {
    if (column.format === 'duration_ms') {
      return formatDurationMs(numeric, formatters);
    }
    if (column.format === 'percent') {
      return `${formatters.number(numeric, { maximumFractionDigits: 1 })}%`;
    }
    if (column.format === 'tokens') {
      return formatters.number(numeric, { notation: 'compact', maximumFractionDigits: 1 });
    }
    if (column.format === 'number') {
      return formatters.number(numeric, { maximumFractionDigits: Number.isInteger(numeric) ? 0 : 2 });
    }
  }
  return String(value);
}

export function formatHeaderTimestamp(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export function formatChartHoverTimestamp(label: unknown) {
  if (typeof label === 'number' && Number.isFinite(label)) {
    return formatHeaderTimestamp(new Date(label).toISOString());
  }
  if (typeof label === 'string' && label !== '') {
    return formatHeaderTimestamp(label);
  }
  return undefined;
}

export function formatTraceTimestamp(value: string, formatters: Formatters) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  const millis = String(date.getMilliseconds()).padStart(3, '0');
  return `${formatters.date(date, { year: 'numeric', month: '2-digit', day: '2-digit' })} ${formatters.time(date, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })}.${millis}`;
}

export function formatWaterfallTick(ms: number, formatters: Formatters) {
  if (!Number.isFinite(ms) || ms <= 0) {
    return '0μs';
  }
  if (ms < 1) {
    return `${formatters.number(ms * 1000, { maximumFractionDigits: 0 })}μs`;
  }
  if (ms < 1000) {
    return `${formatters.number(ms, { maximumFractionDigits: ms >= 100 ? 0 : 1 })}ms`;
  }
  return `${formatters.number(ms / 1000, { maximumFractionDigits: 2 })}s`;
}

export function formatTimeRangeAbsolute(startIso: string, endIso: string) {
  return `${formatHeaderTimestamp(startIso)} ~ ${formatHeaderTimestamp(endIso)}`;
}

export const CATEGORY_AXIS_MIN_WIDTH = 72;
export const CATEGORY_AXIS_MAX_WIDTH = 168;
export const CATEGORY_BAR_MAX_SIZE = 20;

const CATEGORY_TICK_CHAR_PX = 8;
const CATEGORY_TICK_PAD_PX = 20;

export function shortenResourceId(id: string, max = 22) {
  if (id.length <= max) {
    return id;
  }
  const tail = 6;
  const head = Math.max(8, max - tail - 1);
  return `${id.slice(0, head)}…${id.slice(-tail)}`;
}

export function panelCategoryLabel(name: string, msg: (id: string, fallback: string) => string) {
  return panelNamedLabel('category', name, msg);
}

export function categoryAxisWidth(labels: readonly string[]) {
  const longest = labels.reduce((width, label) => Math.max(width, [...label].length), 0);
  return Math.min(
    CATEGORY_AXIS_MAX_WIDTH,
    Math.max(CATEGORY_AXIS_MIN_WIDTH, Math.ceil(longest * CATEGORY_TICK_CHAR_PX + CATEGORY_TICK_PAD_PX)),
  );
}

export function truncateCategoryTick(label: string, axisWidth: number) {
  const maxChars = Math.max(4, Math.floor((axisWidth - CATEGORY_TICK_PAD_PX) / CATEGORY_TICK_CHAR_PX));
  const chars = [...label];
  if (chars.length <= maxChars) {
    return label;
  }
  return `${chars.slice(0, maxChars - 1).join('')}…`;
}

export function categoryValueAxisMax(values: readonly number[], integerTicks = false) {
  const dataMax = values.reduce((max, value) => (value > max ? value : max), 0);
  if (dataMax <= 0) {
    return 1;
  }
  const padded = dataMax * 1.08;
  if (!integerTicks) {
    return padded;
  }
  return Math.max(Math.ceil(padded), Math.ceil(dataMax) + 1);
}

export const COMPOUND_SERIES_SEPARATOR = ' · ';

export function splitCompoundSeries(name: string) {
  const index = name.lastIndexOf(COMPOUND_SERIES_SEPARATOR);
  if (index <= 0 || index + COMPOUND_SERIES_SEPARATOR.length >= name.length) {
    return null;
  }
  return {
    base: name.slice(0, index),
    suffix: name.slice(index + COMPOUND_SERIES_SEPARATOR.length),
  };
}

export function formatSeriesParts(name: string, msg: (id: string, fallback: string) => string) {
  const compound = splitCompoundSeries(name);
  if (!compound) {
    const full = panelNamedLabel('series', name, msg);
    return { full, base: full };
  }
  const base = panelNamedLabel('series', compound.base, msg);
  const suffix = panelNamedLabel('series', compound.suffix, msg);
  return { full: `${base}${COMPOUND_SERIES_SEPARATOR}${suffix}`, base, suffix };
}

export function panelSeriesLabel(name: string, msg: (id: string, fallback: string) => string) {
  return formatSeriesParts(name, msg).full;
}

function panelNamedLabel(kind: 'category' | 'series', name: string, msg: (id: string, fallback: string) => string) {
  if (!name) {
    return '—';
  }
  return msg(`observability.${kind}.${name}`, name);
}
