import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRootRoute, createRouter, RouterProvider } from '@tanstack/react-router';
import { resetTestDom, setTestViewport } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { AuthContext } from '../../shared/auth/context';
import { WorkspaceContext } from '../../shared/workspaces/context';
import { defaultWorkspace, type Workspace } from '../../shared/workspaces/api';
import type { RuntimeCapabilities } from '../../shared/workspaces/runtime';
import { visibleConsoleNavigation } from '../../shared/navigation/navigation';
import { visibleSettingsGroups } from '../../shared/navigation/settings-navigation';
import { allowedResourceSources } from '../../features/console-search/resource-search';
import { navigationSearchRecords } from '../../features/console-search/search-data';
import { OverviewPage } from '../../features/dashboard/overview';
import { SessionInspector } from '../../features/managed-agents/sessions/SessionInspector';
import { SessionOutputFiles } from '../../features/managed-agents/sessions/SessionOutputFiles';
import { TooltipProvider } from '../../shared/ui/tooltip';
import { ConsoleShell } from './ConsoleLayout';
import { RuntimeCapabilityGuard } from './RuntimeCapabilityGuard';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

const externalCaps: RuntimeCapabilities = {
  mcp_tunnels: false,
  port_preview: false,
  otlp_traces: false,
  observability_dashboard: false,
  byok_llm_providers: false,
  deployments: false,
  session_webhooks: false,
  session_budget: 'credits',
  session_agent_overrides: false,
  channels: false,
  evaluations: false,
  skills: false,
  memory_stores: false,
  credential_vaults: false,
  mcp_servers: false,
  usage_metering: 'provider_credits',
  artifact_discovery: 'explicit_deliver_tool',
  file_constraints: {
    max_size_bytes: 5 * 1024 * 1024,
    allowed_mime_prefixes: ['text/', 'image/'],
    allow_binary: false,
  },
};

const externalWorkspace: Workspace = {
  ...defaultWorkspace,
  id: 'wrkspc_qoder',
  name: 'Qoder Workspace',
  runtime: {
    profile_uuid: '00000000-0000-4000-8000-000000000099',
    profile_id: 'rprof_qoder_cn',
    profile_name: 'Qoder Cloud',
    provider_kind: 'qoder_cloud',
    capabilities: externalCaps,
  },
};

const mockAccount = {
  uuid: 'acc-1',
  email_address: 'admin@example.com',
  permissions: ['organization:manage', 'members:manage', 'usage:view', 'cost:view'],
  memberships: [{ organization: { uuid: 'org-1', name: 'Acme' }, role: 'admin' }],
};

