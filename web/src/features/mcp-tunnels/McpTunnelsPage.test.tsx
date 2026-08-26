import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { type ReactNode } from 'react';

import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import type { McpTunnel } from './api';
import { visibleTunnelRefreshInterval } from './config';
import { McpTunnelsContent } from './McpTunnelsPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

describe('MCP tunnels list page', () => {
  test('renders empty and error states', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
    mockTunnelApi([]);
    const view = renderPage();
    expect(await screen.findByText('No MCP tunnels yet')).toBeTruthy();
    view.unmount();

    globalThis.fetch = mock(async () => jsonResponse({ error: 'unavailable', message: 'Redis is unavailable' }, 503));
    renderPage();
    expect(await screen.findByText('Could not load MCP tunnels')).toBeTruthy();
    expect(screen.getByText('Redis is unavailable')).toBeTruthy();
  });

  test('creates a tunnel and navigates to its canonical detail route without revealing on the list page', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
    const api = mockTunnelApi([]);
    const navigate = mock(() => undefined);
    renderPage(undefined, 'default', 'en', navigate);

    await screen.findByText('No MCP tunnels yet');
    fireEvent.click(screen.getAllByRole('button', { name: 'New tunnel' })[0]);
    const createDialog = screen.getByRole('dialog', { name: 'Create MCP tunnel' });
    fireEvent.change(within(createDialog).getByPlaceholderText('Local tools'), { target: { value: 'Private tools' } });
    fireEvent.click(within(createDialog).getByRole('button', { name: 'Create' }));

    await waitFor(() => expect(navigate).toHaveBeenCalled());
    expect(navigate).toHaveBeenCalledWith(`/settings/workspaces/default/mcp-tunnels/${activeTunnel.id}`, {
      mcpTunnelCreated: true,
    });
    expect(api.requests.some((request) => request.url.endsWith('/reveal_token'))).toBe(false);
    const createRequest = api.requests.find(
      (request) => request.method === 'POST' && request.url.endsWith('/mcp_tunnels'),
    );
    expect(createRequest?.headers.get('x-csrf-token')).toBe('csrf_test');
  });

  test('opens detail only from ID and name links and keeps list actions focused', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
    mockTunnelApi([activeTunnel]);
    const navigate = mock(() => undefined);
    renderPage(undefined, 'default', 'en', navigate);

    const nameLink = await screen.findByRole('link', { name: 'Private tools' });
    const idLink = screen.getByRole('link', { name: activeTunnel.id });
    const headers = screen.getAllByRole('columnheader').map((header) => header.textContent);
    expect(headers.slice(0, 2)).toEqual(['ID', 'Name']);
    expect(nameLink.getAttribute('href')).toBe(`/settings/workspaces/default/mcp-tunnels/${activeTunnel.id}`);
    expect(idLink.getAttribute('href')).toBe(`/settings/workspaces/default/mcp-tunnels/${activeTunnel.id}`);

    fireEvent.click(nameLink.closest('tr')!);
    expect(navigate).not.toHaveBeenCalled();

    fireEvent.click(idLink);
    expect(navigate).toHaveBeenCalledWith(`/settings/workspaces/default/mcp-tunnels/${activeTunnel.id}`);
    navigate.mockClear();
    fireEvent.click(nameLink);
    expect(navigate).toHaveBeenCalledWith(`/settings/workspaces/default/mcp-tunnels/${activeTunnel.id}`);

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Private tools' }));
    expect(screen.getByRole('menuitem', { name: 'Copy tunnel ID' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Copy MCP URL' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Archive' })).toBeTruthy();
    expect(screen.queryByRole('menuitem', { name: 'View token' })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: 'Test connection' })).toBeNull();
  });

  test('archives through a confirmation dialog', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
    const api = mockTunnelApi([activeTunnel]);
    renderPage();

    await screen.findByText('Private tools');
    fireEvent.click(screen.getByRole('button', { name: 'Actions for Private tools' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive' }));
    const dialog = screen.getByRole('alertdialog', { name: 'Archive MCP tunnel?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive' }));

    await waitFor(() => expect(api.requests.some((request) => request.url.endsWith('/archive'))).toBe(true));
    await waitFor(() => expect(screen.queryByText('Private tools')).toBeNull());
  });

  test('polls only while the page is visible', () => {
    expect(visibleTunnelRefreshInterval()).toBe(10_000);
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    expect(visibleTunnelRefreshInterval()).toBe(false);
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
  });

  test('filters by name or exact ID and can include archived tunnels', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/default/mcp-tunnels');
    const archivedTunnel: McpTunnel = {
      ...activeTunnel,
      id: 'tunnel_fedcba9876543210fedcba9876543210',
      display_name: 'Archived tools',
      archived_at: '2026-08-21T03:00:00Z',
    };
    const api = mockTunnelApi([activeTunnel, archivedTunnel]);
    renderPage();

    await screen.findByText('Private tools');
    const search = screen.getByPlaceholderText('Search by name or exact ID');
    fireEvent.change(search, { target: { value: 'missing' } });
    expect(await screen.findByText('No matching MCP tunnels')).toBeTruthy();
    fireEvent.change(search, { target: { value: activeTunnel.id } });
    expect(await screen.findByText('Private tools')).toBeTruthy();

    fireEvent.change(search, { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Status Active' }));
    fireEvent.click(await screen.findByRole('menuitemradio', { name: 'All' }));
    expect(await screen.findByText('Archived tools')).toBeTruthy();
    expect(api.requests.some((request) => request.url.includes('include_archived=true'))).toBe(true);
  });

  test('uses the console locale and route workspace', async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces/wrkspc_route/mcp-tunnels');
    const api = mockTunnelApi([]);
    renderPage(undefined, 'wrkspc_route', 'zh-CN');

    expect(await screen.findByRole('heading', { name: 'MCP 隧道' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '新建隧道' })).toBeTruthy();
    expect(api.requests[0].url).toContain('/workspaces/wrkspc_route/mcp_tunnels');
  });
});

