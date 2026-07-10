import { expect, mock } from 'bun:test';
import type { EditorView } from '@codemirror/view';
import { resetTestDom } from '../../test/setup';

export { resetTestDom };
export { mock };

const testingLibrary = await import('@testing-library/react');
export const { ManagedAgentsPage } = await import('./ManagedAgentsPage');
export const { WorkspaceContext } = await import('../../shared/workspaces/context');
const { defaultWorkspace } = await import('../../shared/workspaces/api');
const { setConsoleRequestContext } = await import('../../shared/api/client');
const { resetMcpDirectoryCacheForTests } = await import('./agents/tools/api');
const { I18nProvider } = await import('../../shared/i18n');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');

export const { act, cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;
const originalFetch = globalThis.fetch;

export function resetManagedAgentsTestState() {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  resetMcpDirectoryCacheForTests();
}

export function expectPageTextToContain(value: string) {
  expect(document.body.textContent?.replace(/ /g, ' ')).toContain(value);
}

export function codeBlockContaining(value: string) {
  return Array.from(document.querySelectorAll('pre')).find((element) => element.textContent?.includes(value));
}

export async function selectManagedComboboxOption(container: HTMLElement, name: string | RegExp, optionName: string | RegExp) {
  const trigger = within(container).getByRole('combobox', { name });
  fireEvent.pointerDown(trigger);
  fireEvent.pointerUp(trigger);
  fireEvent.click(trigger);

  const option = await screen.findByRole('option', { name: optionName });
  fireEvent.pointerDown(option);
  fireEvent.pointerUp(option);
  fireEvent.click(option);
}

export type CodeMirrorTestElement = HTMLElement & {
  __agentConfigCodeMirrorView?: EditorView;
};

export function setAgentConfigEditorValue(container: HTMLElement, value: string, name: string | RegExp) {
  const editor = within(container).getByRole('textbox', { name }) as CodeMirrorTestElement;
  const view = editor.__agentConfigCodeMirrorView ?? (editor.closest('.cm-editor') as CodeMirrorTestElement | null)?.__agentConfigCodeMirrorView;
  if (!view) {
    throw new Error('CodeMirror test view was not attached.');
  }
  act(() => {
    view.dispatch({
      changes: {
        from: 0,
        to: view.state.doc.length,
        insert: value
      }
    });
  });
}

export function renderManagedAgentsPage(section: Parameters<typeof ManagedAgentsPage>[0]['section'], locale: 'en' | 'zh-CN' = 'en') {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false
      }
    }
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLocale={locale}>
        <WorkspaceContext.Provider value={workspaceContextValue('default')}>
          <ManagedAgentsPage section={section} />
        </WorkspaceContext.Provider>
      </I18nProvider>
    </QueryClientProvider>
  );
}

export type AgentFixture = {
  id: string;
  name: string;
  archived_at?: string | null;
  description?: string | null;
  mcp_servers?: unknown[];
  metadata?: Record<string, unknown>;
  model?: string | { id?: string; speed?: string };
  multiagent?: unknown | null;
  skills?: unknown[];
  system?: string | null;
  tools?: Array<Record<string, unknown>>;
  version?: number;
  versions?: AgentFixture[];
  created_at?: string;
  updated_at?: string;
};

export type SessionFixture = {
  id: string;
  agentId: string;
  version: number;
  deploymentId?: string | null;
  inputTokens?: number;
  outputTokens?: number;
  title?: string | null;
  status?: string;
  archived_at?: string | null;
  created_at?: string;
  updated_at?: string;
};

export type DeploymentFixture = {
  id: string;
  agentId: string;
  version?: number;
  name: string;
  status?: string;
  archived_at?: string | null;
  created_at?: string;
  updated_at?: string;
};

export type SkillFixture = {
  id: string;
  displayTitle?: string;
  latestVersion?: string;
  source?: string;
  created_at?: string;
  updated_at?: string;
};

export type MockAgentsApiOptions = {
  sessions?: SessionFixture[];
  deployments?: DeploymentFixture[];
  skills?: SkillFixture[];
  mcpDirectoryServers?: Array<Record<string, unknown>>;
  mcpDirectoryErrorOnce?: boolean;
  analyticsOverview?: Record<string, unknown>;
  analyticsTimeseries?: Array<Record<string, unknown>>;
  quickstartStream?: string | ((body: Record<string, unknown>) => string);
  agentUpdateErrorStatus?: number;
  agentsListErrorOnce?: boolean;
  agentsSearchErrorOnce?: boolean;
  agentArchiveErrorOnce?: boolean;
  agentsSearchPageSize?: number;
};

export type RecordedRequest = {
  url: string;
  method: string;
  headers: Record<string, string>;
  body?: Record<string, unknown>;
};

