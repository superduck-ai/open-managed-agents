import { anthropicBetaApi } from '../../shared/api/anthropic';
import { consoleApi } from '../../shared/api/client';
import { consumeSseBuffer, postJsonSseStream } from '../../shared/api/streaming';
import { type QueryClient } from '@tanstack/react-query';
import { agentDetailCreatedRange, agentDetailStatusValues } from './agents/AgentsResourcePage';
import { credentialAuthBody, normalizeMemoryFolderPath } from './resources/ManagedResources';
import { compareSessionEvents, sessionEventType } from './sessions/SessionDetailPage';
import {
  type AgentApiResponse,
  type AgentCreatedFilter,
  type AgentDetailDeploymentFilters,
  type AgentDetailSessionFilters,
  type AgentListFilters,
  type AgentPageResponse,
  type AgentSearchResponse,
  type AgentSessionAnalyticsOverview,
  type AgentSessionAnalyticsTimeseries,
  type AgentUpdateInput,
  type CreateAgentInput,
  type CredentialFormValues,
  type DeploymentApiResponse,
  type DeploymentRunApiResponse,
  type EnvironmentApiResponse,
  type EnvironmentWorkApiResponse,
  type ManagedEntityApiResponse,
  type ManagedEntityFormValues,
  type ManagedEntityListFilters,
  type ManagedEntitySection,
  type MemoryApiResponse,
  type MemoryFormValues,
  type MemoryStoreApiResponse,
  type PageCursor,
  type PageResponse,
  type QuickstartCreateEnvironmentInput,
  type QuickstartDeploymentInput,
  type QuickstartSessionEvent,
  type QuickstartStreamEvent,
  type SessionApiResponse,
  type SessionDetailDeltaFrames,
  type SessionDetailEventCache,
  type SessionEventCachePatch,
  type SessionResourceApiResponse,
  type SessionThreadApiResponse,
  type VaultApiResponse,
  type VaultCredentialApiResponse,
} from './types';
import { isContentSha256, objectRecord, toRecord } from './utils';

export function workspaceHeaders(workspaceId: string) {
  return workspaceId ? { 'x-workspace-id': workspaceId } : undefined;
}

export function sdkBody(value: object): Record<string, unknown> {
  return value as Record<string, unknown>;
}

export const defaultAgentFilters: AgentListFilters = { created: 'all', status: 'active' };

export const agentsListLimit = 20;

export const agentSearchLimit = 100;

export const agentSearchMaxPages = 3;

export const exactAgentIdPattern = /^agent_(?:staging_|local_)?[0-9a-zA-Z]{20,}$/i;

export async function getEffectiveModelMappings(orgUuid: string) {
  const response = await consoleApi<{ model_mappings?: Record<string, string> }>(
    `/api/organizations/${encodeURIComponent(orgUuid)}/models`,
  );
  return response.model_mappings ?? {};
}

export function createdFilterStartISOString(filter: AgentCreatedFilter) {
  const now = Date.now();
  if (filter === 'last7') {
    return new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString();
  }
  if (filter === 'last30') {
    return new Date(now - 30 * 24 * 60 * 60 * 1000).toISOString();
  }
  return null;
}

export function listAgents(workspaceId: string, page?: PageCursor, filters: AgentListFilters = defaultAgentFilters) {
  const params: Record<string, string | number | boolean> = {
    limit: agentsListLimit,
    include_archived: filters.status === 'all',
  };
  const createdAtGTE = createdFilterStartISOString(filters.created);
  if (createdAtGTE) {
    params['created_at[gte]'] = createdAtGTE;
  }
  if (page) {
    params.page = page;
  }
  return anthropicBetaApi.agents.list<AgentApiResponse>(params, workspaceId) as Promise<AgentPageResponse>;
}

export function searchAgentsByNamePage(
  workspaceId: string,
  name: string,
  limit: number,
  page?: PageCursor,
  filters: AgentListFilters = defaultAgentFilters,
) {
  return consoleApi<AgentPageResponse>('/v1/agents:search?beta=true', {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    body: JSON.stringify({
      name,
      limit,
      include_archived: filters.status === 'all',
      ...(page ? { page } : {}),
    }),
  });
}

export function dedupeAgentsById(agents: AgentApiResponse[]) {
  const seen = new Set<string>();
  return agents.filter((agent) => {
    if (seen.has(agent.id)) {
      return false;
    }
    seen.add(agent.id);
    return true;
  });
}

export async function searchAgentsByName(
  workspaceId: string,
  name: string,
  filters: AgentListFilters = defaultAgentFilters,
): Promise<AgentSearchResponse> {
  const rows: AgentApiResponse[] = [];
  let cursor: PageCursor = null;
  let nextPage: PageCursor = null;
  let truncated = false;

  for (let pageCount = 0; pageCount < agentSearchMaxPages && rows.length < agentSearchLimit; pageCount += 1) {
    const limit = Math.max(1, agentSearchLimit - rows.length);
    const page = await searchAgentsByNamePage(workspaceId, name, limit, cursor, filters);
    rows.push(...(page.data ?? []));
    nextPage = page.next_page ?? null;
    if (!nextPage) {
      break;
    }
    if (rows.length >= agentSearchLimit || pageCount + 1 >= agentSearchMaxPages) {
      truncated = true;
      break;
    }
    cursor = nextPage;
  }

  return {
    data: dedupeAgentsById(rows).slice(0, agentSearchLimit),
    next_page: nextPage,
    truncated,
  };
}

export function createAgent(input: CreateAgentInput, workspaceId: string) {
  return anthropicBetaApi.agents.create<AgentApiResponse>(sdkBody(input), workspaceId);
}

export function archiveAgent(agentId: string, workspaceId: string) {
  return anthropicBetaApi.agents.archive<AgentApiResponse>(agentId, workspaceId);
}

export function retrieveAgent(agentId: string, workspaceId: string, version?: number | null) {
  const params: Record<string, number> = {};
  if (version) {
    params.version = version;
  }
  return anthropicBetaApi.agents.retrieve<AgentApiResponse>(agentId, params, workspaceId);
}

export function listAgentVersions(agentId: string, workspaceId: string) {
  return anthropicBetaApi.agents.versions.list<AgentApiResponse>(
    agentId,
    { limit: 100 },
    workspaceId,
  ) as Promise<AgentPageResponse>;
}

export type AgentSkillApiResponse = {
  id: string;
  type: 'skill';
  display_title: string;
  latest_version: string;
  source: string;
  created_at: string;
  updated_at: string;
};

export function retrieveAgentSkill(skillId: string, workspaceId: string) {
  return anthropicBetaApi.skills.retrieve<AgentSkillApiResponse>(skillId, workspaceId);
}

export function updateAgentDetail(agentId: string, input: AgentUpdateInput, workspaceId: string) {
  return anthropicBetaApi.agents.update<AgentApiResponse>(agentId, sdkBody(input), workspaceId);
}

