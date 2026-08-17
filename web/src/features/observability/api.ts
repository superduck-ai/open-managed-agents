import { anthropicBetaApi } from '../../shared/api/anthropic';
import { consoleApi } from '../../shared/api/client';
import type {
  ObservabilityDashboard,
  ObservabilityPanelResult,
  ObservabilityScopeOption,
  ObservabilityTraceDetail,
  ObservabilityTraceList,
  PanelQueryVariables,
} from './types';

const SCOPE_LIST_LIMIT = 100;

function orgPath(orgUuid: string, suffix: string) {
  return `/api/organizations/${encodeURIComponent(orgUuid)}/observability${suffix}`;
}

type AgentListRow = { id: string; name?: string | null };
type SessionListRow = { id: string; title?: string | null };

export function listObservabilityAgents(workspaceId: string) {
  return anthropicBetaApi.agents
    .list<AgentListRow>({ limit: SCOPE_LIST_LIMIT, include_archived: true }, workspaceId)
    .then(
      (page) =>
        (page.data ?? []).map((agent) => ({
          id: agent.id,
          label: agent.name?.trim() || agent.id,
          description: agent.name?.trim() ? agent.id : undefined,
        })) satisfies ObservabilityScopeOption[],
    );
}

type AgentVersionRow = { version: number };

export function listObservabilityAgentVersions(agentId: string, workspaceId: string) {
  return anthropicBetaApi.agents.versions
    .list<AgentVersionRow>(agentId, { limit: SCOPE_LIST_LIMIT }, workspaceId)
    .then((page) => {
      const versions = [
        ...new Set(
          (page.data ?? []).map((row) => row.version).filter((version) => Number.isInteger(version) && version > 0),
        ),
      ].sort((left, right) => right - left);
      return versions.map((version) => ({
        id: String(version),
        label: `v${version}`,
      })) satisfies ObservabilityScopeOption[];
    });
}

export function listObservabilitySessions(workspaceId: string, agentId?: string) {
  const params: Record<string, unknown> = {
    limit: SCOPE_LIST_LIMIT,
    include_archived: true,
  };
  if (agentId) {
    params.agent_id = agentId;
  }
  return anthropicBetaApi.sessions.list<SessionListRow>(params, workspaceId).then(
    (page) =>
      (page.data ?? []).map((session) => ({
        id: session.id,
        label: session.title?.trim() || session.id,
        description: session.title?.trim() ? session.id : undefined,
      })) satisfies ObservabilityScopeOption[],
  );
}

function workspaceHeaders(workspaceId: string) {
  return { 'X-Workspace-ID': workspaceId };
}

export function getObservabilityDashboard(orgUuid: string, workspaceId: string) {
  return consoleApi<ObservabilityDashboard>(orgPath(orgUuid, '/dashboard'), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function queryObservabilityPanel(
  orgUuid: string,
  workspaceId: string,
  queryRef: string,
  variables: Record<string, unknown>,
) {
  return consoleApi<ObservabilityPanelResult>(orgPath(orgUuid, '/panels/query'), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    body: JSON.stringify({ query_ref: queryRef, variables }),
  });
}

export function listObservabilityTraces(
  orgUuid: string,
  workspaceId: string,
  params: PanelQueryVariables & { trace_id?: string; status?: string; offset?: number },
) {
  const search = new URLSearchParams({
    start_time: params.start_time,
    end_time: params.end_time,
  });
  if (params.agent_id) search.set('agent_id', params.agent_id);
  if (params.session_id) search.set('session_id', params.session_id);
  for (const version of params.agent_version ?? []) {
    search.append('agent_version', String(version));
  }
  if (params.trace_id) search.set('trace_id', params.trace_id);
  if (params.status) search.set('status', params.status);
  if (params.offset) search.set('offset', String(params.offset));
  return consoleApi<ObservabilityTraceList>(`${orgPath(orgUuid, '/traces')}?${search.toString()}`, {
    headers: workspaceHeaders(workspaceId),
  });
}

export function getObservabilityTrace(
  orgUuid: string,
  workspaceId: string,
  traceId: string,
  params: Pick<PanelQueryVariables, 'start_time' | 'end_time' | 'agent_id' | 'session_id' | 'agent_version'>,
) {
  const search = new URLSearchParams({ start_time: params.start_time, end_time: params.end_time });
  if (params.agent_id) search.set('agent_id', params.agent_id);
  if (params.session_id) search.set('session_id', params.session_id);
  for (const version of params.agent_version ?? []) {
    search.append('agent_version', String(version));
  }
  const query = search.toString();
  const path = orgPath(orgUuid, `/traces/${encodeURIComponent(traceId)}`);
  return consoleApi<ObservabilityTraceDetail>(`${path}?${query}`, {
    headers: workspaceHeaders(workspaceId),
  });
}