function renderWithExternalWorkspace(ui: React.ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <I18nProvider initialLocale="en">
        <AuthContext.Provider
          value={{
            status: 'authenticated',
            account: mockAccount,
            refresh: async () => undefined,
            logout: async () => undefined,
          }}
        >
          <WorkspaceContext.Provider
            value={{
              orgUuid: 'org-1',
              workspaces: [defaultWorkspace, externalWorkspace],
              activeWorkspace: externalWorkspace,
              activeWorkspaceId: externalWorkspace.id,
              isLoading: false,
              error: null,
              selectWorkspace: () => undefined,
              createWorkspace: async () => externalWorkspace,
              refreshWorkspaces: async () => undefined,
            }}
          >
            <TooltipProvider>{ui}</TooltipProvider>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

test('navigation filtering hides unsupported items while preserving org-level /settings/llm and /settings/telemetry', () => {
  const mainHrefs = visibleConsoleNavigation(externalCaps).flatMap((item) =>
    item.type === 'link' ? [item.href] : item.children.map((child) => child.href),
  );
  expect(mainHrefs).not.toContain('/deployments');
  expect(mainHrefs).not.toContain('/mcp-tunnels');
  expect(mainHrefs).not.toContain('/webhooks');
  expect(mainHrefs).not.toContain('/channels');
  expect(mainHrefs).not.toContain('/evaluations');
  expect(mainHrefs).not.toContain('/skills');
  expect(mainHrefs).not.toContain('/memory-stores');
  expect(mainHrefs).not.toContain('/credential-vaults');
  expect(mainHrefs).not.toContain('/mcp-servers');
  expect(mainHrefs).toContain('/agents');
  expect(mainHrefs).toContain('/sessions');
  expect(mainHrefs).toContain('/files');

  const settingsHrefs = visibleSettingsGroups(mockAccount, 'org-1', externalCaps).flatMap((group) =>
    group.children.map((item) => item.href),
  );
  expect(settingsHrefs).not.toContain('/observability');
  expect(settingsHrefs).toContain('/settings/llm');
  expect(settingsHrefs).toContain('/settings/telemetry');

  expect(allowedResourceSources(externalCaps)).not.toContain('deployments');
  const searchHrefs = navigationSearchRecords(
    mockAccount,
    'org-1',
    externalWorkspace.id,
    (_id, fallback) => fallback,
    externalCaps,
  ).map((record) => record.id);
  expect(searchHrefs).not.toContain('/deployments');
  expect(searchHrefs).not.toContain('/observability');
  expect(searchHrefs).toContain('/settings/llm');
  expect(searchHrefs).toContain('/settings/telemetry');
});

test('RuntimeCapabilityGuard blocks unsupported deep links and allows org-level settings routes', () => {
  const onNavigate = mock(() => undefined);
  renderWithExternalWorkspace(
    <RuntimeCapabilityGuard currentPath="/workspaces/wrkspc_qoder/deployments" onNavigate={onNavigate}>
      <div>Deployments Body</div>
    </RuntimeCapabilityGuard>,
  );

  expect(screen.queryByText('Deployments Body')).toBeNull();
  expect(screen.getByText('Not supported by this workspace runtime')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Return to overview' }));
  expect(onNavigate).toHaveBeenCalledWith('/dashboard');

  cleanup();
  renderWithExternalWorkspace(
    <RuntimeCapabilityGuard currentPath="/settings/llm">
      <div>Org LLM Models</div>
    </RuntimeCapabilityGuard>,
  );
  expect(screen.getByText('Org LLM Models')).toBeTruthy();

  cleanup();
  renderWithExternalWorkspace(
    <RuntimeCapabilityGuard currentPath="/settings/telemetry">
      <div>Org Telemetry</div>
    </RuntimeCapabilityGuard>,
  );
  expect(screen.getByText('Org Telemetry')).toBeTruthy();
});

test('ConsoleShell displays runtime engine badge for non-native workspace and hides deployments nav link', () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  setTestViewport(1280, 900);
  renderWithExternalWorkspace(
    <ConsoleShell account={mockAccount} currentPath="/dashboard" onLogout={() => undefined}>
      <div>Dashboard Body</div>
    </ConsoleShell>,
  );
  expect(screen.getAllByText('Qoder Cloud').length).toBeGreaterThan(0);
  expect(screen.queryByRole('link', { name: 'Deployments' })).toBeNull();
  expect(screen.getByRole('link', { name: 'Agents' })).toBeTruthy();
});

test('OverviewPage never prompts LLMProviderRequired when byok_llm_providers is false', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  const requestedUrls: string[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    requestedUrls.push(url);
    if (url.includes('/overview')) {
      return Response.json({
        start_time: '2026-10-26T00:00:00Z',
        end_time: '2026-11-25T00:00:00Z',
        timezone: 'UTC',
        agent_count: 0,
        active_agent_count: 0,
        session_count: 0,
        failed_session_count: 0,
        daily: [],
        agents: [],
      });
    }
    return Response.json({ error: { message: 'no llm provider' } }, { status: 400 });
  }) as typeof fetch;

  const root = createRootRoute({ component: OverviewPage });
  const router = createRouter({
    routeTree: root,
    history: createMemoryHistory({ initialEntries: ['/dashboard'] }),
  });

  renderWithExternalWorkspace(<RouterProvider router={router} />);
  expect(await screen.findByText('Create your first agent')).toBeTruthy();
  expect(requestedUrls.some((url) => url.includes('/models'))).toBe(false);
});

test('SessionInspector hides Trajectory tab when otlp_traces is false and displays Credits usage for provider_credits', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  renderWithExternalWorkspace(
    <SessionInspector
      activeTab="trajectory"
      activeLane=""
      events={[
        {
          id: 'sevt_usage_1',
          type: 'session.usage',
          usage: { model_credits: 1.25, sandbox_runtime_credits: 0.5, total_credits: 1.75 },
        },
      ]}
      eventsByLaneId={new Map()}
      lanes={[{ id: '', label: 'Orchestrator', group: 'Main', isMain: true }]}
      onActiveTabChange={() => undefined}
      onClose={() => undefined}
      onSelectEntry={() => undefined}
      onSelectLane={() => undefined}
      refreshKey={0}
      selectedEntry={null}
      session={{
        id: 'sesn_qoder_1',
        type: 'session',
        status: 'idle',
        title: 'Qoder Run',
        agent: 'agent_1',
        environment_id: 'env_1',
        resources: [],
        created_at: '2026-11-25T00:00:00Z',
        updated_at: '2026-11-25T00:01:00Z',
      }}
      workspaceId={externalWorkspace.id}
    />,
  );

  expect(screen.queryByRole('tab', { name: 'Trajectory' })).toBeNull();
  expect(screen.getByText('Credits usage')).toBeTruthy();
  expect(screen.getByText('1.75')).toBeTruthy();
  expect(screen.getByText('1.25')).toBeTruthy();
  expect(screen.getByText('0.5')).toBeTruthy();
});

test('SessionOutputFiles shows DeliverArtifacts hint when artifact_discovery is explicit_deliver_tool and no artifacts exist', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  renderWithExternalWorkspace(
    <SessionOutputFiles
      session={{
        id: 'sesn_qoder_2',
        type: 'session',
        status: 'idle',
        title: 'Qoder Output Test',
        agent: 'agent_1',
        environment_id: 'env_1',
        resources: [],
        created_at: '2026-11-25T00:00:00Z',
        updated_at: '2026-11-25T00:01:00Z',
      }}
      workspaceId={externalWorkspace.id}
      refreshKey={0}
      artifactDiscovery="explicit_deliver_tool"
      hasEvents
    >
      <div>Transcript Content</div>
    </SessionOutputFiles>,
  );

  await waitFor(() => expect(screen.getByTestId('session-deliver-artifacts-hint')).toBeTruthy());
  expect(screen.getByTestId('session-deliver-artifacts-hint').textContent).toContain('DeliverArtifacts');
});