export function mockAgentsApi(initialAgents: AgentFixture[], options: MockAgentsApiOptions = {}) {
  let agents = initialAgents.map(agentResponse);
  let agentsListErrorsRemaining = options.agentsListErrorOnce ? 1 : 0;
  let agentsSearchErrorsRemaining = options.agentsSearchErrorOnce ? 1 : 0;
  let agentArchiveErrorsRemaining = options.agentArchiveErrorOnce ? 1 : 0;
  let mcpDirectoryErrorsRemaining = options.mcpDirectoryErrorOnce ? 1 : 0;
  const now = new Date().toISOString();
  const skillDetails = new Map((options.skills ?? []).map((skill) => [skill.id, skillResponse(skill)]));
  let environments = [
    {
      id: 'env_option123456',
      archived_at: null,
      config: { type: 'cloud' },
      created_at: now,
      description: 'Option environment',
      name: 'Option environment',
      scope: 'workspace',
      state: 'active',
      type: 'environment',
      updated_at: now
    }
  ];
  let vaults = [
    {
      id: 'vault_option123456',
      archived_at: null,
      created_at: now,
      display_name: 'Option vault',
      type: 'vault',
      updated_at: now
    }
  ];
  const deployments: Record<string, unknown>[] = (options.deployments ?? []).map(deploymentResponse);
  const sessionEvents = new Map<string, Record<string, unknown>[]>();
  const versionsById = new Map<string, ReturnType<typeof agentResponse>[]>();
  initialAgents.forEach((fixture) => {
    const current = agentResponse(fixture);
    const historical = (fixture.versions ?? []).map((versionFixture) =>
      agentResponse({
        ...versionFixture,
        id: fixture.id
      })
    );
    versionsById.set(fixture.id, sortTestAgentVersions([current, ...historical]));
  });
  const requests: RecordedRequest[] = [];

  const fetchMock = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const headers = Object.fromEntries(new Headers(init?.headers).entries());
    const body = parseBody(init?.body);
    requests.push({ url, method, headers, body });

    if (url.startsWith('/api/directory/servers?') && method === 'GET') {
      if (mcpDirectoryErrorsRemaining > 0) {
        mcpDirectoryErrorsRemaining -= 1;
        return jsonResponse({ error: { message: 'MCP directory unavailable' } }, 503);
      }
      return jsonResponse({ servers: options.mcpDirectoryServers ?? [] });
    }

    if (url.startsWith('/v1/agents?') && method === 'GET') {
      if (agentsListErrorsRemaining > 0) {
        agentsListErrorsRemaining -= 1;
        return jsonResponse({ error: { message: 'agents list failed' } }, 500);
      }
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const includeArchived = params.get('include_archived') === 'true';
      const createdAtGTE = params.get('created_at[gte]');
      const filteredAgents = agents.filter((agent) => {
        if (!includeArchived && agent.archived_at) {
          return false;
        }
        if (createdAtGTE && new Date(agent.created_at).getTime() < new Date(createdAtGTE).getTime()) {
          return false;
        }
        return true;
      });
      const limit = Number(params.get('limit') ?? filteredAgents.length) || filteredAgents.length;
      const page = params.get('page');
      const offset = page === 'next_cursor' ? limit : 0;
      const data = filteredAgents.slice(offset, offset + limit);
      const nextPage = offset + limit < filteredAgents.length ? 'next_cursor' : null;
      return jsonResponse({ data, next_page: nextPage });
    }

    if (url.startsWith('/v1/agents:search?') && method === 'POST') {
      if (agentsSearchErrorsRemaining > 0) {
        agentsSearchErrorsRemaining -= 1;
        return jsonResponse({ error: { message: 'agents search failed' } }, 500);
      }
      const query = typeof body?.name === 'string' ? body.name.toLowerCase() : '';
      const includeArchived = body?.include_archived === true;
      const filteredAgents = agents.filter((agent) => {
        if (!includeArchived && agent.archived_at) {
          return false;
        }
        return agent.name.toLowerCase().includes(query);
      });
      const limit = Number(body?.limit ?? filteredAgents.length) || filteredAgents.length;
      const page = typeof body?.page === 'string' ? body.page : null;
      const parsedOffset = page?.startsWith('search_') ? Number(page.slice('search_'.length)) : NaN;
      const offset = Number.isFinite(parsedOffset) ? parsedOffset : page === 'next_cursor' ? limit : 0;
      const pageSize = Math.min(limit, options.agentsSearchPageSize ?? limit);
      const data = filteredAgents.slice(offset, offset + pageSize);
      const nextOffset = offset + pageSize;
      const nextPage = nextOffset < filteredAgents.length ? `search_${nextOffset}` : null;
      return jsonResponse({ data, next_page: nextPage });
    }

    const retrieveMatch = url.match(/^\/v1\/agents\/([^/]+)\?/);
    if (retrieveMatch && !url.includes('/versions?') && method === 'GET') {
      const agentId = decodeURIComponent(retrieveMatch[1]);
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const requestedVersion = Number(params.get('version'));
      const versionedAgents = versionsById.get(agentId) ?? [];
      const agent = Number.isFinite(requestedVersion) && requestedVersion > 0
        ? versionedAgents.find((candidate) => candidate.version === requestedVersion)
        : agents.find((candidate) => candidate.id === agentId);
      return agent ? jsonResponse(agent) : jsonResponse({ error: { message: 'not found' } }, 404);
    }

    const versionsMatch = url.match(/^\/v1\/agents\/([^/]+)\/versions\?/);
    if (versionsMatch && method === 'GET') {
      const agentId = decodeURIComponent(versionsMatch[1]);
      return jsonResponse({ data: versionsById.get(agentId) ?? [], next_page: null });
    }

    const skillRetrieveMatch = url.match(/^\/v1\/skills\/([^/]+)\?beta=true$/);
    if (skillRetrieveMatch && method === 'GET') {
      const skillId = decodeURIComponent(skillRetrieveMatch[1]);
      return jsonResponse(skillDetails.get(skillId) ?? skillResponse({ id: skillId }));
    }

    if (url.startsWith('/v1/sessions?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const agentId = params.get('agent_id');
      const requestedVersion = Number(params.get('agent_version'));
      const deploymentId = params.get('deployment_id');
      const includeArchived = params.get('include_archived') === 'true';
      const statuses = sessionStatusValuesFromSearchParams(params);
      const filteredSessions = (options.sessions ?? []).filter((session) => {
        if (agentId && session.agentId !== agentId) {
          return false;
        }
        if (Number.isFinite(requestedVersion) && requestedVersion > 0 && session.version !== requestedVersion) {
          return false;
        }
        if (deploymentId && session.deploymentId !== deploymentId) {
          return false;
        }
        if (statuses.length && !statuses.includes(session.status ?? 'idle')) {
          return false;
        }
        if (!includeArchived && session.archived_at) {
          return false;
        }
        return true;
      });
      const limit = Number(params.get('limit') ?? filteredSessions.length) || filteredSessions.length;
      const data = filteredSessions.slice(0, limit).map(sessionResponse);
      return jsonResponse({ data, next_page: filteredSessions.length > limit ? 'next_cursor' : null });
    }

    if (url.startsWith('/v1/environments?') && method === 'GET') {
      return jsonResponse({ data: environments, next_page: null });
    }

    if (url === '/v1/environments?beta=true' && method === 'POST') {
      const created = {
        id: 'env_created123456',
        archived_at: null,
        config: body?.config ?? { type: 'cloud' },
        created_at: new Date().toISOString(),
        description: typeof body?.description === 'string' ? body.description : null,
        metadata: body?.metadata ?? {},
        name: typeof body?.name === 'string' ? body.name : 'Created environment',
        scope: typeof body?.scope === 'string' ? body.scope : 'organization',
        state: 'active',
        type: 'environment',
        updated_at: new Date().toISOString()
      };
      environments = [created, ...environments];
      return jsonResponse(created);
    }

    if (url.startsWith('/v1/vaults?') && method === 'GET') {
      return jsonResponse({ data: vaults, next_page: null });
    }

    if (url === '/v1/vaults?beta=true' && method === 'POST') {
      const created = {
        id: 'vault_created123456',
        archived_at: null,
        created_at: new Date().toISOString(),
        display_name: typeof body?.display_name === 'string' ? body.display_name : 'Quickstart vault',
        metadata: body?.metadata ?? {},
        type: 'vault',
        updated_at: new Date().toISOString()
      };
      vaults = [created, ...vaults];
      return jsonResponse(created);
    }

    const credentialCreateMatch = url.match(/^\/v1\/vaults\/([^/]+)\/credentials\?beta=true$/);
    if (credentialCreateMatch && method === 'POST') {
      return jsonResponse({
        id: 'cred_created123456',
        archived_at: null,
        created_at: new Date().toISOString(),
        display_name: typeof body?.display_name === 'string' ? body.display_name : 'Quickstart credential',
        type: 'vault_credential',
        updated_at: new Date().toISOString(),
        vault_id: decodeURIComponent(credentialCreateMatch[1])
      });
    }

    const environmentRetrieveMatch = url.match(/^\/v1\/environments\/([^/]+)\?beta=true$/);
    if (environmentRetrieveMatch && method === 'GET') {
      const environmentId = decodeURIComponent(environmentRetrieveMatch[1]);
      const environment = environments.find((item) => item.id === environmentId);
      return environment ? jsonResponse(environment) : jsonResponse({ error: { message: 'not found' } }, 404);
    }

    if (url === '/v1/sessions?beta=true' && method === 'POST') {
      const createdAt = new Date().toISOString();
      const created = {
        id: 'sesn_created123456',
        agent: body?.agent ?? { type: 'agent', id: 'agent_created123456', version: 1 },
        archived_at: null,
        created_at: createdAt,
        deployment_id: null,
        environment_id: typeof body?.environment_id === 'string' ? body.environment_id : 'env_option123456',
        status: 'idle',
        title: null,
        type: 'session',
        updated_at: createdAt,
        vault_ids: []
      };
      sessionEvents.set(created.id, []);
      return jsonResponse(created);
    }

    if (url.startsWith('/v1/deployments?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const agentId = params.get('agent_id');
      const includeArchived = params.get('include_archived') === 'true';
      const status = params.get('status');
      const filteredDeployments = deployments.filter((deployment) => {
        if (agentId && deployment.agent_id !== agentId) {
          return false;
        }
        if (status && deployment.status !== status) {
          return false;
        }
        if (!includeArchived && deployment.archived_at) {
          return false;
        }
        return true;
      });
      const limit = Number(params.get('limit') ?? filteredDeployments.length) || filteredDeployments.length;
      return jsonResponse({ data: filteredDeployments.slice(0, limit), next_page: filteredDeployments.length > limit ? 'next_cursor' : null });
    }

    if (url === '/v1/deployments?beta=true' && method === 'POST') {
      const created = {
        id: 'dep_created123456',
        agent: body?.agent ?? { type: 'agent', id: 'agent_created123456', version: 1 },
        agent_id: objectIdFromRef(body?.agent) || 'agent_created123456',
        agent_version: objectVersionFromRef(body?.agent) || 1,
        archived_at: null,
        created_at: new Date().toISOString(),
        environment_id: typeof body?.environment_id === 'string' ? body.environment_id : 'env_option123456',
        name: typeof body?.name === 'string' ? body.name : 'Quickstart deployment',
        schedule: body?.schedule ?? null,
        status: 'active',
        type: 'deployment',
        updated_at: new Date().toISOString(),
        vault_ids: Array.isArray(body?.vault_ids) ? body.vault_ids : []
      };
      deployments.unshift(created);
      return jsonResponse(created);
    }

    if (url.startsWith('/v1/memory_stores?') && method === 'GET') {
      return jsonResponse({ data: [], next_page: null });
    }

    if (url.startsWith('/api/organizations/org_test/analytics/sessions/overview') && method === 'GET') {
      return jsonResponse(options.analyticsOverview ?? {
        sessions_count: 0,
        error_rate: 0,
        input_tokens: { total: 0, p50: 0, p95: 0 },
        output_tokens: { total: 0, p50: 0, p95: 0 },
        duration: { p50: 0, p95: 0 },
        active_time: { p50: 0, p95: 0 },
        input_tokens_per_session: { p50: 0, p95: 0 },
        output_tokens_per_session: { p50: 0, p95: 0 },
        turns_per_session: { p50: 0, p95: 0 },
        tool_call_counts: {},
        stop_reason_counts: {},
        data_as_of: null
      });
    }

    if (url.startsWith('/api/organizations/org_test/analytics/sessions/timeseries') && method === 'GET') {
      return jsonResponse({ data: options.analyticsTimeseries ?? [] });
    }

    const sessionEventsMatch = url.match(/^\/v1\/sessions\/([^/]+)\/events\?beta=true/);
    if (sessionEventsMatch && method === 'GET') {
      const sessionId = decodeURIComponent(sessionEventsMatch[1]);
      return jsonResponse({ data: sessionEvents.get(sessionId) ?? [], next_page: null });
    }

    if (sessionEventsMatch && method === 'POST') {
      const sessionId = decodeURIComponent(sessionEventsMatch[1]);
      const incoming = Array.isArray(body?.events) ? (body.events as Record<string, unknown>[]) : [];
      const existingEvents = sessionEvents.get(sessionId) ?? [];
      const lastEventAt = existingEvents.map((event) => Date.parse(String(event.created_at))).filter(Number.isFinite).at(-1) ?? Date.now();
      const now = Math.max(Date.now(), lastEventAt + 999);
      const created = incoming.map((event, index) => ({
        id: `evt_${sessionId}_${index + 1}`,
        created_at: new Date(now + index).toISOString(),
        ...event
      }));
      const transientEchoes = created.flatMap((event, index) => {
        if (event.type !== 'user.message') {
          return [];
        }
        return [{
          ...event,
          id: `evt_${sessionId}_echo_${String(event.id ?? index + 1)}`,
          created_at: new Date(now + index + 4000).toISOString()
        }];
      });
      const derived = created.flatMap((event, index) => {
        if (event.type !== 'user.message') {
          return [];
        }
        return [
          {
            id: `evt_${sessionId}_system_${index + 1}`,
            created_at: new Date(now + index + 5000).toISOString(),
            type: 'system.message',
            message: 'System message'
          },
          {
            id: `evt_${sessionId}_agent_thinking_${index + 1}`,
            created_at: new Date(now + index + 9000).toISOString(),
            type: 'agent.message',
            content: [{ type: 'thinking', thinking: 'Extract the requested schema fields and return JSON.' }]
          },
          {
            id: `evt_${sessionId}_agent_${index + 1}`,
            created_at: new Date(now + index + 10000).toISOString(),
            type: 'agent.message',
            content: [{
              type: 'text',
              text: '```json\n{\n  "order_id": "ORD-7742",\n  "carrier": "FedEx",\n  "ship_date": "2025-03-03",\n  "delivery_date": "2025-03-07",\n  "total_amount": 142.50,\n  "currency": "USD"\n}\n```'
            }]
          },
          {
            id: `evt_${sessionId}_idle_${index + 1}`,
            created_at: new Date(now + index + 10001).toISOString(),
            type: 'session.status_idle',
            content: [{
              type: 'text',
              text: '```json\n{\n  "order_id": "ORD-7742",\n  "carrier": "FedEx",\n  "ship_date": "2025-03-03",\n  "delivery_date": "2025-03-07",\n  "total_amount": 142.50,\n  "currency": "USD"\n}\n```'
            }]
          }
        ];
      });
      sessionEvents.set(sessionId, [...existingEvents, ...created, ...transientEchoes, ...derived]);
      return jsonResponse({ data: created });
    }

    const sessionEventsStreamMatch = url.match(/^\/v1\/sessions\/([^/]+)\/events\/stream\?/);
    if (sessionEventsStreamMatch && method === 'GET') {
      return streamResponse('');
    }

    const sessionThreadEventsStreamMatch = url.match(/^\/v1\/sessions\/([^/]+)\/threads\/([^/]+)\/stream\?/);
    if (sessionThreadEventsStreamMatch && method === 'GET') {
      return streamResponse('');
    }

    if (url === '/v1/agents?beta=true' && method === 'POST') {
      const name = typeof body?.name === 'string' ? body.name : 'Untitled agent';
      const created = agentResponse({
        id: 'agent_created123456',
        name,
        description: typeof body?.description === 'string' ? body.description : null,
        model: typeof body?.model === 'string' ? { id: body.model, speed: 'standard' } : { id: 'claude-sonnet-4-6' }
      });
      agents = [created, ...agents];
      versionsById.set(created.id, [created]);
      return jsonResponse(created);
    }

    if (url.match(/^\/api\/organizations\/[^/]+\/proxy\/v1\/messages$/) && method === 'POST') {
      const stream = typeof options.quickstartStream === 'function'
        ? options.quickstartStream(body ?? {})
        : options.quickstartStream ?? quickstartTextStream("Let's configure the environment.");
      return streamResponse(stream);
    }

    const updateMatch = url.match(/^\/v1\/agents\/([^/]+)\?beta=true$/);
    if (updateMatch && method === 'POST') {
      const agentId = decodeURIComponent(updateMatch[1]);
      const current = agents.find((agent) => agent.id === agentId);
      if (!current) {
        return jsonResponse({ error: { message: 'not found' } }, 404);
      }
      if (options.agentUpdateErrorStatus) {
        return jsonResponse({ error: { message: 'forced update failure' } }, options.agentUpdateErrorStatus);
      }
      if (body?.version !== current.version) {
        return jsonResponse({ error: { message: 'version conflict' } }, 409);
      }
      const model =
        typeof body.model === 'string'
          ? { id: body.model, speed: 'standard' }
          : body.model && typeof body.model === 'object' && !Array.isArray(body.model)
            ? (body.model as { id?: string; speed?: string })
            : current.model;
      const updated = agentResponse({
        id: agentId,
        name: typeof body.name === 'string' ? body.name : current.name,
        archived_at: current.archived_at,
        description: typeof body.description === 'string' || body.description === null ? (body.description as string | null) : current.description,
        mcp_servers: Array.isArray(body.mcp_servers) ? body.mcp_servers : current.mcp_servers,
        metadata: body.metadata && typeof body.metadata === 'object' && !Array.isArray(body.metadata) ? (body.metadata as Record<string, unknown>) : current.metadata,
        model,
        multiagent: body.multiagent === undefined ? current.multiagent : body.multiagent,
        skills: Array.isArray(body.skills) ? body.skills : current.skills,
        system: typeof body.system === 'string' || body.system === null ? (body.system as string | null) : current.system,
        tools: Array.isArray(body.tools) ? (body.tools as Array<Record<string, unknown>>) : current.tools,
        version: current.version + 1,
        created_at: current.created_at,
        updated_at: new Date().toISOString()
      });
      agents = agents.map((agent) => (agent.id === agentId ? updated : agent));
      versionsById.set(agentId, sortTestAgentVersions([updated, ...(versionsById.get(agentId) ?? [])]));
      return jsonResponse(updated);
    }

    const archiveMatch = url.match(/^\/v1\/agents\/([^/]+)\/archive\?beta=true$/);
    if (archiveMatch && method === 'POST') {
      if (agentArchiveErrorsRemaining > 0) {
        agentArchiveErrorsRemaining -= 1;
        return jsonResponse({ error: { message: 'agent archive failed' } }, 500);
      }
      const agentId = decodeURIComponent(archiveMatch[1]);
      const existing = agents.find((agent) => agent.id === agentId);
      agents = agents.filter((agent) => agent.id !== agentId);
      return jsonResponse({
        ...(existing ?? agentResponse({ id: agentId, name: 'Archived agent' })),
        archived_at: new Date().toISOString()
      });
    }

    return jsonResponse({ error: { message: 'not found' } }, 404);
  });

  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return { requests };
}