export function listAgentDetailSessions(agentId: string, workspaceId: string, filters: AgentDetailSessionFilters) {
  const statuses = agentDetailStatusValues(filters.status);
  const params: Record<string, unknown> = {
    limit: 8,
    agent_id: agentId,
  };
  const createdRange = agentDetailCreatedRange(filters.created);
  if (createdRange.gte) {
    params['created_at[gte]'] = createdRange.gte;
  }
  if (createdRange.lte) {
    params['created_at[lte]'] = createdRange.lte;
  }
  if (filters.version) {
    params.agent_version = filters.version;
  }
  if (filters.deploymentId) {
    params.deployment_id = filters.deploymentId;
  }
  if (statuses.length) {
    params.statuses = statuses;
  }
  if (statuses.includes('terminated')) {
    params.include_archived = true;
  }
  if (filters.cursor) {
    params.page = filters.cursor;
  }
  return anthropicBetaApi.sessions.list<SessionApiResponse>(params, workspaceId) as Promise<
    PageResponse<SessionApiResponse>
  >;
}

export function listAgentDetailDeployments(
  agentId: string,
  workspaceId: string,
  filters: AgentDetailDeploymentFilters,
) {
  const params: Record<string, string | number | boolean> = {
    limit: 20,
    agent_id: agentId,
    include_archived: true,
  };
  if (filters.cursor) {
    params.page = filters.cursor;
  }
  return anthropicBetaApi.deployments.list<DeploymentApiResponse>(params, workspaceId) as Promise<
    PageResponse<DeploymentApiResponse>
  >;
}

export function createAgentDetailSession(
  agent: AgentApiResponse,
  values: ManagedEntityFormValues,
  workspaceId: string,
) {
  return anthropicBetaApi.sessions.create<SessionApiResponse>(
    {
      title: values.name.trim() || undefined,
      agent: { type: 'agent', id: agent.id },
      environment_id: values.environmentId,
      vault_ids: values.vaultIds.length ? values.vaultIds : undefined,
    },
    workspaceId,
  );
}

export function createAgentDetailDeployment(
  agent: AgentApiResponse,
  values: ManagedEntityFormValues,
  workspaceId: string,
) {
  return anthropicBetaApi.deployments.create<DeploymentApiResponse>(
    {
      name: values.name.trim(),
      description: values.description.trim() || null,
      agent: { type: 'agent', id: agent.id, version: agent.version },
      environment_id: values.environmentId,
      vault_ids: values.vaultIds,
      metadata: {},
      resources: deploymentResources(values.memoryStoreIds),
      initial_events: deploymentInitialEvents(values.initialMessage),
      schedule: deploymentSchedule(values),
    },
    workspaceId,
  );
}

export function getAgentSessionAnalyticsOverview(orgUuid: string, agentId: string) {
  const params = new URLSearchParams({ agent_id: agentId });
  return consoleApi<AgentSessionAnalyticsOverview>(
    `/api/organizations/${encodeURIComponent(orgUuid)}/analytics/sessions/overview?${params.toString()}`,
  );
}

export function getAgentSessionAnalyticsTimeseries(orgUuid: string, agentId: string, groupBy?: string) {
  const params = new URLSearchParams({ agent_id: agentId });
  if (groupBy) {
    params.set('group_by', groupBy);
  }
  return consoleApi<AgentSessionAnalyticsTimeseries>(
    `/api/organizations/${encodeURIComponent(orgUuid)}/analytics/sessions/timeseries?${params.toString()}`,
  );
}

export function listManagedEntities(
  section: ManagedEntitySection,
  workspaceId: string,
  page?: PageCursor,
  filters?: ManagedEntityListFilters,
) {
  const params: Record<string, unknown> = {
    limit: 5,
    include_archived: filters?.includeArchived ?? false,
  };
  if (page) {
    params.page = page;
  }
  if (filters?.created) {
    const createdRange = agentDetailCreatedRange(filters.created);
    if (createdRange.gte) {
      params['created_at[gte]'] = createdRange.gte;
    }
    if (createdRange.lte) {
      params['created_at[lte]'] = createdRange.lte;
    }
  }
  if (section === 'sessions') {
    if (filters?.agentId) {
      params.agent_id = filters.agentId;
    }
    if (filters?.deploymentId) {
      params.deployment_id = filters.deploymentId;
    }
    if (filters?.statuses?.length) {
      params.statuses = filters.statuses;
    }
  }
  if (section === 'deployments') {
    if (filters?.agentId) {
      params.agent_id = filters.agentId;
    }
    if (filters?.status === 'all') {
      params.include_archived = true;
    } else if (filters?.status) {
      params.status = filters.status;
    }
  }
  switch (section) {
    case 'sessions':
      return anthropicBetaApi.sessions.list<SessionApiResponse>(params, workspaceId) as Promise<
        PageResponse<ManagedEntityApiResponse>
      >;
    case 'deployments':
      return anthropicBetaApi.deployments.list<DeploymentApiResponse>(params, workspaceId) as Promise<
        PageResponse<ManagedEntityApiResponse>
      >;
    case 'environments':
      return anthropicBetaApi.environments.list<EnvironmentApiResponse>(params, workspaceId) as Promise<
        PageResponse<ManagedEntityApiResponse>
      >;
    case 'credential-vaults':
      return anthropicBetaApi.vaults.list<VaultApiResponse>(params, workspaceId) as Promise<
        PageResponse<ManagedEntityApiResponse>
      >;
    case 'memory-stores':
      return anthropicBetaApi.memoryStores.list<MemoryStoreApiResponse>(params, workspaceId) as Promise<
        PageResponse<ManagedEntityApiResponse>
      >;
  }
}

export function retrieveManagedEntity(section: ManagedEntitySection, entityId: string, workspaceId: string) {
  switch (section) {
    case 'sessions':
      return anthropicBetaApi.sessions.retrieve<SessionApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'deployments':
      return anthropicBetaApi.deployments.retrieve<DeploymentApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'environments':
      return anthropicBetaApi.environments.retrieve<EnvironmentApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'credential-vaults':
      return anthropicBetaApi.vaults.retrieve<VaultApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'memory-stores':
      return anthropicBetaApi.memoryStores.retrieve<MemoryStoreApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
  }
}

