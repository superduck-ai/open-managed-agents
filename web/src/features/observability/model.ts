import type {
  ObservabilityPanel,
  ObservabilityPanelResult,
  ObservabilityTabId,
  ObservabilityVariableSpec,
  PanelQueryVariables,
  TimeseriesSeries,
} from './types';

export type TimePresetId = '15m' | '1h' | '6h' | '24h' | '7d' | '30d' | 'custom';

export type ObservabilityFilters = {
  preset: TimePresetId;
  start: string;
  end: string;
  agentId: string;
  sessionId: string;
  agentVersions: number[];
  models: string[];
  tools: string[];
  traceId: string;
  status: '' | 'ok' | 'error';
};

const PRESET_MS: Record<Exclude<TimePresetId, 'custom'>, number> = {
  '15m': 15 * 60 * 1000,
  '1h': 60 * 60 * 1000,
  '6h': 6 * 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
};

const MAX_RANGE_MS = 30 * 24 * 60 * 60 * 1000;

export const TIME_PRESETS: Array<{ id: Exclude<TimePresetId, 'custom'>; labelId: string; fallback: string }> = [
  { id: '15m', labelId: 'observability.filter.15m', fallback: 'Last 15 minutes' },
  { id: '1h', labelId: 'observability.filter.1h', fallback: 'Last hour' },
  { id: '6h', labelId: 'observability.filter.6h', fallback: 'Last 6 hours' },
  { id: '24h', labelId: 'observability.filter.24h', fallback: 'Last 24 hours' },
  { id: '7d', labelId: 'observability.filter.7d', fallback: 'Last 7 days' },
  { id: '30d', labelId: 'observability.filter.30d', fallback: 'Last 30 days' },
];

export function defaultObservabilityFilters(now = Date.now()): ObservabilityFilters {
  const end = new Date(now);
  const start = new Date(now - PRESET_MS['24h']);
  return {
    preset: '24h',
    start: start.toISOString(),
    end: end.toISOString(),
    agentId: '',
    sessionId: '',
    agentVersions: [],
    models: [],
    tools: [],
    traceId: '',
    status: '',
  };
}

export function filtersForPreset(
  preset: Exclude<TimePresetId, 'custom'>,
  now = Date.now(),
): Pick<ObservabilityFilters, 'preset' | 'start' | 'end'> {
  return {
    preset,
    start: new Date(now - PRESET_MS[preset]).toISOString(),
    end: new Date(now).toISOString(),
  };
}

/**
 * Relative presets keep their meaning across refreshes: re-anchor the window
 * to the current clock instead of reusing the timestamps captured when the
 * preset was applied. Custom (absolute) ranges are returned unchanged.
 */
export function refreshedTimeRange(filters: ObservabilityFilters, now = Date.now()): ObservabilityFilters {
  if (filters.preset === 'custom') {
    return filters;
  }
  return { ...filters, ...filtersForPreset(filters.preset, now) };
}

export function customRangeIsValid(startIso: string, endIso: string) {
  const start = Date.parse(startIso);
  const end = Date.parse(endIso);
  return Number.isFinite(start) && Number.isFinite(end) && end > start && end - start <= MAX_RANGE_MS;
}

export function clampedCustomRange(
  startIso: string,
  endIso: string,
): Pick<ObservabilityFilters, 'preset' | 'start' | 'end'> | null {
  const start = Date.parse(startIso);
  const end = Date.parse(endIso);
  if (!Number.isFinite(start) || !Number.isFinite(end) || start === end) {
    return null;
  }
  const from = Math.min(start, end);
  const to = Math.min(Math.max(start, end), from + MAX_RANGE_MS);
  if (to <= from) {
    return null;
  }
  return { preset: 'custom', start: new Date(from).toISOString(), end: new Date(to).toISOString() };
}

export function zoomOutTimeRange(
  startIso: string,
  endIso: string,
  now = Date.now(),
): Pick<ObservabilityFilters, 'preset' | 'start' | 'end'> {
  const start = Date.parse(startIso);
  const end = Date.parse(endIso);
  const duration = Number.isFinite(start) && Number.isFinite(end) && end > start ? end - start : PRESET_MS['1h'];
  const nextDuration = Math.min(MAX_RANGE_MS, Math.max(duration * 2, PRESET_MS['15m']));
  const nextEnd = Math.min(now, (Number.isFinite(end) ? end : now) + (nextDuration - duration) / 2);
  return {
    preset: 'custom',
    start: new Date(nextEnd - nextDuration).toISOString(),
    end: new Date(nextEnd).toISOString(),
  };
}

