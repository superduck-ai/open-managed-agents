import { consoleApi } from '../../shared/api/client';
import type { NativeEvent, NativeHeader } from './trajectory/native-types';

export type ObservabilityScope = { kind: 'workspace' } | { kind: 'agent'; agentId: string };
export type SessionSort = 'recent' | 'duration' | 'errors';
export interface SessionTrajectorySummary {
  session_id: string;
  title: string | null;
  agent_id: string;
  agent_version: number;
  status: string;
  subagent_count: number;
  started_at: number;
  last_event_at: number;
  duration_ms: number;
  error_turns: number;
}
export interface TrajectorySegment {
  id: string;
  started_at: string;
}
export interface TrajectoryPage {
  header: NativeHeader | null;
  events: NativeEvent[];
  next_seq: number;
  has_more: boolean;
  segment: string;
  segments: TrajectorySegment[];
}
export interface QueryScope {
  orgUuid: string;
  workspaceId: string;
  agentId?: string;
  sessionId?: string;
  startTime?: string;
  endTime?: string;
}
export interface TrajectoryTarget {
  sessionId: string;
  threadId?: string;
  segment?: string;
}

function path(scope: QueryScope, suffix: string, params: Record<string, string>) {
  const query = new URLSearchParams(params);
  if (scope.agentId) query.set('agent_id', scope.agentId);
  if (scope.sessionId) query.set('session_id', scope.sessionId);
  if (scope.startTime) query.set('start_time', scope.startTime);
  if (scope.endTime) query.set('end_time', scope.endTime);
  return `/api/organizations/${encodeURIComponent(scope.orgUuid)}/observability/trajectories${suffix}?${query}`;
}

export function listTrajectories(scope: QueryScope, offset = 0, sort: SessionSort = 'recent') {
  return consoleApi<{ items: SessionTrajectorySummary[]; next_offset: number | null }>(
    path(scope, '', { offset: String(offset), sort }),
    { headers: { 'X-Workspace-ID': scope.workspaceId } },
  );
}

export const DASHBOARD_TABS = [
  ['overview', 'Overview'],
  ['models', 'Models'],
  ['tools', 'Tools'],
  ['subagents', 'Sub-agents'],
] as const;

export type DashboardTab = (typeof DASHBOARD_TABS)[number][0];
export type PanelUnit = 'count' | 'tokens' | 'duration_ms' | 'percent' | 'version' | 'text';
export interface DashboardColumn {
  key: string;
  unit: PanelUnit;
}
interface PanelBase {
  id: string;
  span: number;
  unit?: PanelUnit;
  chart?: 'bar' | 'line' | 'area';
  stacked?: boolean;
  compare?: boolean;
  columns?: DashboardColumn[];
}
export interface StatData {
  value: number | null;
  previous?: number | null;
  secondary?: number | null;
  trend?: SeriesPoint[];
}
export interface PercentilesData {
  p50: number | null;
  p90: number | null;
  p95: number | null;
}
export interface SeriesPoint {
  t: number;
  v: number | null;
}
export interface SeriesData {
  key: string;
  points: SeriesPoint[];
}
export interface BarData {
  name: string;
  value: number;
}
export type TableRowData = Record<string, string | number | null>;
export type DashboardPanel =
  | (PanelBase & { render: 'stat'; data: StatData })
  | (PanelBase & { render: 'percentiles'; data: PercentilesData })
  | (PanelBase & { render: 'series'; data: SeriesData[] })
  | (PanelBase & { render: 'bars'; data: BarData[] })
  | (PanelBase & { render: 'table' | 'subagents'; data: { rows: TableRowData[] } });
export interface ObservabilityDashboard {
  tab: DashboardTab;
  interval_ms: number;
  filter_options: string[];
  panels: DashboardPanel[];
}
export interface DashboardQuery {
  agentVersions: number[];
  model?: string;
  tool?: string;
}

export function observabilityDashboard(scope: QueryScope, tab: DashboardTab, filters: DashboardQuery) {
  const query = new URLSearchParams();
  if (scope.agentId) query.set('agent_id', scope.agentId);
  if (scope.sessionId) query.set('session_id', scope.sessionId);
  if (scope.startTime) query.set('start_time', scope.startTime);
  if (scope.endTime) query.set('end_time', scope.endTime);
  filters.agentVersions.forEach((version) => query.append('agent_version', String(version)));
  if (filters.model) query.set('model', filters.model);
  if (filters.tool) query.set('tool', filters.tool);
  return consoleApi<ObservabilityDashboard>(
    `/api/organizations/${encodeURIComponent(scope.orgUuid)}/observability/dashboards/${tab}?${query}`,
    { headers: { 'X-Workspace-ID': scope.workspaceId } },
  );
}

export function trajectoryPage(scope: QueryScope, target: TrajectoryTarget, after = -1, signal?: AbortSignal) {
  const params: Record<string, string> = { after_seq: String(after) };
  if (target.threadId) params.thread_id = target.threadId;
  if (target.segment) params.segment = target.segment;
  return consoleApi<TrajectoryPage>(path(scope, `/${encodeURIComponent(target.sessionId)}`, params), {
    signal,
    headers: { 'X-Workspace-ID': scope.workspaceId },
  });
}