export function mockManagedResourceApi() {
  const now = new Date().toISOString();
  const requests: RecordedRequest[] = [];
  const resources = {
    agents: [
      agentResponse({
        id: 'agent_option123456',
        name: 'Option agent'
      })
    ],
    sessions: [
      {
        id: 'sesn_one123456',
        agent: { type: 'agent', id: 'agent_option123456', name: 'Ecommerce Basket Analysis Agent', version: 3 },
        archived_at: null,
        created_at: new Date(Date.now() - 90_000).toISOString(),
        environment_id: 'env_option123456',
        status: 'running',
        title: 'Session one',
        type: 'session',
        updated_at: now,
        vault_ids: ['vlt_one123456']
      }
    ],
    sessionResources: [
      {
        id: 'file_orders123456',
        type: 'file',
        created_at: new Date(Date.now() - 80_000).toISOString(),
        filename: 'orders.zip'
      }
    ],
    sessionThreads: [
      {
        id: 'sthr_reporter123456',
        type: 'session_thread',
        role: 'reporter',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: null,
        created_at: new Date(Date.now() - 45_000).toISOString(),
        updated_at: now
      },
      {
        id: 'sthr_analyst123456',
        type: 'session_thread',
        role: 'analyst',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: null,
        created_at: new Date(Date.now() - 45_000).toISOString(),
        updated_at: now
      },
      {
        id: 'sthr_forecaster123456',
        type: 'session_thread',
        role: 'forecaster',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: null,
        created_at: new Date(Date.now() - 45_000).toISOString(),
        updated_at: now
      },
      {
        id: 'sthr_archived123456',
        type: 'session_thread',
        role: 'archived',
        parent_thread_id: 'sthr_orchestrator123456',
        archived_at: new Date(Date.now() - 10_000).toISOString(),
        created_at: new Date(Date.now() - 44_000).toISOString(),
        updated_at: now
      }
    ],
    sessionEvents: [
      {
        id: 'evt_status_start',
        type: 'session.status_running',
        created_at: new Date(Date.now() - 88_000).toISOString()
      },
      {
        id: 'evt_user_queued',
        type: 'user.message',
        created_at: new Date(Date.now() - 84_000).toISOString(),
        is_queued: true,
        content: [{ type: 'text', text: 'Queued warmup request' }]
      },
        {
          id: 'evt_user_orders',
          type: 'user.message',
          created_at: new Date(Date.now() - 80_000).toISOString(),
          content: [{ type: 'text', text: 'Data Analysis Task - orders' }]
        },
        {
          id: 'evt_internal_system',
          type: 'system.message',
          created_at: new Date(Date.now() - 78_000).toISOString(),
          message: 'Starting Claude Code'
        },
        {
          id: 'evt_model_start',
          type: 'span.model_request_start',
          created_at: new Date(Date.now() - 77_000).toISOString()
        },
	        {
	          id: 'evt_agent_prepare',
	          type: 'agent.message',
	          created_at: new Date(Date.now() - 72_000).toISOString(),
	          content: [{ type: 'text', text: "I'll start by unzipping the dataset, then prepare the unified CSV files." }]
	      },
	      {
	        id: 'evt_tool_unzip',
	        type: 'agent.tool_use',
        created_at: new Date(Date.now() - 68_000).toISOString(),
        name: 'Bash',
        input: { command: 'unzip /mnt/session/uploads/orders.zip -d /workspace/data/' },
        usage: { input_tokens: 780, cache_read_input_tokens: 20, cache_creation_input_tokens: 5, output_tokens: 0 },
        duration_ms: 4000
      },
      {
        id: 'evt_tool_unzip_result',
        type: 'agent.tool_result',
        created_at: new Date(Date.now() - 67_000).toISOString(),
	        tool_use_id: 'evt_tool_unzip',
	        content: [{ type: 'text', text: 'Archive extracted.' }]
	      },
	      {
	        id: 'evt_model_end',
	        type: 'span.model_request_end',
	        model_request_start_id: 'evt_model_start',
	        created_at: new Date(Date.now() - 66_500).toISOString(),
	        model_usage: { input_tokens: 18200, output_tokens: 425 }
	      },
	      {
	        id: 'evt_tool_write_prepare',
	        type: 'agent.tool_use',
        created_at: new Date(Date.now() - 64_000).toISOString(),
        name: 'Write',
        input: { file_path: '/workspace/prepare.py', content: 'print("prepare")' },
        duration_ms: 3000
      },
      {
        id: 'evt_tool_run_prepare',
        type: 'agent.tool_use',
        created_at: new Date(Date.now() - 60_000).toISOString(),
        name: 'Bash',
        input: { command: 'cd /workspace && python3 prepare.py' },
        duration_ms: 18_000
      },
        {
          id: 'evt_agent_delegate',
          type: 'agent.message',
          created_at: new Date(Date.now() - 40_000).toISOString(),
          content: [{ type: 'text', text: "Data is ready. Now I'll delegate to all three agents in parallel." }],
          usage: { input_tokens: 6823, output_tokens: 68 },
          duration_ms: 2000
        },
        {
          id: 'evt_agent_tool_subagent',
          type: 'agent.tool_use',
          created_at: new Date(Date.now() - 39_900).toISOString(),
          name: 'Agent',
          input: {
            description: 'Ask reporter to summarize order cohorts.',
            prompt: 'Summarize order cohorts.',
            subagent_type: 'reporter'
          }
        },
        {
          id: 'evt_tool_batch_read',
          type: 'agent.tool_use',
          created_at: new Date(Date.now() - 39_800).toISOString(),
          name: 'Read',
        input: { file_path: '/workspace/data/orders.csv' },
        bracket_id: 'span_prepare_batch'
      },
      {
        id: 'evt_tool_batch_glob',
        type: 'agent.tool_use',
        created_at: new Date(Date.now() - 39_700).toISOString(),
        name: 'Glob',
        input: { pattern: '*.csv' },
        bracket_id: 'span_prepare_batch'
      },
      {
        id: 'evt_subagent_reporter',
        type: 'session.thread_created',
        session_thread_id: 'sthr_reporter123456',
        agent_name: 'reporter',
        created_at: new Date(Date.now() - 39_000).toISOString(),
        usage: { input_tokens: 7223, output_tokens: 168 },
        duration_ms: 28000
      },
      {
        id: 'evt_subagent_analyst',
        type: 'session.thread_created',
        session_thread_id: 'sthr_analyst123456',
        agent_name: 'analyst',
        created_at: new Date(Date.now() - 38_000).toISOString(),
        usage: { input_tokens: 7257, output_tokens: 247 },
        duration_ms: 31000
      },
      {
        id: 'evt_subagent_forecaster',
        type: 'session.thread_created',
        session_thread_id: 'sthr_forecaster123456',
        agent_name: 'forecaster',
        created_at: new Date(Date.now() - 37_000).toISOString(),
        usage: { input_tokens: 7317, output_tokens: 253 },
        duration_ms: 35000
      },
      {
        id: 'evt_thread_message_sent',
        type: 'agent.thread_message_sent',
        to_session_thread_id: 'sthr_reporter123456',
        to_agent_name: 'reporter',
        tool_use_id: 'tool_reporter123456',
        created_at: new Date(Date.now() - 36_500).toISOString(),
        content: [{ type: 'text', text: 'Summarize order cohorts.' }],
        usage: { input_tokens: 7223, output_tokens: 168 },
        duration_ms: 28000
      },
      {
        id: 'evt_thread_message_received',
        type: 'agent.thread_message_received',
        from_session_thread_id: 'sthr_reporter123456',
        from_agent_name: 'reporter',
        created_at: new Date(Date.now() - 36_000).toISOString(),
        tool_use_id: 'tool_reporter123456',
        content: [{ type: 'text', text: 'Reporter sent the first cohort summary.' }],
        raw_tool_result: {
          type: 'tool_result',
          tool_use_id: 'tool_reporter123456',
          content: [{ type: 'text', text: 'Reporter sent the first cohort summary.' }]
        },
        tool_use_result: {
          status: 'completed',
          content: [{ type: 'text', text: 'Reporter sent the first cohort summary.' }]
        }
      },
      {
        id: 'evt_orphan_tool_result_as_thread_message',
        type: 'agent.thread_message_received',
        from_session_thread_id: 'sthr_orphan_tool123456',
        tool_use_id: 'tool_orphan_bash123456',
        created_at: new Date(Date.now() - 35_500).toISOString(),
        content: [{ type: 'text', text: '(Bash completed with no output)' }],
        raw_tool_result: {
          type: 'tool_result',
          tool_use_id: 'tool_orphan_bash123456',
          content: '(Bash completed with no output)',
          is_error: false
        }
      },
      {
        id: 'evt_markdown_result',
        type: 'session.status_idle',
        created_at: new Date(Date.now() - 29_000).toISOString(),
        result:
          'Verification with `Glob("*")` shows only:\n\n- `.bash_logout`\n- `.bashrc`\n- `.profile`\n\n| Language | Translation | Directory listing |\n|---|---|---|\n| Chinese Simplified (zh-CN) | **你好，世界** | reported empty |\n| Japanese (ja) | こんにちは、世界 | inaccurate mixed list |\n\nNote: The translations look correct.'
      }
    ],
    sessionThreadEvents: {
      sthr_reporter123456: [
	      {
	        id: 'evt_reporter_span_start',
	        type: 'span.model_request_start',
	        session_thread_id: 'sthr_reporter123456',
	        created_at: new Date(Date.now() - 36_000).toISOString()
	      },
	      {
	        id: 'evt_reporter',
	        type: 'agent.message',
	        session_thread_id: 'sthr_reporter123456',
	        thread_name: 'reporter',
	        created_at: new Date(Date.now() - 35_000).toISOString(),
	        content: [{ type: 'text', text: 'Reporter is summarizing order cohorts.' }]
	      },
	      {
	        id: 'evt_reporter_content_tool',
	        type: 'agent.message',
	        session_thread_id: 'sthr_reporter123456',
	        thread_name: 'reporter',
	        created_at: new Date(Date.now() - 34_500).toISOString(),
	        content: [{ type: 'tool_use', id: 'tool_reporter_weather', name: 'mcp__weather_service__get_weather', input: { location: 'Beijing' } }]
	      },
	      {
	        id: 'evt_reporter_span',
	        type: 'span.model_request_end',
	        session_thread_id: 'sthr_reporter123456',
	        model_request_start_id: 'evt_reporter_span_start',
	        created_at: new Date(Date.now() - 34_000).toISOString(),
	        model_usage: { input_tokens: 7223, output_tokens: 168 }
	      }
      ],
      sthr_analyst123456: [
      {
        id: 'evt_analyst',
        type: 'agent.message',
        session_thread_id: 'sthr_analyst123456',
        thread_name: 'analyst',
        created_at: new Date(Date.now() - 32_000).toISOString(),
        content: [{ type: 'text', text: 'Analyst is calculating basket lift.' }],
        usage: { input_tokens: 7257, output_tokens: 247 },
        duration_ms: 31000
      },
      {
        id: 'evt_analyst_thinking',
        type: 'agent.thinking',
        session_thread_id: 'sthr_analyst123456',
        created_at: new Date(Date.now() - 31_000).toISOString(),
        content: [{ type: 'thinking', thinking: 'Reviewing basket pair frequencies.' }]
      },
      {
        id: 'evt_analyst_wrapped_thread_status',
        type: 'agent.tool_result',
        session_thread_id: 'sthr_analyst123456',
        created_at: new Date(Date.now() - 30_500).toISOString(),
        content: [
          {
            type: 'text',
            text: JSON.stringify({
              id: 'sevt_thread_running',
              type: 'session.thread_status_running',
              session_thread_id: 'sthr_analyst123456',
              agent_name: 'analyst',
              created_at: new Date(Date.now() - 30_500).toISOString()
            })
          }
        ]
      }
      ],
      sthr_forecaster123456: [
      {
        id: 'evt_forecaster',
        type: 'agent.message',
        session_thread_id: 'sthr_forecaster123456',
        thread_name: 'forecaster',
        created_at: new Date(Date.now() - 30_000).toISOString(),
        content: [{ type: 'text', text: 'Forecaster is projecting reorder risk.' }],
        usage: { input_tokens: 7317, output_tokens: 253 },
        duration_ms: 35000
      }
      ],
      sthr_archived123456: [
      {
        id: 'evt_archived',
        type: 'agent.message',
        session_thread_id: 'sthr_archived123456',
        thread_name: 'archived',
        created_at: new Date(Date.now() - 25_000).toISOString(),
        content: [{ type: 'text', text: 'Archived thread details.' }]
      }
      ]
    },
    deployments: [
      {
        id: 'dep_one123456',
        agent: 'agent_option123456',
        archived_at: null,
        created_at: now,
        description: 'A deployment',
        environment_id: 'env_option123456',
        name: 'Deployment one',
        paused_reason: null,
        schedule: null,
        status: 'active',
        type: 'deployment',
        updated_at: now,
        vault_ids: []
      }
    ],
    environments: [
      {
        id: 'env_one123456',
        archived_at: null,
        config: { type: 'cloud' },
        created_at: now,
        description: 'Primary environment',
        name: 'Environment one',
        scope: 'workspace',
        state: 'active',
        type: 'environment',
        updated_at: now
      },
      {
        id: 'env_option123456',
        archived_at: null,
        config: { type: 'cloud' },
        created_at: now,
        description: 'Option environment',
        name: 'Option environment',
        scope: 'workspace',
        state: 'active',
        type: 'environment',
        updated_at: now
      }
    ],
    vaults: [
      {
        id: 'vlt_one123456',
        archived_at: null,
        created_at: now,
        display_name: 'Vault one',
        type: 'vault',
        updated_at: now
      }
    ],
    vaultCredentials: {
      vlt_one123456: [
        {
          id: 'vcrd_one123456',
          archived_at: null,
          auth: { type: 'static_bearer' },
          created_at: now,
          display_name: 'Vault credential one',
          type: 'vault_credential',
          updated_at: now,
          vault_id: 'vlt_one123456'
        }
      ]
    },
    memoryStores: [
      {
        id: 'memstore_one123456',
        archived_at: null,
        created_at: now,
        description: 'A memory store',
        name: 'Memory one',
        type: 'memory_store',
        updated_at: now
      }
    ],
    memories: [
      {
        id: 'mem_one123456',
        content: 'Remember the release plan.',
        content_sha256: 'memory-hash-one',
        content_size_bytes: 26,
        created_at: now,
        memory_store_id: 'memstore_one123456',
        memory_version_id: 'memver_one123456',
        path: '/project/brief.md',
        type: 'memory',
        updated_at: now
      },
      {
        id: 'mem_nested123456',
        content: 'aaaa',
        content_sha256: 'memory-hash-nested',
        content_size_bytes: 4,
        created_at: now,
        memory_store_id: 'memstore_one123456',
        memory_version_id: 'memver_nested123456',
        path: '/aaa/bbbb',
        type: 'memory',
        updated_at: now
      },
      {
        id: 'mem_deep123456',
        content: 'xxxx',
        content_sha256: 'memory-hash-deep',
        content_size_bytes: 4,
        created_at: now,
        memory_store_id: 'memstore_one123456',
        memory_version_id: 'memver_deep123456',
        path: '/cccc/dddd/ccc/xxxx',
        type: 'memory',
        updated_at: now
      }
    ]
  };

  const fetchMock = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const headers = Object.fromEntries(new Headers(init?.headers).entries());
    const body = parseBody(init?.body);
    requests.push({ url, method, headers, body });

    if (url.startsWith('/v1/agents?') && method === 'GET') {
      return jsonResponse({ data: resources.agents, next_page: null });
    }
    if (url.startsWith('/v1/sessions?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const agentId = params.get('agent_id');
      const deploymentId = params.get('deployment_id');
      const includeArchived = params.get('include_archived') === 'true';
      const statuses = sessionStatusValuesFromSearchParams(params);
      const filteredSessions = resources.sessions.filter((session) => {
        const sessionAgentId = objectIdFromRef(session.agent);
        if (agentId && sessionAgentId !== agentId) {
          return false;
        }
        if (deploymentId && session.deployment_id !== deploymentId) {
          return false;
        }
        if (statuses.length && !statuses.includes(session.status ?? 'idle')) {
          return false;
        }
        if (!includeArchived && session.archived_at) {
          return false;
        }
        return matchesCreatedAtParams(session, params);
      });
      return jsonResponse({ data: filteredSessions, next_page: null });
    }
    const retrieveSessionMatch = url.match(/^\/v1\/sessions\/([^/?]+)\?beta=true$/);
    if (retrieveSessionMatch && method === 'GET') {
      const sessionId = decodeURIComponent(retrieveSessionMatch[1]);
      const session = resources.sessions.find((item) => item.id === sessionId);
      return session ? jsonResponse(session) : jsonResponse({ error: { message: 'not found' } }, 404);
    }
    const sessionResourcesMatch = url.match(/^\/v1\/sessions\/([^/?]+)\/resources\?/);
    if (sessionResourcesMatch && method === 'GET') {
      return jsonResponse({ data: resources.sessionResources, next_page: null });
    }
    const sessionThreadsMatch = url.match(/^\/v1\/sessions\/([^/?]+)\/threads\?/);
    if (sessionThreadsMatch && method === 'GET') {
      return jsonResponse({ data: resources.sessionThreads, next_page: null });
    }
    const sessionThreadEventsMatch = url.match(/^\/v1\/sessions\/([^/?]+)\/threads\/([^/?]+)\/events\?/);
    if (sessionThreadEventsMatch && method === 'GET') {
      const threadId = decodeURIComponent(sessionThreadEventsMatch[2]);
      return jsonResponse({
        data: persistedSessionEvents(resources.sessionThreadEvents[threadId as keyof typeof resources.sessionThreadEvents] ?? []),
        next_page: null
      });
    }
    const sessionEventsMatch = url.match(/^\/v1\/sessions\/([^/?]+)\/events\?/);
    if (sessionEventsMatch && method === 'GET') {
      return jsonResponse({ data: persistedSessionEvents(resources.sessionEvents), next_page: null });
    }
    if (url.startsWith('/v1/deployments?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const agentId = params.get('agent_id');
      const includeArchived = params.get('include_archived') === 'true';
      const status = params.get('status');
      const filteredDeployments = resources.deployments.filter((deployment) => {
        if (agentId && objectIdFromRef(deployment.agent) !== agentId && deployment.agent_id !== agentId) {
          return false;
        }
        if (status && deployment.status !== status) {
          return false;
        }
        if (!includeArchived && deployment.archived_at) {
          return false;
        }
        return matchesCreatedAtParams(deployment, params);
      });
      return jsonResponse({ data: filteredDeployments, next_page: null });
    }
    if (url.startsWith('/v1/environments?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const includeArchived = params.get('include_archived') === 'true';
      const filteredEnvironments = resources.environments.filter((environment) => {
        if (!includeArchived && environment.archived_at) {
          return false;
        }
        return matchesCreatedAtParams(environment, params);
      });
      return jsonResponse({ data: filteredEnvironments, next_page: null });
    }
    const retrieveEnvironmentMatch = url.match(/^\/v1\/environments\/([^/?]+)\?beta=true$/);
    if (retrieveEnvironmentMatch && method === 'GET') {
      const environmentId = decodeURIComponent(retrieveEnvironmentMatch[1]);
      const environment = resources.environments.find((item) => item.id === environmentId);
      return environment ? jsonResponse(environment) : jsonResponse({ error: { message: 'not found' } }, 404);
    }
    const environmentWorkMatch = url.match(/^\/v1\/environments\/([^/?]+)\/work\?/);
    if (environmentWorkMatch && method === 'GET') {
      return jsonResponse({ data: [], next_page: null });
    }
    if (url.startsWith('/v1/vaults?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const includeArchived = params.get('include_archived') === 'true';
      const filteredVaults = resources.vaults.filter((vault) => {
        if (!includeArchived && vault.archived_at) {
          return false;
        }
        return matchesCreatedAtParams(vault, params);
      });
      return jsonResponse({ data: filteredVaults, next_page: null });
    }
    const retrieveVaultMatch = url.match(/^\/v1\/vaults\/([^/?]+)\?beta=true$/);
    if (retrieveVaultMatch && method === 'GET') {
      const vaultId = decodeURIComponent(retrieveVaultMatch[1]);
      const vault = resources.vaults.find((item) => item.id === vaultId);
      return vault ? jsonResponse(vault) : jsonResponse({ error: { message: 'not found' } }, 404);
    }
    const listVaultCredentialsMatch = url.match(/^\/v1\/vaults\/([^/?]+)\/credentials\?beta=true&limit=50&include_archived=false$/);
    if (listVaultCredentialsMatch && method === 'GET') {
      const vaultId = decodeURIComponent(listVaultCredentialsMatch[1]);
      const credentials = (resources.vaultCredentials[vaultId] ?? []).filter((credential) => !credential.archived_at);
      return jsonResponse({ data: credentials, next_page: null });
    }
    const createVaultCredentialMatch = url.match(/^\/v1\/vaults\/([^/?]+)\/credentials\?beta=true$/);
    if (createVaultCredentialMatch && method === 'POST') {
      const vaultId = decodeURIComponent(createVaultCredentialMatch[1]);
      const created = {
        id: 'vcrd_created123456',
        archived_at: null,
        auth: body?.auth ?? { type: 'static_bearer' },
        created_at: new Date().toISOString(),
        display_name: typeof body?.display_name === 'string' ? body.display_name : 'Created credential',
        type: 'vault_credential' as const,
        updated_at: new Date().toISOString(),
        vault_id: vaultId
      };
      resources.vaultCredentials[vaultId] = [created, ...(resources.vaultCredentials[vaultId] ?? [])];
      return jsonResponse(created);
    }
    const updateVaultCredentialMatch = url.match(/^\/v1\/vaults\/([^/?]+)\/credentials\/([^/?]+)\?beta=true$/);
    if (updateVaultCredentialMatch && method === 'POST') {
      const vaultId = decodeURIComponent(updateVaultCredentialMatch[1]);
      const credentialId = decodeURIComponent(updateVaultCredentialMatch[2]);
      const existing = (resources.vaultCredentials[vaultId] ?? []).find((credential) => credential.id === credentialId) ?? resources.vaultCredentials[vaultId]?.[0];
      if (!existing) {
        return jsonResponse({ error: { message: 'not found' } }, 404);
      }
      const updated = {
        ...existing,
        auth: body?.auth ?? existing.auth,
        display_name: typeof body?.display_name === 'string' ? body.display_name : existing.display_name,
        updated_at: new Date().toISOString()
      };
      resources.vaultCredentials[vaultId] = [updated, ...(resources.vaultCredentials[vaultId] ?? []).filter((credential) => credential.id !== credentialId)];
      return jsonResponse(updated);
    }
    const archiveVaultCredentialMatch = url.match(/^\/v1\/vaults\/([^/?]+)\/credentials\/([^/?]+)\/archive\?beta=true$/);
    if (archiveVaultCredentialMatch && method === 'POST') {
      const vaultId = decodeURIComponent(archiveVaultCredentialMatch[1]);
      const credentialId = decodeURIComponent(archiveVaultCredentialMatch[2]);
      const existing = (resources.vaultCredentials[vaultId] ?? []).find((credential) => credential.id === credentialId);
      if (!existing) {
        return jsonResponse({ error: { message: 'not found' } }, 404);
      }
      const archived = { ...existing, archived_at: new Date().toISOString(), updated_at: new Date().toISOString() };
      resources.vaultCredentials[vaultId] = [archived, ...(resources.vaultCredentials[vaultId] ?? []).filter((credential) => credential.id !== credentialId)];
      return jsonResponse(archived);
    }
    const deleteVaultCredentialMatch = url.match(/^\/v1\/vaults\/([^/?]+)\/credentials\/([^/?]+)\?beta=true$/);
    if (deleteVaultCredentialMatch && method === 'DELETE') {
      const vaultId = decodeURIComponent(deleteVaultCredentialMatch[1]);
      const credentialId = decodeURIComponent(deleteVaultCredentialMatch[2]);
      resources.vaultCredentials[vaultId] = (resources.vaultCredentials[vaultId] ?? []).filter((credential) => credential.id !== credentialId);
      return jsonResponse({ id: credentialId, type: 'vault_credential_deleted' });
    }
    if (url.startsWith('/v1/memory_stores?') && method === 'GET') {
      const params = new URL(url, 'https://oma.duck.ai').searchParams;
      const includeArchived = params.get('include_archived') === 'true';
      const filteredMemoryStores = resources.memoryStores.filter((memoryStore) => {
        if (!includeArchived && memoryStore.archived_at) {
          return false;
        }
        return matchesCreatedAtParams(memoryStore, params);
      });
      return jsonResponse({ data: filteredMemoryStores, next_page: null });
    }
    if (url === '/v1/memory_stores/memstore_one123456?beta=true' && method === 'GET') {
      return jsonResponse(resources.memoryStores[0]);
    }
    const listMemoriesMatch = url.match(/^\/v1\/memory_stores\/memstore_one123456\/memories\?beta=true&path_prefix=([^&]+)&depth=1&limit=100&order_by=path$/);
    if (listMemoriesMatch && method === 'GET') {
      const page = memoryDepthPage(resources.memories, decodeURIComponent(listMemoriesMatch[1]));
      return jsonResponse({
        data: [...page.data, { ...resources.memories[0], id: 'mem_invalid123456', path: { nested: '/invalid.md' } as unknown as string }],
        prefixes: [...page.prefixes, { nested: '/invalid-prefix/' } as unknown as string],
        next_page: null
      });
    }
    const retrieveMemoryMatch = url.match(/^\/v1\/memory_stores\/memstore_one123456\/memories\/([^/]+)\?beta=true&view=full$/);
    if (retrieveMemoryMatch && method === 'GET') {
      const memoryId = decodeURIComponent(retrieveMemoryMatch[1]);
      return jsonResponse(resources.memories.find((memory) => memory.id === memoryId) ?? resources.memories[0]);
    }
    if (url === '/v1/memory_stores/memstore_one123456/memories?beta=true&view=full' && method === 'POST') {
      const content = typeof body?.content === 'string' ? body.content : '';
      const created = {
        ...resources.memories[0],
        id: 'mem_created123456',
        content,
        content_size_bytes: content.length,
        path: typeof body?.path === 'string' ? body.path : '/new-memory.md',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      resources.memories = [created, ...resources.memories];
      return jsonResponse(created);
    }
    const updateMemoryMatch = url.match(/^\/v1\/memory_stores\/memstore_one123456\/memories\/([^/]+)\?beta=true&view=full$/);
    if (updateMemoryMatch && method === 'POST') {
      const memoryId = decodeURIComponent(updateMemoryMatch[1]);
      const existing = resources.memories.find((memory) => memory.id === memoryId) ?? resources.memories[0];
      const updated = {
        ...existing,
        content: typeof body?.content === 'string' ? body.content : existing.content,
        path: typeof body?.path === 'string' ? body.path : existing.path,
        updated_at: new Date().toISOString()
      };
      resources.memories = [updated, ...resources.memories.filter((memory) => memory.id !== memoryId)];
      return jsonResponse(updated);
    }
    const deleteMemoryMatch = url.match(/^\/v1\/memory_stores\/memstore_one123456\/memories\/([^/]+)\?beta=true$/);
    if (deleteMemoryMatch && method === 'DELETE') {
      const memoryId = decodeURIComponent(deleteMemoryMatch[1]);
      resources.memories = resources.memories.filter((memory) => memory.id !== memoryId);
      return jsonResponse({ id: memoryId, type: 'memory_deleted' });
    }

    if (url === '/v1/sessions?beta=true' && method === 'POST') {
      const created = {
        ...resources.sessions[0],
        id: 'sesn_created123456',
        title: typeof body?.title === 'string' ? body.title : 'Created session',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      resources.sessions = [created, ...resources.sessions];
      return jsonResponse(created);
    }

    if (url === '/v1/deployments?beta=true' && method === 'POST') {
      const created = {
        ...resources.deployments[0],
        id: 'dep_created123456',
        name: typeof body?.name === 'string' ? body.name : 'Created deployment',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      resources.deployments = [created, ...resources.deployments];
      return jsonResponse(created);
    }

    if (url === '/v1/environments?beta=true' && method === 'POST') {
      const created = {
        ...resources.environments[0],
        id: 'env_created123456',
        name: typeof body?.name === 'string' ? body.name : 'Created environment',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString()
      };
      resources.environments = [created, ...resources.environments];
      return jsonResponse(created);
    }

    const deploymentAction = url.match(/^\/v1\/deployments\/([^/]+)\/(run|pause|unpause)\?beta=true$/);
    if (deploymentAction && method === 'POST') {
      const deploymentId = decodeURIComponent(deploymentAction[1]);
      const action = deploymentAction[2];
      const existing = resources.deployments.find((deployment) => deployment.id === deploymentId) ?? resources.deployments[0];
      if (action === 'pause') {
        existing.status = 'paused';
      }
      if (action === 'unpause') {
        existing.status = 'active';
      }
      return jsonResponse(action === 'run' ? { id: 'drun_created123456', type: 'deployment_run' } : existing);
    }

    const archiveMatch = url.match(/^\/v1\/(sessions|deployments|environments|vaults|memory_stores)\/([^/]+)\/archive\?beta=true$/);
    if (archiveMatch && method === 'POST') {
      const collection = collectionForTestEndpoint(resources, archiveMatch[1]);
      const resourceId = decodeURIComponent(archiveMatch[2]);
      const existing = collection.find((resource) => resource.id === resourceId) ?? collection[0];
      removeFromCollection(collection, resourceId);
      return jsonResponse({ ...existing, archived_at: new Date().toISOString() });
    }

    const deleteMatch = url.match(/^\/v1\/(sessions|environments|vaults|memory_stores)\/([^/]+)\?beta=true$/);
    if (deleteMatch && method === 'DELETE') {
      const collection = collectionForTestEndpoint(resources, deleteMatch[1]);
      const resourceId = decodeURIComponent(deleteMatch[2]);
      removeFromCollection(collection, resourceId);
      return jsonResponse({ id: resourceId, type: `${deleteMatch[1]}_deleted` });
    }

    return jsonResponse({ error: { message: 'not found' } }, 404);
  });

  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return { requests, resources };
}

export type TestResourcesShape = {
  sessions: Array<{ id: string }>;
  deployments: Array<{ id: string }>;
  environments: Array<{ id: string }>;
  vaults: Array<{ id: string }>;
  memoryStores: Array<{ id: string }>;
};

export function collectionForTestEndpoint(resources: TestResourcesShape, endpoint: string) {
  switch (endpoint) {
    case 'sessions':
      return resources.sessions;
    case 'deployments':
      return resources.deployments;
    case 'environments':
      return resources.environments;
    case 'vaults':
      return resources.vaults;
    case 'memory_stores':
      return resources.memoryStores;
    default:
      return resources.sessions;
  }
}

export function removeFromCollection<T extends { id: string }>(collection: T[], id: string) {
  const index = collection.findIndex((item) => item.id === id);
  if (index >= 0) {
    collection.splice(index, 1);
  }
}

export function agentResponse(agent: AgentFixture) {
  const now = new Date().toISOString();
  return {
    id: agent.id,
    archived_at: agent.archived_at ?? null,
    created_at: agent.created_at ?? now,
    description: agent.description ?? 'A test agent',
    mcp_servers: agent.mcp_servers ?? [],
    metadata: agent.metadata ?? {},
    model: agent.model ?? { id: 'claude-sonnet-4-6', speed: 'standard' },
    multiagent: agent.multiagent ?? null,
    name: agent.name,
    skills: agent.skills ?? [],
    system: agent.system ?? null,
    tools: agent.tools ?? [{ type: 'agent_toolset_20260401' }],
    type: 'agent',
    updated_at: agent.updated_at ?? now,
    version: agent.version ?? 1
  };
}

export function skillResponse(skill: SkillFixture) {
  const now = new Date().toISOString();
  return {
    id: skill.id,
    type: 'skill',
    display_title: skill.displayTitle ?? skill.id,
    latest_version: skill.latestVersion ?? '20260701',
    source: skill.source ?? 'custom',
    created_at: skill.created_at ?? now,
    updated_at: skill.updated_at ?? now
  };
}

export function sortTestAgentVersions(agents: ReturnType<typeof agentResponse>[]) {
  const byVersion = new Map<number, ReturnType<typeof agentResponse>>();
  agents.forEach((agent) => byVersion.set(agent.version, agent));
  return [...byVersion.values()].sort((left, right) => right.version - left.version);
}

export function sessionResponse(session: SessionFixture) {
  const now = new Date().toISOString();
  return {
    id: session.id,
    agent: { type: 'agent', id: session.agentId, version: session.version },
    archived_at: session.archived_at ?? null,
    created_at: session.created_at ?? now,
    deployment_id: session.deploymentId ?? null,
    environment_id: 'env_option123456',
    stats: {
      input_tokens: session.inputTokens ?? 0,
      output_tokens: session.outputTokens ?? 0
    },
    status: session.status ?? 'idle',
    title: session.title ?? null,
    type: 'session',
    updated_at: session.updated_at ?? now,
    vault_ids: []
  };
}

export function deploymentResponse(deployment: DeploymentFixture) {
  const now = new Date().toISOString();
  return {
    id: deployment.id,
    agent: { type: 'agent', id: deployment.agentId, version: deployment.version ?? 1 },
    agent_id: deployment.agentId,
    agent_version: deployment.version ?? 1,
    archived_at: deployment.archived_at ?? null,
    created_at: deployment.created_at ?? now,
    environment_id: 'env_option123456',
    name: deployment.name,
    schedule: null,
    status: deployment.status ?? 'active',
    type: 'deployment',
    updated_at: deployment.updated_at ?? now,
    vault_ids: []
  };
}

export function objectIdFromRef(value: unknown) {
  if (typeof value === 'string') {
    return value;
  }
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const id = (value as Record<string, unknown>).id;
    return typeof id === 'string' ? id : null;
  }
  return null;
}

export function objectVersionFromRef(value: unknown) {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const version = (value as Record<string, unknown>).version;
    return typeof version === 'number' ? version : null;
  }
  return null;
}

export function sessionStatusValuesFromUrl(url: string) {
  const params = new URL(url, 'https://oma.duck.ai').searchParams;
  return sessionStatusValuesFromSearchParams(params);
}

export function sessionStatusValuesFromSearchParams(params: URLSearchParams) {
  const rawValues = [
    ...params.getAll('statuses'),
    ...params.getAll('statuses[]')
  ];
  if (!rawValues.length) {
    const fallback = params.get('statuses');
    if (fallback) {
      rawValues.push(fallback);
    }
  }
  return rawValues
    .flatMap((value) => value.split(','))
    .map((status) => status.trim())
    .filter(Boolean)
    .sort();
}

export function matchesCreatedAtParams(resource: { created_at?: string | null }, params: URLSearchParams) {
  const gte = params.get('created_at[gte]');
  const lte = params.get('created_at[lte]');
  if (!gte && !lte) {
    return true;
  }
  if (!resource.created_at) {
    return false;
  }
  const createdAt = new Date(resource.created_at).getTime();
  if (Number.isNaN(createdAt)) {
    return false;
  }
  if (gte) {
    const gteTime = new Date(gte).getTime();
    if (!Number.isNaN(gteTime) && createdAt < gteTime) {
      return false;
    }
  }
  if (lte) {
    const lteTime = new Date(lte).getTime();
    if (!Number.isNaN(lteTime) && createdAt > lteTime) {
      return false;
    }
  }
  return true;
}

export function memoryDepthPage(memories: Array<{ path: string; content?: string | null; [key: string]: unknown }>, pathPrefix: string) {
  const normalizedPrefix = normalizeTestFolderPath(pathPrefix);
  const prefixes = new Map<string, { path: string; type: 'memory_prefix' }>();
  const directMemories: unknown[] = [];
  for (const memory of memories) {
    if (!memory.path.startsWith(normalizedPrefix)) {
      continue;
    }
    const remainder = memory.path.slice(normalizedPrefix.length);
    const segments = remainder.split('/').filter(Boolean);
    if (!segments.length) {
      continue;
    }
    if (segments.length > 1) {
      const prefix = `${normalizedPrefix}${segments[0]}/`;
      prefixes.set(prefix, { path: prefix, type: 'memory_prefix' });
      continue;
    }
    directMemories.push({ ...memory, content: null });
  }
  const data = [...prefixes.values(), ...directMemories].sort((left, right) => String((left as { path?: unknown }).path ?? '').localeCompare(String((right as { path?: unknown }).path ?? '')));
  return { data, prefixes: [...prefixes.values()] };
}

export function normalizeTestFolderPath(path: string) {
  const trimmed = path.trim() || '/';
  const prefixed = trimmed.startsWith('/') ? trimmed : `/${trimmed}`;
  return prefixed.endsWith('/') ? prefixed : `${prefixed}/`;
}

export function parseBody(body: BodyInit | null | undefined) {
  if (!body || typeof body !== 'string') {
    return undefined;
  }
  return JSON.parse(body) as Record<string, unknown>;
}

export function persistedSessionEvents<T extends Record<string, unknown>>(events: T[]) {
  return events.map((event) => ({
    ...event,
    processed_at: typeof event.processed_at === 'string' ? event.processed_at : event.created_at
  }));
}

export function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  });
}