export function panelVariablesFromFilters(
  filters: ObservabilityFilters,
  scope: { agentId?: string; sessionId?: string },
  extras: { includeModel?: boolean; includeTool?: boolean } = {},
): PanelQueryVariables {
  const variables: PanelQueryVariables = {
    start_time: filters.start,
    end_time: filters.end,
  };
  const agentId = scope.agentId || filters.agentId.trim();
  const sessionId = scope.sessionId || filters.sessionId.trim();
  if (agentId) variables.agent_id = agentId;
  if (sessionId) variables.session_id = sessionId;
  if (filters.agentVersions.length) variables.agent_version = filters.agentVersions;
  if (extras.includeModel && filters.models.length) variables.model = filters.models;
  if (extras.includeTool && filters.tools.length) variables.tool = filters.tools;
  return variables;
}

export function tabFromSearch(raw: string | null): ObservabilityTabId {
  if (raw === 'model' || raw === 'tool' || raw === 'traces') {
    return raw;
  }
  return 'overview';
}

export function traceIdFromSearch(raw: string | null): string | null {
  const value = raw?.trim() ?? '';
  if (!value || value.length > 128) {
    return null;
  }
  return value;
}

export function panelIdFromSearch(raw: string | null): string | null {
  const value = raw?.trim() ?? '';
  if (!value || value.length > 128 || !/^[a-z][a-z0-9._-]*$/i.test(value)) {
    return null;
  }
  return value;
}

export function tabForPanelId(
  tabs: Array<{ id: string; panels: Array<{ id: string }> }>,
  panelId: string,
): ObservabilityTabId | null {
  for (const tab of tabs) {
    if (tab.panels.some((panel) => panel.id === panelId)) {
      return tabFromSearch(tab.id);
    }
  }
  if (panelId.startsWith('trace.trend.')) {
    return 'traces';
  }
  return null;
}

export function writeWorkspaceObservabilitySearch(
  tab: ObservabilityTabId,
  traceId: string | null,
  panelId: string | null = null,
) {
  if (typeof window === 'undefined') {
    return;
  }
  const url = new URL(window.location.href);
  const nextTab = traceId ? 'traces' : tab;
  if (nextTab === 'overview') {
    url.searchParams.delete('tab');
  } else {
    url.searchParams.set('tab', nextTab);
  }
  if (traceId) {
    url.searchParams.set('trace_id', traceId);
    url.searchParams.delete('panel');
  } else {
    url.searchParams.delete('trace_id');
    if (panelId) {
      url.searchParams.set('panel', panelId);
    } else {
      url.searchParams.delete('panel');
    }
  }
  window.history.replaceState(window.history.state, '', url);
}

export function parseCsvList(value: string) {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

export function toggleAgentVersion(selected: number[], version: number) {
  if (selected.includes(version)) {
    return selected.filter((item) => item !== version);
  }
  return [...selected, version].sort((left, right) => right - left);
}

export function pickDeclaredVariables(
  specs: ObservabilityVariableSpec[] | undefined,
  variables: PanelQueryVariables,
): Record<string, unknown> {
  const declared = specs ?? [
    { name: 'start_time', type: 'time' as const, required: true },
    { name: 'end_time', type: 'time' as const, required: true },
  ];
  const out: Record<string, unknown> = {};
  for (const spec of declared) {
    const value = (variables as Record<string, unknown>)[spec.name];
    if (value === undefined || value === '') {
      continue;
    }
    if (Array.isArray(value) && value.length === 0) {
      continue;
    }
    out[spec.name] = value;
  }
  return out;
}

export function isPanelDataEmpty(
  result: ObservabilityPanelResult | undefined,
  renderType: ObservabilityPanel['render_type'],
) {
  if (!result) {
    return true;
  }
  if (renderType === 'stat') {
    const data = result.data as { current: number | null };
    return data.current === null || data.current === undefined;
  }
  if (renderType === 'timeseries') {
    const data = result.data as { series: Array<{ points: unknown[] }> };
    return !data.series?.some((series) => series.points?.length);
  }
  if (renderType === 'categorical') {
    const data = result.data as { items: unknown[] };
    return !data.items?.length;
  }
  if (renderType === 'multistat') {
    const data = result.data as { items: unknown[]; series?: Array<{ points: unknown[] }> };
    return !data.items?.length && !data.series?.some((series) => series.points?.length);
  }
  const data = result.data as { rows: unknown[] };
  return !data.rows?.length;
}

const DATE_TIME_INPUT_RE = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})(?::(\d{2}))?$/;