type RecordedRequest = {
  url: string;
  method: string;
  headers: Headers;
  body?: Record<string, unknown>;
};

function mockTunnelApi(initialTunnels: McpTunnel[]) {
  let tunnels = [...initialTunnels];
  const requests: RecordedRequest[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : undefined;
    requests.push({ url, method, headers, body });
    if (method === 'GET') {
      const includeArchived = url.includes('include_archived=true');
      return jsonResponse(tunnels.filter((tunnel) => includeArchived || !tunnel.archived_at));
    }
    if (url.endsWith('/mcp_tunnels')) {
      const created = { ...activeTunnel, display_name: String(body?.display_name || '') };
      tunnels = [...tunnels, created];
      return jsonResponse(created);
    }
    if (url.endsWith('/archive')) {
      tunnels = tunnels.map((tunnel) =>
        url.includes(tunnel.id) ? { ...tunnel, archived_at: '2026-08-21T03:00:00Z' } : tunnel,
      );
      return jsonResponse(tunnels.find((tunnel) => url.includes(tunnel.id)));
    }
    return jsonResponse({ error: 'not_found', message: 'Not found' }, 404);
  });
  return { requests };
}

function renderPage(
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  routeWorkspaceId = 'default',
  locale: Locale = 'en',
  onNavigate?: (href: string, state?: Record<string, unknown>) => void,
) {
  return render(
    <TunnelHarness queryClient={queryClient} locale={locale}>
      <McpTunnelsContent routeWorkspaceId={routeWorkspaceId} onNavigate={onNavigate} />
    </TunnelHarness>,
  );
}

function TunnelHarness({
  children,
  queryClient,
  locale,
}: {
  children: ReactNode;
  queryClient: QueryClient;
  locale: Locale;
}) {
  return (
    <I18nProvider initialLocale={locale}>
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider value={authValue}>
          <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>
    </I18nProvider>
  );
}

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
}

const authValue: AuthContextValue = {
  account: { uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' },
  status: 'authenticated',
  csrfToken: 'csrf_test',
  refresh: async () => ({ account: { uuid: 'acct_test', email_address: 'test@example.com' } }),
  logout: async () => undefined,
};

const workspaceValue: WorkspaceContextValue = {
  orgUuid: 'org_test',
  workspaces: [defaultWorkspace],
  activeWorkspace: defaultWorkspace,
  activeWorkspaceId: defaultWorkspace.id,
  isLoading: false,
  error: null,
  selectWorkspace: () => undefined,
  createWorkspace: async () => defaultWorkspace,
  refreshWorkspaces: async () => undefined,
};

const activeTunnel: McpTunnel = {
  id: 'tunnel_0123456789abcdef0123456789abcdef',
  type: 'tunnel',
  display_name: 'Private tools',
  domain: 'tunnel.example',
  created_at: '2026-08-21T00:00:00Z',
  archived_at: null,
  mcp_url: 'https://oma.example/v1/mcp/tunnel_0123456789abcdef0123456789abcdef',
  connection: {
    state: 'connected',
    instance_count: 2,
    channels: [{ name: 'main', process_affinity: true, instance_count: 2 }],
  },
};