export function streamResponse(body: string) {
  return new Response(body, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' }
  });
}

export function quickstartTextStream(text: string) {
  return [
    sseFrame('message_start', { type: 'message_start', message: { id: 'msg_test', type: 'message' } }),
    sseFrame('content_block_start', { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }),
    sseFrame('content_block_delta', { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text } }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 0 }),
    sseFrame('message_stop', { type: 'message_stop' })
  ].join('');
}

export function quickstartToolStream(name: string, input: Record<string, unknown>) {
  return [
    sseFrame('message_start', { type: 'message_start', message: { id: 'msg_tool', type: 'message' } }),
    sseFrame('content_block_start', {
      type: 'content_block_start',
      index: 0,
      content_block: { type: 'tool_use', id: `toolu_${name}`, name, input: {} }
    }),
    sseFrame('content_block_delta', {
      type: 'content_block_delta',
      index: 0,
      delta: { type: 'input_json_delta', partial_json: JSON.stringify(input) }
    }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 0 }),
    sseFrame('message_stop', { type: 'message_stop' })
  ].join('');
}

export function quickstartTextAndToolStream(text: string, name: string, input: Record<string, unknown>) {
  return [
    sseFrame('message_start', { type: 'message_start', message: { id: 'msg_text_tool', type: 'message' } }),
    sseFrame('content_block_start', { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }),
    sseFrame('content_block_delta', { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text } }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 0 }),
    sseFrame('content_block_start', {
      type: 'content_block_start',
      index: 1,
      content_block: { type: 'tool_use', id: `toolu_${name}`, name, input: {} }
    }),
    sseFrame('content_block_delta', {
      type: 'content_block_delta',
      index: 1,
      delta: { type: 'input_json_delta', partial_json: JSON.stringify(input) }
    }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 1 }),
    sseFrame('message_stop', { type: 'message_stop' })
  ].join('');
}