/** Parses "YYYY-MM-DD HH:mm[:ss]" as local time. Returns null for bad format or overflow (e.g. 2026-02-31). */
export function parseDateTimeInput(text: string): Date | null {
  const match = DATE_TIME_INPUT_RE.exec(text.trim());
  if (!match) {
    return null;
  }
  const [, year, month, day, hours, minutes, seconds = '00'] = match;
  const date = new Date(Number(year), Number(month) - 1, Number(day), Number(hours), Number(minutes), Number(seconds));
  const roundTrips =
    date.getFullYear() === Number(year) &&
    date.getMonth() === Number(month) - 1 &&
    date.getDate() === Number(day) &&
    date.getHours() === Number(hours) &&
    date.getMinutes() === Number(minutes) &&
    date.getSeconds() === Number(seconds);
  return roundTrips ? date : null;
}

/** Formats an ISO timestamp or Date as local "YYYY-MM-DD HH:mm:ss" for the range text inputs. */
export function formatDateTimeInput(value: string | Date): string {
  const date = typeof value === 'string' ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  const pad = (n: number) => String(n).padStart(2, '0');
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    ` ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
  );
}

export function replaceDateKeepingTime(existing: string, day: Date, fallbackTime: string): string {
  const parsed = parseDateTimeInput(existing);
  const time = parsed ? formatDateTimeInput(parsed).slice(11) : fallbackTime;
  return `${formatDateTimeInput(day).slice(0, 10)} ${time}`;
}

export function customRangeFieldErrors(
  start: Date | null,
  end: Date | null,
  messages: { format: string; range: string },
): { start?: string; end?: string } {
  if (!start || !end) {
    return {
      ...(start ? {} : { start: messages.format }),
      ...(end ? {} : { end: messages.format }),
    };
  }
  if (!customRangeIsValid(start.toISOString(), end.toISOString())) {
    return { end: messages.range };
  }
  return {};
}

export function utcOffsetLabel(date = new Date()): string {
  const offsetMinutes = -date.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? '+' : '-';
  const abs = Math.abs(offsetMinutes);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `UTC${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
}

export function timeAxisDomain(startIso: string, endIso: string): [number, number] | null {
  const start = Date.parse(startIso);
  const end = Date.parse(endIso);
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
    return null;
  }
  return [start, end];
}