export function createManagedEntity(
  section: ManagedEntitySection,
  values: ManagedEntityFormValues,
  workspaceId: string,
) {
  const body = createManagedEntityBody(section, values);
  switch (section) {
    case 'sessions':
      return anthropicBetaApi.sessions.create<SessionApiResponse>(
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'deployments':
      return anthropicBetaApi.deployments.create<DeploymentApiResponse>(
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'environments':
      return anthropicBetaApi.environments.create<EnvironmentApiResponse>(
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'credential-vaults':
      return anthropicBetaApi.vaults.create<VaultApiResponse>(body, workspaceId) as Promise<ManagedEntityApiResponse>;
    case 'memory-stores':
      return anthropicBetaApi.memoryStores.create<MemoryStoreApiResponse>(
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
  }
}

export function updateManagedEntity(
  section: ManagedEntitySection,
  entityId: string,
  values: ManagedEntityFormValues,
  workspaceId: string,
) {
  const body = updateManagedEntityBody(section, values);
  switch (section) {
    case 'sessions':
      return anthropicBetaApi.sessions.update<SessionApiResponse>(
        entityId,
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'deployments':
      return anthropicBetaApi.deployments.update<DeploymentApiResponse>(
        entityId,
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'environments':
      return anthropicBetaApi.environments.update<EnvironmentApiResponse>(
        entityId,
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'credential-vaults':
      return anthropicBetaApi.vaults.update<VaultApiResponse>(
        entityId,
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'memory-stores':
      return anthropicBetaApi.memoryStores.update<MemoryStoreApiResponse>(
        entityId,
        body,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
  }
}

export function archiveManagedEntity(section: ManagedEntitySection, entityId: string, workspaceId: string) {
  switch (section) {
    case 'sessions':
      return anthropicBetaApi.sessions.archive<SessionApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'deployments':
      return anthropicBetaApi.deployments.archive<DeploymentApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'environments':
      return anthropicBetaApi.environments.archive<EnvironmentApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'credential-vaults':
      return anthropicBetaApi.vaults.archive<VaultApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
    case 'memory-stores':
      return anthropicBetaApi.memoryStores.archive<MemoryStoreApiResponse>(
        entityId,
        workspaceId,
      ) as Promise<ManagedEntityApiResponse>;
  }
}

export function deleteManagedEntity(section: ManagedEntitySection, entityId: string, workspaceId: string) {
  switch (section) {
    case 'sessions':
      return anthropicBetaApi.sessions.delete<{ id: string; type: string }>(entityId, workspaceId);
    case 'environments':
      return anthropicBetaApi.environments.delete<{ id: string; type: string }>(entityId, workspaceId);
    case 'credential-vaults':
      return anthropicBetaApi.vaults.delete<{ id: string; type: string }>(entityId, workspaceId);
    case 'memory-stores':
      return anthropicBetaApi.memoryStores.delete<{ id: string; type: string }>(entityId, workspaceId);
    case 'deployments':
      return anthropicBetaApi.deployments.archive<{ id: string; type: string }>(entityId, workspaceId);
  }
}

export function runDeployment(deploymentId: string, workspaceId: string) {
  return anthropicBetaApi.deployments.run<unknown>(deploymentId, workspaceId);
}

export function pauseDeployment(deploymentId: string, workspaceId: string) {
  return anthropicBetaApi.deployments.pause<DeploymentApiResponse>(deploymentId, workspaceId);
}

export function unpauseDeployment(deploymentId: string, workspaceId: string) {
  return anthropicBetaApi.deployments.unpause<DeploymentApiResponse>(deploymentId, workspaceId);
}

export function listDeploymentRuns(deploymentId: string, workspaceId: string) {
  return anthropicBetaApi.deploymentRuns.list<DeploymentRunApiResponse>(
    { limit: 60, deployment_id: deploymentId },
    workspaceId,
  ) as Promise<PageResponse<DeploymentRunApiResponse>>;
}

export function listEnvironmentWork(environmentId: string, workspaceId: string) {
  return anthropicBetaApi.environments.work.list<EnvironmentWorkApiResponse>(
    environmentId,
    { limit: 50 },
    workspaceId,
  ) as Promise<PageResponse<EnvironmentWorkApiResponse>>;
}

export function listSessionResources(sessionId: string, workspaceId: string) {
  return anthropicBetaApi.sessions.resources.list<SessionResourceApiResponse>(sessionId, {}, workspaceId) as Promise<
    PageResponse<SessionResourceApiResponse>
  >;
}

export const SESSION_DETAIL_EVENT_PAGE_LIMIT = 500;

export const SESSION_DETAIL_STREAM_IDLE_TIMEOUT_MS = 90_000;

export const SESSION_DETAIL_STREAM_FALLBACK_LIMIT = 20;

export const SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS = 5000;

export const sessionDetailRequestInFlight = new Map<string, Promise<unknown>>();

export function sessionDetailSingleFlight<T>(key: string, load: () => Promise<T>): Promise<T> {
  const current = sessionDetailRequestInFlight.get(key) as Promise<T> | undefined;
  if (current) {
    return current;
  }
  const request = load().finally(() => {
    if (sessionDetailRequestInFlight.get(key) === request) {
      sessionDetailRequestInFlight.delete(key);
    }
  });
  sessionDetailRequestInFlight.set(key, request);
  return request;
}

export function retrieveSessionDetailSession(sessionId: string, workspaceId: string) {
  return sessionDetailSingleFlight(
    `session:${workspaceId}:${sessionId}`,
    () => retrieveManagedEntity('sessions', sessionId, workspaceId) as Promise<SessionApiResponse>,
  );
}

export function listSessionThreads(sessionId: string, workspaceId: string, page?: PageCursor, limit = 50) {
  return anthropicBetaApi.sessions.threads.list<SessionThreadApiResponse>(
    sessionId,
    page ? { limit, page } : { limit },
    workspaceId,
  ) as Promise<PageResponse<SessionThreadApiResponse>>;
}

export async function listAllSessionThreads(sessionId: string, workspaceId: string) {
  return sessionDetailSingleFlight(`threads:${workspaceId}:${sessionId}`, async () => {
    const data: SessionThreadApiResponse[] = [];
    let page: PageCursor = null;
    do {
      const response = await listSessionThreads(sessionId, workspaceId, page, SESSION_DETAIL_EVENT_PAGE_LIMIT);
      data.push(...(response.data ?? []));
      page = response.next_page ?? null;
    } while (page);
    return { data, next_page: null } satisfies PageResponse<SessionThreadApiResponse>;
  });
}

export function listSessionResourcesForDetail(sessionId: string, workspaceId: string) {
  return sessionDetailSingleFlight(`resources:${workspaceId}:${sessionId}`, () =>
    listSessionResources(sessionId, workspaceId),
  );
}

export function listSessionEvents(
  sessionId: string,
  workspaceId: string,
  order: 'asc' | 'desc' = 'desc',
  limit = 50,
  page?: PageCursor,
  signal?: AbortSignal,
) {
  return fetchSessionEventsPage({ sessionId, workspaceId, order, limit, page, signal });
}

export async function fetchSessionEventsPage({
  sessionId,
  threadId,
  workspaceId,
  order = 'asc',
  limit = SESSION_DETAIL_EVENT_PAGE_LIMIT,
  page,
  signal,
}: {
  sessionId: string;
  threadId?: string;
  workspaceId: string;
  order?: 'asc' | 'desc';
  limit?: number;
  page?: PageCursor;
  signal?: AbortSignal;
}): Promise<PageResponse<QuickstartSessionEvent>> {
  const params = new URLSearchParams({
    beta: 'true',
    limit: String(limit),
    order,
  });
  if (page) {
    params.set('page', page);
  }
  const headers = new Headers();
  if (workspaceId) {
    headers.set('X-Workspace-ID', workspaceId);
  }
  const path = threadId
    ? `/v1/sessions/${encodeURIComponent(sessionId)}/threads/${encodeURIComponent(threadId)}/events?${params.toString()}`
    : `/v1/sessions/${encodeURIComponent(sessionId)}/events?${params.toString()}`;
  const response = await fetch(path, {
    credentials: 'include',
    headers,
    signal,
  });
  if (!response.ok) {
    throw new Error(`Could not list session events (${response.status})`);
  }
  const payload = (await response.json()) as Partial<PageResponse<QuickstartSessionEvent>>;
  return {
    data: Array.isArray(payload.data)
      ? payload.data.map((event) => sessionEventWithResponseThread(event, threadId))
      : [],
    next_page: typeof payload.next_page === 'string' ? payload.next_page : null,
  };
}

export function sessionEventWithResponseThread(event: QuickstartSessionEvent, threadId?: string) {
  if (!threadId) {
    return event;
  }
  const hasOwner =
    (typeof event.session_thread_id === 'string' && event.session_thread_id.trim()) ||
    (typeof event.thread_id === 'string' && event.thread_id.trim());
  return hasOwner ? event : { ...event, session_thread_id: threadId };
}

export function sessionThreadShouldFetchEvents(thread: SessionThreadApiResponse, includeArchived = false) {
  return sessionThreadIsChild(thread) && (includeArchived || !sessionThreadIsArchived(thread));
}

export function sessionThreadIsChild(thread: SessionThreadApiResponse) {
  return typeof thread.parent_thread_id === 'string' && thread.parent_thread_id.trim().length > 0;
}

export function sessionThreadIsArchived(thread: SessionThreadApiResponse) {
  return typeof thread.archived_at === 'string' && thread.archived_at.trim().length > 0;
}

export function sessionThreadListSignature(threads: SessionThreadApiResponse[]) {
  return threads
    .map((thread) => `${thread.id}:${thread.type}:${thread.archived_at ?? ''}:${thread.parent_thread_id ?? ''}`)
    .join('|');
}

export function createQuickstartEnvironment(input: QuickstartCreateEnvironmentInput, workspaceId: string) {
  const reuseEnvironmentId = typeof input.reuse_environment_id === 'string' ? input.reuse_environment_id.trim() : '';
  if (reuseEnvironmentId) {
    return retrieveManagedEntity('environments', reuseEnvironmentId, workspaceId) as Promise<EnvironmentApiResponse>;
  }
  return anthropicBetaApi.environments.create<EnvironmentApiResponse>(
    quickstartEnvironmentRequestBody(input),
    workspaceId,
  );
}

export function quickstartEnvironmentRequestBody(input: QuickstartCreateEnvironmentInput | Record<string, unknown>) {
  const name = typeof input.name === 'string' && input.name.trim() ? input.name.trim() : 'Quickstart environment';
  const body: Record<string, unknown> = {
    name,
    metadata: {},
    scope: 'organization',
    config: quickstartEnvironmentConfig(input.config),
  };
  if (typeof input.description === 'string' && input.description.trim()) {
    body.description = input.description.trim();
  }
  return body;
}

export function quickstartEnvironmentConfig(configValue: unknown) {
  const config = objectRecord(configValue);
  if (config.type === 'self_hosted') {
    return { type: 'self_hosted' };
  }
  return {
    type: 'cloud',
    packages: quickstartEnvironmentPackages(config.packages),
    networking: quickstartEnvironmentNetworking(config.networking),
  };
}

export function quickstartEnvironmentPackages(packagesValue: unknown) {
  const packages = objectRecord(packagesValue);
  return {
    pip: quickstartPackageList(packages.pip),
    npm: quickstartPackageList(packages.npm),
    apt: quickstartPackageList(packages.apt),
    cargo: quickstartPackageList(packages.cargo),
    gem: quickstartPackageList(packages.gem),
    go: quickstartPackageList(packages.go),
  };
}

export function quickstartPackageList(value: unknown) {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

export function quickstartEnvironmentNetworking(networkingValue: unknown) {
  const networking = objectRecord(networkingValue);
  if (networking.type === 'limited') {
    const allowedHosts = Array.isArray(networking.allowed_hosts)
      ? networking.allowed_hosts.filter((host): host is string => typeof host === 'string')
      : [];
    return {
      type: 'limited',
      allow_mcp_servers: networking.allow_mcp_servers === true,
      allow_package_managers: networking.allow_package_managers === true,
      allowed_hosts: allowedHosts,
    };
  }
  return { type: 'unrestricted' };
}

export function createQuickstartVault(input: Record<string, unknown>, workspaceId: string) {
  const displayName =
    typeof input.display_name === 'string' && input.display_name.trim()
      ? input.display_name.trim()
      : typeof input.name === 'string' && input.name.trim()
        ? input.name.trim()
        : 'Quickstart vault';
  return anthropicBetaApi.vaults.create<VaultApiResponse>({ display_name: displayName, metadata: {} }, workspaceId);
}

export function createQuickstartVaultCredential(vaultId: string, input: Record<string, unknown>, workspaceId: string) {
  const displayName =
    typeof input.display_name === 'string' && input.display_name.trim()
      ? input.display_name.trim()
      : typeof input.name === 'string' && input.name.trim()
        ? input.name.trim()
        : 'Quickstart credential';
  const auth = input.auth && typeof input.auth === 'object' && !Array.isArray(input.auth) ? input.auth : null;
  if (!auth) {
    throw new Error('Credential auth is required before a vault credential can be created.');
  }
  return anthropicBetaApi.vaults.credentials.create<VaultCredentialApiResponse>(
    vaultId,
    { display_name: displayName, auth, metadata: {} },
    workspaceId,
  );
}

export function createQuickstartSession(
  agent: AgentApiResponse,
  environmentId: string,
  vaultIds: string[],
  workspaceId: string,
) {
  return anthropicBetaApi.sessions.create<SessionApiResponse>(
    {
      title: null,
      agent: { type: 'agent', id: agent.id, version: agent.version },
      environment_id: environmentId,
      vault_ids: vaultIds,
      metadata: {},
      resources: [],
    },
    workspaceId,
  );
}

export function postQuickstartSessionMessage(sessionId: string, message: string, workspaceId: string) {
  return anthropicBetaApi.sessions.events.send<unknown>(
    sessionId,
    {
      events: [{ type: 'user.message', content: [{ type: 'text', text: message }] }],
    },
    workspaceId,
  );
}

export function interruptQuickstartSession(sessionId: string, workspaceId: string) {
  return anthropicBetaApi.sessions.events.send<unknown>(
    sessionId,
    {
      events: [{ type: 'user.interrupt' }],
    },
    workspaceId,
  );
}

export async function streamQuickstartSessionEvents({
  sessionId,
  workspaceId,
  signal,
  onEvent,
}: {
  sessionId: string;
  workspaceId: string;
  signal: AbortSignal;
  onEvent: (event: QuickstartSessionEvent) => void;
}) {
  const headers = new Headers({ Accept: 'text/event-stream' });
  if (workspaceId) {
    headers.set('X-Workspace-ID', workspaceId);
  }
  const response = await fetch(`/v1/sessions/${encodeURIComponent(sessionId)}/events/stream?beta=true`, {
    credentials: 'include',
    headers,
    signal,
  });

  if (!response.ok || !response.body) {
    return;
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    const parsed = consumeSseBuffer<QuickstartSessionEvent>(buffer);
    buffer = parsed.remaining;
    parsed.events.forEach((event) => onEvent(event.data));
  }
  buffer += decoder.decode();
  consumeSseBuffer<QuickstartSessionEvent>(`${buffer}\n\n`).events.forEach((event) => onEvent(event.data));
}

export async function streamSessionEvents({
  sessionId,
  threadId,
  workspaceId,
  signal,
  onOpen,
  onEvent,
}: {
  sessionId: string;
  threadId?: string;
  workspaceId: string;
  signal: AbortSignal;
  onOpen?: () => void;
  onEvent: (event: QuickstartSessionEvent) => void;
}) {
  const headers = new Headers({ Accept: 'text/event-stream' });
  if (workspaceId) {
    headers.set('X-Workspace-ID', workspaceId);
  }
  const params = new URLSearchParams({ beta: 'true' });
  params.append('event_deltas[]', 'agent.message');
  params.append('event_deltas[]', 'agent.thinking');
  const path = threadId
    ? `/v1/sessions/${encodeURIComponent(sessionId)}/threads/${encodeURIComponent(threadId)}/stream?${params.toString()}`
    : `/v1/sessions/${encodeURIComponent(sessionId)}/events/stream?${params.toString()}`;
  const streamSignal = sessionLinkedAbortSignal(signal, SESSION_DETAIL_STREAM_IDLE_TIMEOUT_MS);
  const response = await fetch(path, {
    credentials: 'include',
    headers,
    signal: streamSignal.signal,
  });
  if (!response.ok || !response.body) {
    streamSignal.dispose();
    throw new SessionStreamError(response.status);
  }
  onOpen?.();
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  try {
    streamSignal.touch();
    for (;;) {
      const { value, done } = await reader.read();
      if (done) {
        break;
      }
      streamSignal.touch();
      buffer += decoder.decode(value, { stream: true });
      const parsed = consumeSseBuffer<QuickstartSessionEvent>(buffer);
      buffer = parsed.remaining;
      parsed.events.forEach((event) => onEvent(sessionEventWithResponseThread(event.data, threadId)));
    }
    buffer += decoder.decode();
    consumeSseBuffer<QuickstartSessionEvent>(`${buffer}\n\n`).events.forEach((event) =>
      onEvent(sessionEventWithResponseThread(event.data, threadId)),
    );
  } finally {
    streamSignal.dispose();
  }
}

export class SessionStreamError extends Error {
  status: number;

  constructor(status: number) {
    super(`Session event stream failed (${status})`);
    this.status = status;
  }
}

export function sessionLinkedAbortSignal(parent: AbortSignal, idleTimeoutMs: number) {
  const controller = new AbortController();
  let timeout: number | null = null;
  const abort = () => controller.abort(parent.reason);
  const clear = () => {
    if (timeout !== null) {
      window.clearTimeout(timeout);
      timeout = null;
    }
  };
  const touch = () => {
    clear();
    timeout = window.setTimeout(() => controller.abort(new Error('Session event stream timed out')), idleTimeoutMs);
  };
  if (parent.aborted) {
    abort();
  } else {
    parent.addEventListener('abort', abort, { once: true });
  }
  return {
    signal: controller.signal,
    touch,
    dispose: () => {
      clear();
      parent.removeEventListener('abort', abort);
    },
  };
}

export function emptySessionDetailEventCache(): SessionDetailEventCache {
  return {
    events: [],
    syncedThrough: null,
    historyComplete: false,
    sawTerminated: false,
  };
}

export function sessionDetailEventCacheKey(workspaceId: string, sessionId: string, threadId = '') {
  return ['managed-agents', 'session-detail-events', workspaceId, sessionId, threadId] as const;
}

export function sessionDetailDeltaFramesKey(workspaceId: string, sessionId: string, threadId = '') {
  return ['managed-agents', 'session-detail-delta-frames', workspaceId, sessionId, threadId] as const;
}

export function mergeSessionEventCache(
  cache: SessionDetailEventCache | undefined,
  incoming: QuickstartSessionEvent[],
  patch: SessionEventCachePatch = {},
): SessionDetailEventCache {
  const current = cache ?? emptySessionDetailEventCache();
  const indexById = new Map<string, number>();
  current.events.forEach((event, index) => {
    const id = sessionStableEventId(event);
    if (id) {
      indexById.set(id, index);
    }
  });

  let nextEvents: QuickstartSessionEvent[] | null = null;
  let sawTerminated = current.sawTerminated || patch.sawTerminated === true;
  for (const event of incoming) {
    const id = sessionStableEventId(event);
    if (!id) {
      continue;
    }
    if (sessionEventType(event) === 'session.status_terminated') {
      sawTerminated = true;
    }
    const existingIndex = indexById.get(id);
    if (existingIndex === undefined) {
      nextEvents = nextEvents ?? [...current.events];
      indexById.set(id, nextEvents.length);
      nextEvents.push(event);
      continue;
    }
    const events = nextEvents ?? current.events;
    const existing = events[existingIndex];
    if (sessionIncomingEventShouldReplace(existing, event)) {
      nextEvents = nextEvents ?? [...current.events];
      nextEvents[existingIndex] = event;
    }
  }

  const syncedThrough = patch.syncedThrough !== undefined ? patch.syncedThrough : current.syncedThrough;
  const historyComplete = patch.historyComplete !== undefined ? patch.historyComplete : current.historyComplete;
  if (
    nextEvents === null &&
    syncedThrough === current.syncedThrough &&
    historyComplete === current.historyComplete &&
    sawTerminated === current.sawTerminated
  ) {
    return current;
  }
  return {
    events: nextEvents ?? current.events,
    syncedThrough,
    historyComplete,
    sawTerminated,
  };
}

export function sessionStableEventId(event: QuickstartSessionEvent) {
  return typeof event.id === 'string' && event.id ? event.id : null;
}

export function sessionIncomingEventShouldReplace(existing: QuickstartSessionEvent, incoming: QuickstartSessionEvent) {
  const existingProcessedAt = sessionNullableProcessedAt(existing);
  const incomingProcessedAt = sessionNullableProcessedAt(incoming);
  if (existingProcessedAt === null && incomingProcessedAt !== null) {
    return true;
  }
  if (incomingProcessedAt !== null && existingProcessedAt !== incomingProcessedAt) {
    return true;
  }
  return false;
}

export function sessionNullableProcessedAt(event: QuickstartSessionEvent) {
  return typeof event.processed_at === 'string' && event.processed_at ? event.processed_at : null;
}

export async function syncSessionEventHistory({
  queryClient,
  sessionId,
  workspaceId,
  threadId = '',
  signal,
  fromStart = false,
  force = false,
}: {
  queryClient: QueryClient;
  sessionId: string;
  workspaceId: string;
  threadId?: string;
  signal?: AbortSignal;
  fromStart?: boolean;
  force?: boolean;
}) {
  const cacheKey = sessionDetailEventCacheKey(workspaceId, sessionId, threadId);
  const current = queryClient.getQueryData<SessionDetailEventCache>(cacheKey);
  if (!fromStart && !force && current?.historyComplete) {
    return current;
  }
  const initialPage = fromStart ? null : (current?.syncedThrough ?? null);
  const requestKey = `events:${workspaceId}:${sessionId}:${threadId}:${fromStart ? 'start' : force ? 'force' : (initialPage ?? 'tail')}`;
  return sessionDetailSingleFlight(requestKey, async () => {
    if (signal?.aborted) {
      throw signal.reason;
    }
    if (fromStart) {
      queryClient.setQueryData(cacheKey, emptySessionDetailEventCache());
      queryClient.setQueryData(sessionDetailDeltaFramesKey(workspaceId, sessionId, threadId), {});
    }
    let page = initialPage;
    let sawTerminated = false;
    do {
      if (signal?.aborted) {
        throw signal.reason;
      }
      const response = await fetchSessionEventsPage({
        sessionId,
        threadId: threadId || undefined,
        workspaceId,
        order: 'asc',
        limit: SESSION_DETAIL_EVENT_PAGE_LIMIT,
        page,
      });
      const nextPage = response.next_page ?? null;
      sawTerminated =
        sawTerminated || response.data.some((event) => sessionEventType(event) === 'session.status_terminated');
      queryClient.setQueryData<SessionDetailEventCache>(cacheKey, (cache) =>
        mergeSessionEventCache(
          cache,
          response.data,
          nextPage
            ? { historyComplete: false, syncedThrough: nextPage, sawTerminated }
            : { historyComplete: true, sawTerminated },
        ),
      );
      page = nextPage;
    } while (page && !signal?.aborted);
    return queryClient.getQueryData<SessionDetailEventCache>(cacheKey) ?? emptySessionDetailEventCache();
  });
}

export function mergeSessionStreamFrame(
  queryClient: QueryClient,
  workspaceId: string,
  sessionId: string,
  threadId: string,
  event: QuickstartSessionEvent,
) {
  const eventType = sessionEventType(event);
  if (eventType === 'event_start' || eventType === 'event_delta') {
    mergeSessionDeltaFrame(queryClient, workspaceId, sessionId, threadId, event);
    return;
  }
  const cacheKey = sessionDetailEventCacheKey(workspaceId, sessionId, threadId);
  queryClient.setQueryData<SessionDetailEventCache>(cacheKey, (cache) => mergeSessionEventCache(cache, [event]));
}

export function sessionEventHistoryShouldSkipStream(events: QuickstartSessionEvent[], threadId: string) {
  const orderedEvents = [...events].sort(compareSessionEvents);
  for (let index = orderedEvents.length - 1; index >= 0; index -= 1) {
    const type = sessionEventType(orderedEvents[index]);
    if (threadId) {
      if (type === 'session.thread_status_idle' || type === 'session.thread_status_terminated') {
        return true;
      }
      if (type === 'session.thread_status_running' || type === 'session.thread_status_rescheduled') {
        return false;
      }
      continue;
    }
    if (type === 'session.status_idle' || type === 'session.status_terminated' || type === 'session.deleted') {
      return true;
    }
    if (type === 'session.status_running' || type === 'session.status_rescheduled') {
      return false;
    }
  }
  return false;
}

export function sessionPrimaryHistoryShouldSkipStream(
  queryClient: QueryClient,
  workspaceId: string,
  sessionId: string,
) {
  const primaryCache = queryClient.getQueryData<SessionDetailEventCache>(
    sessionDetailEventCacheKey(workspaceId, sessionId, ''),
  );
  return primaryCache ? sessionEventHistoryShouldSkipStream(primaryCache.events, '') : false;
}

export function mergeSessionDeltaFrame(
  queryClient: QueryClient,
  workspaceId: string,
  sessionId: string,
  threadId: string,
  event: QuickstartSessionEvent,
) {
  const deltaKey = sessionDetailDeltaFramesKey(workspaceId, sessionId, threadId);
  if (sessionEventType(event) === 'event_start') {
    const started = sessionStreamingMessageFromStart(event, threadId);
    const id = sessionStableEventId(started);
    if (!id) {
      return;
    }
    queryClient.setQueryData<SessionDetailDeltaFrames>(deltaKey, (cache) => ({
      ...(cache ?? {}),
      [id]: { message: started, frames: [event] },
    }));
    const cacheKey = sessionDetailEventCacheKey(workspaceId, sessionId, threadId);
    queryClient.setQueryData<SessionDetailEventCache>(cacheKey, (cache) =>
      mergeSessionEventCache(cache, [{ ...started, processed_at: null, is_streaming: true }]),
    );
    return;
  }

  const eventId = typeof event.event_id === 'string' ? event.event_id : '';
  if (!eventId) {
    return;
  }
  queryClient.setQueryData<SessionDetailDeltaFrames>(deltaKey, (cache) => {
    const current = cache?.[eventId];
    if (!current) {
      return cache ?? {};
    }
    return {
      ...(cache ?? {}),
      [eventId]: {
        message: sessionStreamingMessageFromDelta(current.message, event),
        frames: [...current.frames, event],
      },
    };
  });
}

export function sessionStreamingMessageFromStart(
  event: QuickstartSessionEvent,
  threadId: string,
): QuickstartSessionEvent {
  const started = toRecord(event.event) ?? {};
  const type = sessionEventType(started);
  const content = Array.isArray(started.content) ? started.content : [];
  return sessionEventWithResponseThread(
    {
      ...started,
      type: type === 'agent.thinking' ? 'agent.thinking' : 'agent.message',
      content,
    },
    threadId || undefined,
  );
}

export function sessionStreamingMessageFromDelta(
  message: QuickstartSessionEvent,
  event: QuickstartSessionEvent,
): QuickstartSessionEvent {
  const delta = toRecord(event.delta);
  const contentDelta = toRecord(delta?.content);
  if (!contentDelta) {
    return message;
  }
  const index = typeof delta?.index === 'number' ? delta.index : 0;
  const content = Array.isArray(message.content) ? [...message.content] : [];
  const currentBlock = toRecord(content[index]);
  if (!currentBlock) {
    content[index] = { ...contentDelta };
    return { ...message, content };
  }
  if (contentDelta.type === 'text' && currentBlock.type === 'text') {
    content[index] = { ...currentBlock, text: `${currentBlock.text ?? ''}${contentDelta.text ?? ''}` };
    return { ...message, content };
  }
  if (contentDelta.type === 'thinking' && currentBlock.type === 'thinking') {
    const nextThinking = `${currentBlock.thinking ?? currentBlock.text ?? ''}${contentDelta.thinking ?? contentDelta.text ?? ''}`;
    content[index] = { ...currentBlock, thinking: nextThinking };
    return { ...message, content };
  }
  return message;
}

export function cleanupIncompleteSessionStreamEvents(
  queryClient: QueryClient,
  workspaceId: string,
  sessionId: string,
  threadId = '',
) {
  const cacheKey = sessionDetailEventCacheKey(workspaceId, sessionId, threadId);
  queryClient.setQueryData<SessionDetailEventCache>(cacheKey, (cache) => {
    if (!cache) {
      return cache;
    }
    const events = cache.events.filter((event) => {
      const type = sessionEventType(event);
      return (type !== 'agent.message' && type !== 'agent.thinking') || sessionNullableProcessedAt(event) !== null;
    });
    return events.length === cache.events.length ? cache : { ...cache, events };
  });
}

export function sessionDetailScopeEvents(
  queryClient: QueryClient,
  workspaceId: string,
  sessionId: string,
  threadIds: string[],
) {
  const events = threadIds.flatMap((threadId) => {
    const cache = queryClient.getQueryData<SessionDetailEventCache>(
      sessionDetailEventCacheKey(workspaceId, sessionId, threadId),
    );
    return cache?.events ?? [];
  });
  return mergeSessionEventsById(events);
}

export function sessionDetailDeltaFrames(
  queryClient: QueryClient,
  workspaceId: string,
  sessionId: string,
  threadIds: string[],
) {
  const frames: SessionDetailDeltaFrames = {};
  threadIds.forEach((threadId) => {
    Object.assign(
      frames,
      queryClient.getQueryData<SessionDetailDeltaFrames>(
        sessionDetailDeltaFramesKey(workspaceId, sessionId, threadId),
      ) ?? {},
    );
  });
  return frames;
}

export function mergeSessionEventsById(events: QuickstartSessionEvent[]) {
  const cache = mergeSessionEventCache(undefined, events);
  return coalesceSessionCrossPostedToolEvents(cache.events).sort(compareSessionEvents);
}

export function coalesceSessionCrossPostedToolEvents(events: QuickstartSessionEvent[]) {
  const output: QuickstartSessionEvent[] = [];
  const indexByKey = new Map<string, number>();
  events.forEach((event) => {
    const canonicalKey = sessionCrossPostedToolEventKey(event);
    if (!canonicalKey) {
      output.push(event);
      return;
    }
    const existingIndex = indexByKey.get(canonicalKey);
    if (existingIndex === undefined) {
      indexByKey.set(canonicalKey, output.length);
      output.push(event);
      return;
    }
    output[existingIndex] = event;
  });
  return output;
}

export function sessionCrossPostedToolEventKey(event: QuickstartSessionEvent) {
  const type = sessionEventType(event);
  if (type !== 'agent.tool_use' && type !== 'agent.mcp_tool_use' && type !== 'agent.custom_tool_use') {
    return '';
  }
  const threadId = sessionEventOwnerThreadId(event);
  const toolUseId = sessionEventToolUseId(event);
  if (!threadId || !toolUseId) {
    return '';
  }
  return `${type}:${threadId}:${toolUseId}`;
}

export function sessionEventOwnerThreadId(event: QuickstartSessionEvent) {
  return sessionEventStringField(event, 'session_thread_id') || sessionEventStringField(event, 'thread_id');
}

export function sessionEventToolUseId(event: QuickstartSessionEvent) {
  return (
    sessionEventStringField(event, 'tool_use_id') ||
    sessionEventStringField(event, 'mcp_tool_use_id') ||
    sessionEventStringField(event, 'custom_tool_use_id') ||
    sessionEventStringField(event, 'id')
  );
}

export function sessionEventStringField(event: QuickstartSessionEvent, field: string) {
  const value = event[field];
  return typeof value === 'string' ? value.trim() : '';
}

export function sessionStreamShouldStop(error: unknown) {
  return error instanceof SessionStreamError && (error.status === 401 || error.status === 403 || error.status === 404);
}

export function sessionStreamBackoff(error: unknown, currentBackoff: number) {
  if (error instanceof SessionStreamError && error.status === 429) {
    return Math.max(currentBackoff, 10_000);
  }
  return Math.min(Math.max(currentBackoff * 2 || 1000, 1000), 10_000);
}

export function sleepWithAbort(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason);
      return;
    }
    let timer: number;
    const abort = () => {
      window.clearTimeout(timer);
      reject(signal.reason);
    };
    timer = window.setTimeout(() => {
      signal.removeEventListener('abort', abort);
      resolve();
    }, ms);
    signal.addEventListener('abort', abort, { once: true });
  });
}

export function createQuickstartDeployment(
  agent: AgentApiResponse,
  environmentId: string,
  vaultIds: string[],
  input: QuickstartDeploymentInput,
  workspaceId: string,
) {
  const timezone =
    typeof input.timezone === 'string' && input.timezone.trim()
      ? input.timezone.trim()
      : Intl.DateTimeFormat().resolvedOptions().timeZone;
  return anthropicBetaApi.deployments.create<DeploymentApiResponse>(
    {
      name: input.name?.trim() || 'Quickstart deployment',
      agent: { type: 'agent', id: agent.id, version: agent.version },
      environment_id: environmentId,
      ...(vaultIds.length ? { vault_ids: vaultIds } : {}),
      initial_events: [
        {
          type: 'user.message',
          content: [{ type: 'text', text: input.initial_message?.trim() || 'Run the scheduled quickstart task.' }],
        },
      ],
      schedule: {
        type: 'cron',
        expression: input.cron_expression?.trim() || '0 9 * * 1',
        timezone,
      },
    },
    workspaceId,
  );
}

export async function postQuickstartProxyStream({
  orgUuid,
  workspaceId,
  body,
  signal,
  onEvent,
}: {
  orgUuid: string;
  workspaceId: string;
  body: unknown;
  signal: AbortSignal;
  onEvent: (event: QuickstartStreamEvent) => void;
}) {
  const headers = new Headers({
    Accept: 'text/event-stream',
    'Content-Type': 'application/json',
    'X-Organization-UUID': orgUuid,
  });
  if (workspaceId) {
    headers.set('X-Workspace-ID', workspaceId);
  }
  await postJsonSseStream<Record<string, unknown>>({
    url: `/api/organizations/${encodeURIComponent(orgUuid)}/proxy/v1/messages`,
    headers,
    body,
    signal,
    onEvent,
    errorFromResponse: quickstartStreamError,
  });
}

export async function quickstartStreamError(response: Response) {
  try {
    const payload = (await response.json()) as Record<string, unknown>;
    const error = payload.error;
    if (error && typeof error === 'object' && typeof (error as Record<string, unknown>).message === 'string') {
      return new Error(String((error as Record<string, unknown>).message));
    }
    if (typeof payload.message === 'string') {
      return new Error(payload.message);
    }
  } catch {
    // Fall through to status text.
  }
  return new Error(response.statusText || `Request failed with status ${response.status}`);
}

export function listVaultCredentials(vaultId: string, workspaceId: string) {
  return anthropicBetaApi.vaults.credentials.list<VaultCredentialApiResponse>(
    vaultId,
    { limit: 50, include_archived: false },
    workspaceId,
  ) as Promise<PageResponse<VaultCredentialApiResponse>>;
}

export function createVaultCredential(vaultId: string, values: CredentialFormValues, workspaceId: string) {
  return anthropicBetaApi.vaults.credentials.create<VaultCredentialApiResponse>(
    vaultId,
    { display_name: values.displayName.trim(), auth: credentialAuthBody(values, true), metadata: {} },
    workspaceId,
  );
}

export function updateVaultCredential(
  vaultId: string,
  credentialId: string,
  values: CredentialFormValues,
  workspaceId: string,
) {
  return anthropicBetaApi.vaults.credentials.update<VaultCredentialApiResponse>(
    vaultId,
    credentialId,
    { display_name: values.displayName.trim(), auth: credentialAuthBody(values, false), metadata: {} },
    workspaceId,
  );
}

export function archiveVaultCredential(vaultId: string, credentialId: string, workspaceId: string) {
  return anthropicBetaApi.vaults.credentials.archive<VaultCredentialApiResponse>(vaultId, credentialId, workspaceId);
}

export function deleteVaultCredential(vaultId: string, credentialId: string, workspaceId: string) {
  return anthropicBetaApi.vaults.credentials.delete<unknown>(vaultId, credentialId, workspaceId);
}

export function listMemories(memoryStoreId: string, workspaceId: string, pathPrefix = '/') {
  const query = {
    path_prefix: normalizeMemoryFolderPath(pathPrefix),
    depth: '1',
    limit: 100,
    order_by: 'path',
  };
  return anthropicBetaApi.memoryStores.memories.list<MemoryApiResponse>(memoryStoreId, query, workspaceId) as Promise<
    PageResponse<MemoryApiResponse>
  >;
}

export function retrieveMemory(memoryStoreId: string, memoryId: string, workspaceId: string) {
  return anthropicBetaApi.memoryStores.memories.retrieve<MemoryApiResponse>(
    memoryStoreId,
    memoryId,
    { view: 'full' },
    workspaceId,
  );
}

export function createMemory(memoryStoreId: string, values: MemoryFormValues, workspaceId: string) {
  return anthropicBetaApi.memoryStores.memories.create<MemoryApiResponse>(
    memoryStoreId,
    { path: values.path.trim(), content: values.content, view: 'full' },
    workspaceId,
  );
}

export function updateMemory(
  memoryStoreId: string,
  memoryId: string,
  values: MemoryFormValues,
  workspaceId: string,
  expectedContentSha256?: string | null,
) {
  const body: Record<string, unknown> = { path: values.path.trim(), content: values.content };
  if (isContentSha256(expectedContentSha256)) {
    body.precondition = { type: 'content_sha256', content_sha256: expectedContentSha256 };
  }
  return anthropicBetaApi.memoryStores.memories.update<MemoryApiResponse>(
    memoryStoreId,
    memoryId,
    { ...body, view: 'full' },
    workspaceId,
  );
}

export function deleteMemory(memoryStoreId: string, memoryId: string, workspaceId: string) {
  return anthropicBetaApi.memoryStores.memories.delete<unknown>(memoryStoreId, memoryId, workspaceId);
}

export function createManagedEntityBody(section: ManagedEntitySection, values: ManagedEntityFormValues) {
  const name = values.name.trim();
  const description = values.description.trim();
  switch (section) {
    case 'sessions':
      return {
        title: name || null,
        agent: values.agentId,
        environment_id: values.environmentId,
        vault_ids: values.vaultIds,
        metadata: {},
        resources: [],
      };
    case 'deployments':
      return {
        name,
        description: description || null,
        agent: values.agentId,
        environment_id: values.environmentId,
        vault_ids: values.vaultIds,
        metadata: {},
        resources: deploymentResources(values.memoryStoreIds),
        initial_events: deploymentInitialEvents(values.initialMessage),
        schedule: deploymentSchedule(values),
      };
    case 'environments':
      return {
        name,
        description,
        scope: 'organization',
        metadata: {},
        config: {
          type: 'cloud',
          packages: { type: 'packages', apt: [], cargo: [], gem: [], go: [], npm: [], pip: [] },
          networking: { type: 'unrestricted' },
        },
      };
    case 'credential-vaults':
      return {
        display_name: name,
        metadata: {},
      };
    case 'memory-stores':
      return {
        name,
        description,
        metadata: {},
      };
  }
}

export function updateManagedEntityBody(section: ManagedEntitySection, values: ManagedEntityFormValues) {
  const name = values.name.trim();
  const description = values.description.trim();
  switch (section) {
    case 'sessions':
      return {
        title: name || null,
        agent: values.agentId || undefined,
        environment_id: values.environmentId || undefined,
        vault_ids: values.vaultIds,
      };
    case 'deployments':
      return {
        name,
        description: description || null,
        agent: values.agentId || undefined,
        environment_id: values.environmentId || undefined,
        vault_ids: values.vaultIds,
        resources: deploymentResources(values.memoryStoreIds),
        initial_events: deploymentInitialEvents(values.initialMessage),
        schedule: deploymentSchedule(values),
      };
    case 'environments':
      return { name, description };
    case 'credential-vaults':
      return { display_name: name };
    case 'memory-stores':
      return { name, description };
  }
}

export function deploymentInitialEvents(initialMessage: string) {
  return [
    {
      type: 'user.message',
      content: [{ type: 'text', text: initialMessage.trim() }],
    },
  ];
}

export function deploymentResources(memoryStoreIds: string[]) {
  return memoryStoreIds.map((memoryStoreId) => ({
    type: 'memory_store',
    memory_store_id: memoryStoreId,
  }));
}

export function deploymentSchedule(values: ManagedEntityFormValues) {
  if (values.triggerType !== 'schedule') {
    return null;
  }
  return {
    type: 'cron',
    expression: values.cronExpression.trim(),
    timezone: values.timezone.trim() || localTimezone(),
  };
}

export function localTimezone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}