export function quickstartTextServerToolAndToolStream(
  text: string,
  serverToolQuery: string,
  name: string,
  input: Record<string, unknown>
) {
  const frames = [
    sseFrame('message_start', { type: 'message_start', message: { id: 'msg_text_server_tool', type: 'message' } }),
    sseFrame('content_block_start', { type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } }),
    sseFrame('content_block_delta', { type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text } }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 0 }),
    sseFrame('content_block_start', {
      type: 'content_block_start',
      index: 1,
      content_block: { type: 'server_tool_use', id: 'srvtoolu_web_search', name: 'web_search', input: {} }
    }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 1 }),
    sseFrame('content_block_start', {
      type: 'content_block_start',
      index: 2,
      content_block: { type: 'tool_use', id: `toolu_${name}`, name, input: {} }
    }),
    sseFrame('content_block_delta', {
      type: 'content_block_delta',
      index: 2,
      delta: { type: 'input_json_delta', partial_json: JSON.stringify(input) }
    }),
    sseFrame('content_block_stop', { type: 'content_block_stop', index: 2 }),
    sseFrame('message_stop', { type: 'message_stop' })
  ];
  if (serverToolQuery) {
    frames.splice(
      5,
      0,
      sseFrame('content_block_delta', {
        type: 'content_block_delta',
        index: 1,
        delta: { type: 'input_json_delta', partial_json: JSON.stringify({ query: serverToolQuery }) }
      })
    );
  }
  return frames.join('');
}

export function sseFrame(event: string, data: Record<string, unknown>) {
  return `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`;
}

export const serverAgent: AgentFixture = {
  id: 'agent_server123456',
  name: 'Server agent',
  model: { id: 'claude-sonnet-4-6', speed: 'standard' }
};

export function createAgentRequestFixture(name: string) {
  return {
    name,
    description: 'Temporary description',
    model: 'claude-sonnet-4-6',
    system: 'Temporary system prompt.',
    mcp_servers: [],
    tools: [{ type: 'agent_toolset_20260401' }],
    skills: [],
    metadata: { template: 'blank-agent' }
  };
}

export function workspaceContextValue(workspaceId: string) {
  const workspace = {
    ...defaultWorkspace,
    id: workspaceId,
    name: workspaceId === 'default' ? 'Default' : 'foo'
  };
  return {
    orgUuid: 'org_test',
    workspaces: [defaultWorkspace, workspace],
    activeWorkspace: workspace,
    activeWorkspaceId: workspaceId,
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => workspace,
    refreshWorkspaces: async () => undefined
  };
}