export function timeAxisTickOptions(spanMs: number): Intl.DateTimeFormatOptions {
  if (spanMs <= PRESET_MS['24h']) {
    return { hour: '2-digit', minute: '2-digit' };
  }
  if (spanMs <= PRESET_MS['7d']) {
    return { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' };
  }
  return { month: 'short', day: 'numeric' };
}

export function bucketIntervalMs(spanMs: number) {
  if (spanMs <= PRESET_MS['15m']) {
    return 30 * 1000;
  }
  if (spanMs <= PRESET_MS['1h']) {
    return 60 * 1000;
  }
  if (spanMs <= PRESET_MS['6h']) {
    return 5 * 60 * 1000;
  }
  if (spanMs <= PRESET_MS['24h']) {
    return 30 * 60 * 1000;
  }
  if (spanMs <= PRESET_MS['7d']) {
    return 3 * 60 * 60 * 1000;
  }
  return 12 * 60 * 60 * 1000;
}

export function timeAtPlotX(offsetX: number, width: number, startMs: number, endMs: number) {
  if (!(width > 0) || !(endMs > startMs)) {
    return startMs;
  }
  const ratio = Math.min(1, Math.max(0, offsetX / width));
  return startMs + ratio * (endMs - startMs);
}

export function plotYAtValue(value: number, min: number, max: number, height: number) {
  if (!(height > 0) || max <= min) {
    return height;
  }
  const ratio = Math.min(1, Math.max(0, (value - min) / (max - min)));
  return (1 - ratio) * height;
}

function finiteSeriesSample(row: Record<string, number | null | undefined>, name: string) {
  const t = row.t;
  const value = row[name];
  if (typeof t !== 'number' || !Number.isFinite(t) || typeof value !== 'number' || !Number.isFinite(value)) {
    return null;
  }
  return { t, value };
}

function seriesHasGap(
  rows: Array<Record<string, number | null | undefined>>,
  name: string,
  fromMs: number,
  toMs: number,
) {
  return rows.some((row) => {
    const t = row.t;
    if (typeof t !== 'number' || t <= fromMs || t >= toMs) {
      return false;
    }
    const value = row[name];
    return typeof value !== 'number' || !Number.isFinite(value);
  });
}

export function interpolateSeriesValue(
  rows: Array<Record<string, number | null | undefined>>,
  name: string,
  tMs: number,
) {
  if (!Number.isFinite(tMs)) {
    return null;
  }
  let left: { t: number; value: number } | null = null;
  let right: { t: number; value: number } | null = null;
  for (const row of rows) {
    const sample = finiteSeriesSample(row, name);
    if (!sample) {
      continue;
    }
    if (sample.t <= tMs && (!left || sample.t >= left.t)) {
      left = sample;
    }
    if (sample.t >= tMs && (!right || sample.t <= right.t)) {
      right = sample;
    }
  }
  if (!left || !right) {
    return null;
  }
  if (right.t === left.t) {
    return left.value;
  }
  if (seriesHasGap(rows, name, left.t, right.t)) {
    return null;
  }
  return left.value + ((tMs - left.t) / (right.t - left.t)) * (right.value - left.value);
}

function niceCeil(value: number) {
  const base = 10 ** Math.floor(Math.log10(value));
  const frac = value / base;
  if (frac <= 1) return base;
  if (frac <= 2) return 2 * base;
  if (frac <= 5) return 5 * base;
  return 10 * base;
}

// Y 轴与悬停点必须共用同一 domain，否则悬停点会偏离曲线；上界取整到"好看"的刻度值。
export function niceValueDomain(
  rows: Array<Record<string, number | null | undefined>>,
  keys: string[],
  stacked: boolean,
): [number, number] {
  const [min, max] = timeseriesValueDomain(rows, keys, stacked);
  return [min < 0 ? -niceCeil(-min) : min, max > 0 ? niceCeil(max) : max];
}

export function timeseriesValueDomain(
  rows: Array<Record<string, number | null | undefined>>,
  keys: string[],
  stacked: boolean,
): [number, number] {
  let min = 0;
  let max = 0;
  let any = false;
  for (const row of rows) {
    let total = 0;
    for (const key of keys) {
      const value = row[key];
      if (typeof value !== 'number' || !Number.isFinite(value)) {
        continue;
      }
      any = true;
      if (stacked) {
        total += value;
        continue;
      }
      if (value < min) {
        min = value;
      }
      if (value > max) {
        max = value;
      }
    }
    if (stacked) {
      if (total < min) {
        min = total;
      }
      if (total > max) {
        max = total;
      }
    }
  }
  if (!any) {
    return [0, 1];
  }
  if (max === min) {
    // 全 0 时不能塌成 [0, 0]，否则 Y 轴没有高度；上界 +1 让计数图仍能画在底部。
    return [min, min + 1];
  }
  return [min, max];
}

// 值为 0 的点落在 Y 轴下沿，圆点会被 clip 切成贴底的半圆鼓包，看起来像有数据。
export function timeseriesDotVisible(t: unknown, value: unknown, realTimestamps: Set<number>) {
  if (typeof t !== 'number' || !Number.isFinite(t) || !realTimestamps.has(t)) {
    return false;
  }
  return typeof value === 'number' && Number.isFinite(value) && Math.abs(value) >= 1e-6;
}

export function timeseriesAllowsDecimalTicks(unit: string) {
  return unit === 'duration_ms' || unit === 'percent' || unit === 'usd';
}

export function plotLocalPoint(
  clientX: number,
  clientY: number,
  plot: { left: number; top: number; width: number; height: number },
) {
  if (!(plot.width > 0) || !(plot.height > 0)) {
    return null;
  }
  const x = clientX - plot.left;
  const y = clientY - plot.top;
  if (x < 0 || y < 0 || x > plot.width || y > plot.height) {
    return null;
  }
  return { x, y };
}

export function plotOverlayFrame(
  clientX: number,
  clientY: number,
  host: { left: number; top: number },
  plot: { left: number; top: number; width: number; height: number },
) {
  const local = plotLocalPoint(clientX, clientY, plot);
  if (!local) {
    return null;
  }
  return {
    ...local,
    width: plot.width,
    height: plot.height,
    left: plot.left - host.left,
    top: plot.top - host.top,
  };
}

const PERCENTILE_NAME = /^p(\d+)$/;

export function multistatItemNames(items: Array<{ name: string }>) {
  const names = [...new Set(items.map((item) => item.name).filter(Boolean))];
  return names.sort((left, right) => {
    const leftPercentile = PERCENTILE_NAME.exec(left);
    const rightPercentile = PERCENTILE_NAME.exec(right);
    if (leftPercentile && rightPercentile) {
      return Number(leftPercentile[1]) - Number(rightPercentile[1]);
    }
    return names.indexOf(left) - names.indexOf(right);
  });
}

export function selectedMultistatName(items: Array<{ name: string }>, selected: string) {
  const names = multistatItemNames(items);
  if (names.includes(selected)) {
    return selected;
  }
  return names[0] ?? '';
}

export function mergeTimeseriesRows(series: TimeseriesSeries[]) {
  const parsed = series.map((item) => ({
    name: item.name,
    byT: new Map(item.points.map((point) => [Date.parse(point.timestamp), point.value] as const)),
  }));
  const timestamps = [...new Set(parsed.flatMap((item) => [...item.byT.keys()]))]
    .filter(Number.isFinite)
    .sort((left, right) => left - right);
  return timestamps.map((t) => {
    const row: Record<string, number> = { t };
    for (const item of parsed) {
      const value = item.byT.get(t);
      if (value !== undefined) {
        row[item.name] = value;
      }
    }
    return row;
  });
}

export function fillTimeseriesRows(
  rows: Array<Record<string, number>>,
  names: string[],
  startMs: number,
  endMs: number,
  mode: 'zero' | 'gap',
) {
  if (!(endMs > startMs) || names.length === 0) {
    return rows.map((row) => ({ ...row }));
  }
  const interval = bucketIntervalMs(endMs - startMs);
  if (!(interval > 0)) {
    return rows.map((row) => ({ ...row }));
  }
  const byT = new Map<number, Record<string, number>>();
  for (const row of rows) {
    if (typeof row.t === 'number' && Number.isFinite(row.t)) {
      byT.set(row.t, row);
    }
  }
  const first = Math.floor(startMs / interval) * interval;
  const last = Math.floor((endMs - 1) / interval) * interval;
  const out: Array<Record<string, number | null>> = [];
  for (let t = first; t <= last; t += interval) {
    const existing = byT.get(t);
    const next: Record<string, number | null> = { t };
    for (const name of names) {
      const value = existing?.[name];
      next[name] = typeof value === 'number' && Number.isFinite(value) ? value : mode === 'zero' ? 0 : null;
    }
    out.push(next);
  }
  return out;
}

export function timeseriesAxisDomain(
  startIso: string | undefined,
  endIso: string | undefined,
  rows: Array<{ t?: number }>,
): [number, number] | undefined {
  const declared = timeAxisDomain(startIso ?? '', endIso ?? '');
  if (declared) {
    return declared;
  }
  const first = rows[0]?.t;
  const last = rows[rows.length - 1]?.t;
  if (typeof first === 'number' && typeof last === 'number' && last > first) {
    return [first, last];
  }
  return undefined;
}

export function timeseriesTooltipSide(cursorX: number, plotWidth: number): 'left' | 'right' {
  if (!(plotWidth > 0) || cursorX < plotWidth / 2) {
    return 'right';
  }
  return 'left';
}

const HOVER_CARD_GAP = 12;
const HOVER_CARD_PAD = 8;

export function timeseriesHoverCardAnchor(frame: {
  x: number;
  left: number;
  top: number;
  width: number;
  hostWidth: number;
}) {
  const side = timeseriesTooltipSide(frame.x, frame.width);
  const hostWidth = frame.hostWidth > 0 ? frame.hostWidth : frame.left + frame.width;
  const plotRight = frame.left + frame.width;
  const cursorX = frame.left + frame.x;
  if (side === 'right') {
    return {
      side,
      top: frame.top + HOVER_CARD_PAD,
      left: cursorX + HOVER_CARD_GAP,
      right: Math.max(HOVER_CARD_PAD, hostWidth - plotRight + HOVER_CARD_PAD),
    } as const;
  }
  return {
    side,
    top: frame.top + HOVER_CARD_PAD,
    left: frame.left + HOVER_CARD_PAD,
    right: Math.max(HOVER_CARD_PAD, hostWidth - (cursorX - HOVER_CARD_GAP)),
  } as const;
}

export function nextHiddenSeries(hidden: Set<string>, clicked: string, seriesNames: string[], isolate: boolean) {
  if (isolate) {
    const othersHidden = seriesNames.filter((name) => name !== clicked).every((name) => hidden.has(name));
    if (!hidden.has(clicked) && othersHidden) {
      return new Set<string>();
    }
    return new Set(seriesNames.filter((name) => name !== clicked));
  }
  const next = new Set(hidden);
  if (next.has(clicked)) {
    next.delete(clicked);
    return next;
  }
  if (seriesNames.filter((name) => !next.has(name)).length <= 1) {
    return next;
  }
  next.add(clicked);
  return next;
}

export function isApiError(error: unknown): error is { status: number; message: string } {
  return Boolean(
    error &&
    typeof error === 'object' &&
    'status' in error &&
    typeof (error as { status: unknown }).status === 'number',
  );
}
