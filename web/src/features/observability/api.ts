import { consoleApi } from '../../shared/api/client';
import type { NativeEvent, NativeHeader } from './trajectory/native-types';

export type ObservabilityScope =
  { kind: 'workspace' } | { kind: 'agent'; agentId: string } | { kind: 'session'; sessionId: string };
export interface Trajectory {
  id: string;
  session_id: string;
  agent_id: string;
  started_at: number;
  last_event_at: number;
}
export interface TrajectoryPage {
  header: NativeHeader | null;
  events: NativeEvent[];
  next_seq: number;
  has_more: boolean;
}
export interface QueryScope {
  orgUuid: string;
  workspaceId: string;
  sessionId?: string;
  agentId?: string;
  startTime?: string;
  endTime?: string;
}

function path(scope: QueryScope, suffix: string, params: Record<string, string>) {
  const query = new URLSearchParams(params);
  if (scope.sessionId) query.set('session_id', scope.sessionId);
  if (scope.agentId) query.set('agent_id', scope.agentId);
  if (scope.startTime) query.set('start_time', scope.startTime);
  if (scope.endTime) query.set('end_time', scope.endTime);
  return `/api/organizations/${encodeURIComponent(scope.orgUuid)}/observability/trajectories${suffix}?${query}`;
}

export function listTrajectories(scope: QueryScope, offset = 0) {
  return consoleApi<{ items: Trajectory[]; next_offset: number | null }>(path(scope, '', { offset: String(offset) }), {
    headers: { 'X-Workspace-ID': scope.workspaceId },
  });
}

export function trajectoryPage(scope: QueryScope, id: string, after = -1, signal?: AbortSignal) {
  return consoleApi<TrajectoryPage>(path(scope, `/${encodeURIComponent(id)}`, { after_seq: String(after) }), {
    signal,
    headers: { 'X-Workspace-ID': scope.workspaceId },
  });
}
