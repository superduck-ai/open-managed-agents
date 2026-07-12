import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import type { AgentApiResponse } from '../../types';
import { resetTestDom } from '../../../../test/setup';

const testingLibrary = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { AgentToolsSection } = await import('./AgentToolsSection');
const { resetMcpDirectoryCacheForTests } = await import('./api');

const { act, cleanup, fireEvent, render, screen, waitFor } = testingLibrary;
const originalFetch = globalThis.fetch;

beforeEach(() => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_tools_test');
  resetMcpDirectoryCacheForTests();
});

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  resetMcpDirectoryCacheForTests();
});

describe('AgentToolsSection manual MCP refresh', () => {
  test('writes a delayed refresh response only to the version scope that started it', async () => {
    let finishRefresh!: () => void;
    let markRefreshStarted!: () => void;
    const refreshWait = new Promise<void>((resolve) => {
      finishRefresh = resolve;
    });
    const refreshStarted = new Promise<void>((resolve) => {
      markRefreshStarted = resolve;
    });

    globalThis.fetch = mock(async (input, init) => {
      const url = new URL(String(input), 'https://oma.duck.ai');
      const method = init?.method ?? 'GET';
      if (url.pathname === '/api/directory/servers') {
        return jsonResponse({ servers: [] });
      }
      if (url.pathname.endsWith('/mcp_tool_catalogs/refresh') && method === 'POST') {
        expect(url.searchParams.get('version')).toBe('1');
        markRefreshStarted();
        await refreshWait;
        return jsonResponse({
          data: { server_name: 'weather', status: 'ready', tools: [{ name: 'endpoint_a_new' }] },
          version: 1
        });
      }
      if (url.pathname.endsWith('/mcp_tool_catalogs') && method === 'GET') {
        const version = Number(url.searchParams.get('version'));
        return jsonResponse({
          data: [{
            server_name: 'weather',
            status: 'ready',
            tools: [{ name: version === 2 ? 'endpoint_b_tool' : 'endpoint_a_old' }]
          }],
          version
        });
      }
      throw new Error(`Unexpected request: ${method} ${url.pathname}`);
    });

    const queryClient = createQueryClient();
    const versionOne = agentFixture(1, [{ name: 'weather', url: 'https://endpoint-a.example/mcp' }]);
    const versionTwo = agentFixture(2, [{ name: 'weather', url: 'https://endpoint-b.example/mcp' }]);
    const rendered = render(sectionTree(queryClient, versionOne));

    const refreshButton = await screen.findByRole('button', { name: 'Refresh MCP tools for Weather' }) as HTMLButtonElement;
    await waitFor(() => expect(refreshButton.disabled).toBe(false));
    fireEvent.click(refreshButton);
    await refreshStarted;

    rendered.rerender(sectionTree(queryClient, versionTwo));
    const versionTwoKey = catalogKey(2);
    await waitFor(() => expect(catalogToolNames(queryClient, versionTwoKey)).toEqual(['endpoint_b_tool']));

    act(() => finishRefresh());
    const versionOneKey = catalogKey(1);
    await waitFor(() => expect(catalogToolNames(queryClient, versionOneKey)).toEqual(['endpoint_a_new']));
    expect(catalogToolNames(queryClient, versionTwoKey)).toEqual(['endpoint_b_tool']);
  });

  test('reloads the complete catalog collection after refreshing without initial GET data', async () => {
    let catalogGets = 0;
    globalThis.fetch = mock(async (input, init) => {
      const url = new URL(String(input), 'https://oma.duck.ai');
      const method = init?.method ?? 'GET';
      if (url.pathname === '/api/directory/servers') {
        return jsonResponse({ servers: [] });
      }
      if (url.pathname.endsWith('/mcp_tool_catalogs/refresh') && method === 'POST') {
        return jsonResponse({
          data: { server_name: 'weather', status: 'ready', tools: [{ name: 'new_weather' }] },
          version: 1
        });
      }
      if (url.pathname.endsWith('/mcp_tool_catalogs') && method === 'GET') {
        catalogGets += 1;
        if (catalogGets <= 2) {
          return jsonResponse({ error: { message: 'temporary catalog failure' } }, 503);
        }
        return jsonResponse({
          data: [
            { server_name: 'weather', status: 'ready', tools: [{ name: 'new_weather' }] },
            { server_name: 'maps', status: 'ready', tools: [{ name: 'saved_maps' }] }
          ],
          version: 1
        });
      }
      throw new Error(`Unexpected request: ${method} ${url.pathname}`);
    });

    const queryClient = createQueryClient();
    const agent = agentFixture(1, [
      { name: 'weather', url: 'https://weather.example/mcp' },
      { name: 'maps', url: 'https://maps.example/mcp' }
    ]);
    render(sectionTree(queryClient, agent));

    const refreshButton = await screen.findByRole('button', { name: 'Refresh MCP tools for Weather' }) as HTMLButtonElement;
    await waitFor(() => {
      expect(catalogGets).toBe(2);
      expect(refreshButton.disabled).toBe(false);
    });
    fireEvent.click(refreshButton);

    await waitFor(() => expect(catalogToolNames(queryClient, catalogKey(1))).toEqual(['new_weather', 'saved_maps']));
    expect(catalogGets).toBe(3);
  });

  test('announces Directory completion and gives each MCP refresh button a distinct name', async () => {
    let finishDirectory!: () => void;
    const directoryWait = new Promise<void>((resolve) => {
      finishDirectory = resolve;
    });
    globalThis.fetch = mock(async (input, init) => {
      const url = new URL(String(input), 'https://oma.duck.ai');
      const method = init?.method ?? 'GET';
      if (url.pathname === '/api/directory/servers') {
        await directoryWait;
        return jsonResponse({
          servers: [
            { type: 'remote', slug: 'weather', display_name: 'Weather Service', tool_names: ['forecast'] },
            { type: 'remote', slug: 'maps', display_name: 'Maps Service', tool_names: ['directions'] }
          ]
        });
      }
      if (url.pathname.endsWith('/mcp_tool_catalogs') && method === 'GET') {
        return jsonResponse({
          data: [
            { server_name: 'weather', status: 'unknown', tools: null },
            { server_name: 'maps', status: 'unknown', tools: null }
          ],
          version: 1
        });
      }
      throw new Error(`Unexpected request: ${method} ${url.pathname}`);
    });

    const queryClient = createQueryClient();
    const agent = agentFixture(1, [
      { name: 'weather', url: 'https://weather.example/mcp' },
      { name: 'maps', url: 'https://maps.example/mcp' }
    ]);
    render(sectionTree(queryClient, agent));

    const status = await screen.findByRole('status');
    await waitFor(() => {
      expect(status.textContent).toContain('Saved MCP tools loaded.');
      expect(status.textContent).toContain('Loading MCP tool metadata.');
    });

    act(() => finishDirectory());
    await waitFor(() => expect(status.textContent).toContain('MCP tool metadata loaded.'));
    expect(screen.getByRole('button', { name: 'Refresh MCP tools for Weather Service' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Refresh MCP tools for Maps Service' })).toBeTruthy();
  });
});

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, retryDelay: 0 },
      mutations: { retry: false }
    }
  });
}

