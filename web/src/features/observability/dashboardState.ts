import { useCallback, useEffect, useRef, useState } from 'react';
import { DASHBOARD_TABS, type DashboardTab } from './api';
import { DEFAULT_RANGE, REFRESH_INTERVALS, type RawTimeRange, type TimeZoneMode } from './time/range';

export interface DashboardState {
  range: RawTimeRange;
  refresh: string;
  timeZone: TimeZoneMode;
  tab: DashboardTab;
  agentId: string;
  sessionId: string;
  versions: number[];
  model: string;
  tool: string;
  viewPanel: string;
}

const PARAMS = {
  from: 'from',
  to: 'to',
  refresh: 'refresh',
  timeZone: 'timezone',
  tab: 'obs_tab',
  agentId: 'agent_id',
  sessionId: 'session_id',
  versions: 'agent_version',
  model: 'model',
  tool: 'tool',
  viewPanel: 'view_panel',
} as const;

const TEXT_FIELDS = ['agentId', 'sessionId', 'model', 'tool', 'viewPanel'] as const;

export function readDashboardState(search: string): DashboardState {
  const params = new URLSearchParams(search);
  const text = (key: keyof typeof PARAMS) => params.get(PARAMS[key]) ?? '';
  const tab = DASHBOARD_TABS.find(([id]) => id === text('tab'))?.[0] ?? 'overview';
  return {
    range: { from: text('from') || DEFAULT_RANGE.from, to: text('to') || DEFAULT_RANGE.to },
    refresh: (REFRESH_INTERVALS as readonly string[]).includes(text('refresh')) ? text('refresh') : '',
    timeZone: text('timeZone') === 'utc' ? 'utc' : 'browser',
    tab,
    agentId: text('agentId'),
    sessionId: text('sessionId'),
    versions: params
      .getAll(PARAMS.versions)
      .map(Number)
      .filter((version) => Number.isInteger(version) && version > 0),
    model: text('model'),
    tool: text('tool'),
    viewPanel: text('viewPanel'),
  };
}

function setParam(params: URLSearchParams, key: string, value: string, fallback = '') {
  if (value && value !== fallback) params.set(key, value);
  else params.delete(key);
}

export function dashboardSearch(search: string, state: DashboardState) {
  const params = new URLSearchParams(search);
  setParam(params, PARAMS.from, state.range.from, DEFAULT_RANGE.from);
  setParam(params, PARAMS.to, state.range.to, DEFAULT_RANGE.to);
  setParam(params, PARAMS.refresh, state.refresh);
  setParam(params, PARAMS.timeZone, state.timeZone, 'browser');
  setParam(params, PARAMS.tab, state.tab, 'overview');
  TEXT_FIELDS.forEach((field) => setParam(params, PARAMS[field], state[field]));
  params.delete(PARAMS.versions);
  state.versions.forEach((version) => params.append(PARAMS.versions, String(version)));
  const query = params.toString();
  return query ? `?${query}` : '';
}

export type StateUpdate = Partial<DashboardState>;

export function useDashboardState() {
  const [state, setState] = useState(() => readDashboardState(window.location.search));
  const pendingWrite = useRef<'push' | 'replace' | null>(null);
  useEffect(() => {
    const restore = () => setState(readDashboardState(window.location.search));
    window.addEventListener('popstate', restore);
    return () => window.removeEventListener('popstate', restore);
  }, []);
  useEffect(() => {
    const mode = pendingWrite.current;
    pendingWrite.current = null;
    if (!mode) return;
    const search = dashboardSearch(window.location.search, state);
    if (search === window.location.search) return;
    const url = `${window.location.pathname}${search}${window.location.hash}`;
    if (mode === 'push') window.history.pushState(null, '', url);
    else window.history.replaceState(null, '', url);
  }, [state]);
  const update = useCallback((patch: StateUpdate, mode: 'push' | 'replace' = 'replace') => {
    if (pendingWrite.current !== 'push') pendingWrite.current = mode;
    setState((current) => ({ ...current, ...patch }));
  }, []);
  return [state, update] as const;
}