function sectionTree(queryClient: InstanceType<typeof QueryClient>, agent: AgentApiResponse) {
  return (
    <QueryClientProvider client={queryClient}>
      <AgentToolsSection agent={agent} orgUuid="org_uuid" workspaceId="default" />
    </QueryClientProvider>
  );
}

function agentFixture(version: number, mcpServers: Array<{ name: string; url: string }>): AgentApiResponse {
  return {
    id: 'agent_tools_test',
    archived_at: null,
    created_at: '2026-07-11T00:00:00Z',
    description: null,
    mcp_servers: mcpServers,
    metadata: {},
    model: 'claude-sonnet-4-6',
    multiagent: null,
    name: 'Tools test agent',
    skills: [],
    system: null,
    tools: mcpServers.map((server) => ({
      type: 'mcp_toolset',
      mcp_server_name: server.name,
      default_config: { permission_policy: { type: 'always_ask' } }
    })),
    type: 'agent',
    updated_at: '2026-07-11T00:00:00Z',
    version
  };
}

function catalogKey(version: number) {
  return ['agent-mcp-tool-catalogs', 'org_uuid', 'default', 'agent_tools_test', version] as const;
}

function catalogToolNames(
  queryClient: InstanceType<typeof QueryClient>,
  key: ReturnType<typeof catalogKey>
) {
  const value = queryClient.getQueryData<{
    data: Array<{ tools: Array<{ name: string }> | null }>;
  }>(key);
  return value?.data.flatMap((catalog) => catalog.tools?.map((tool) => tool.name) ?? []);
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  });
}
