import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  RouterContextProvider,
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router';
import { afterEach, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { MCPServerDetailPage, MCPServersPage } from './MCPServersPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('lists workspace MCP servers and creates a reusable configuration', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers');
  const requests: Array<{ url: string; method: string; body: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    requests.push({ url, method, body: typeof init?.body === 'string' ? init.body : '' });
    if (method === 'POST') {
      return Response.json(mcpServer({ id: 'mcpsrv_created', name: 'billing', url: 'https://billing.example/mcp' }));
    }
    return Response.json({ data: [mcpServer()], next_page: null });
  });

  renderPage(<MCPServersPage />);

  expect(await screen.findByRole('heading', { name: 'MCP Servers' })).toBeTruthy();
  expect(await screen.findByText('internal-docs')).toBeTruthy();
  expect(screen.getByRole('region', { name: 'MCP servers list' }).closest('[data-slot="card"]')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Create MCP server' }));

  const dialog = await screen.findByRole('dialog', { name: 'Create MCP server' });
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'billing' } });
  fireEvent.change(screen.getByLabelText('Endpoint URL'), { target: { value: 'https://billing.example/mcp' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create', hidden: false }));

  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  const createRequest = requests.find((request) => request.method === 'POST');
  expect(createRequest?.url).toBe('/api/console/organizations/org_test/workspaces/default/mcp_servers');
  expect(JSON.parse(createRequest?.body ?? '{}')).toEqual({
    name: 'billing',
    url: 'https://billing.example/mcp',
  });
  expect(await screen.findByRole('dialog', { name: 'billing' })).toBeTruthy();
  expect(window.location.pathname).toBe('/workspaces/default/mcp-servers/mcpsrv_created');
  expect(dialog).toBeTruthy();
});

test('archives a workspace MCP server after confirmation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers');
  const requests: Array<{ url: string; method: string }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    requests.push({ url, method });
    if (url.endsWith('/archive')) {
      return Response.json(mcpServer({ status: 'archived' }));
    }
    return Response.json({ data: [mcpServer()], next_page: null });
  });

  renderPage(<MCPServersPage />);
  expect(await screen.findByText('internal-docs')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Actions for internal-docs' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Archive' }));
  const confirmation = await screen.findByRole('alertdialog', { name: 'Archive MCP server?' });
  fireEvent.click(screen.getByRole('button', { name: 'Archive', hidden: false }));

  await waitFor(() =>
    expect(requests.some((request) => request.url.endsWith('/mcpsrv_test/archive') && request.method === 'POST')).toBe(
      true,
    ),
  );
  expect(confirmation).toBeTruthy();
});

test('paginates the management list with the backend cursor', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers');
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.includes('page=next_cursor')) {
      return Response.json({ data: [mcpServer({ id: 'mcpsrv_second', name: 'second-page' })], next_page: null });
    }
    return Response.json({ data: [mcpServer()], next_page: 'next_cursor' });
  });

  renderPage(<MCPServersPage />);
  expect(await screen.findByText('internal-docs')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

  expect(await screen.findByText('second-page')).toBeTruthy();
  expect(screen.queryByText('internal-docs')).toBeNull();
  expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);
});

test('opens a resource detail panel, then updates and deletes it', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers/mcpsrv_test');
  const requests: Array<{ url: string; method: string; body: string }> = [];
  let currentServer = mcpServer();
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    requests.push({ url, method, body: typeof init?.body === 'string' ? init.body : '' });
    if (method === 'DELETE') {
      return Response.json({ id: 'mcpsrv_test', type: 'mcp_server_deleted', deleted: true });
    }
    if (method === 'POST') {
      currentServer = mcpServer({ name: 'renamed-docs' });
      return Response.json(currentServer);
    }
    if (url.endsWith('/mcpsrv_test')) {
      return Response.json(currentServer);
    }
    return Response.json({ data: [currentServer], next_page: null });
  });

  renderPage(<MCPServerDetailPage />);
  const detail = await screen.findByRole('dialog', { name: 'internal-docs' });
  expect(within(detail).getByText('https://docs.example/mcp')).toBeTruthy();
  fireEvent.click(within(detail).getByRole('button', { name: 'Actions for internal-docs' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Edit' }));

  const dialog = await screen.findByRole('dialog', { name: 'Edit MCP server' });
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'renamed-docs' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(requests.some((request) => request.method === 'POST')).toBe(true));
  expect(JSON.parse(requests.find((request) => request.method === 'POST')?.body ?? '{}')).toEqual({
    name: 'renamed-docs',
    url: 'https://docs.example/mcp',
  });
  const updatedDetail = await screen.findByRole('dialog', { name: 'renamed-docs' });

  fireEvent.click(within(updatedDetail).getByRole('button', { name: 'Actions for renamed-docs' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Delete', hidden: false }));
  await waitFor(() => expect(requests.some((request) => request.method === 'DELETE')).toBe(true));
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'renamed-docs' })).toBeNull());
  expect(window.location.pathname).toBe('/workspaces/default/mcp-servers');
});

test('opens a row in the right-side resource detail panel and syncs the URL', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers');
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith('/mcpsrv_test')) {
      return Response.json(mcpServer());
    }
    return Response.json({ data: [mcpServer()], next_page: null });
  });

  renderPage(<MCPServersPage />);
  const row = await screen.findByRole('row', { name: /mcpsrv_test internal-docs/ });
  fireEvent.click(row);

  expect(await screen.findByRole('dialog', { name: 'internal-docs' })).toBeTruthy();
  expect(window.location.pathname).toBe('/workspaces/default/mcp-servers/mcpsrv_test');
});

test('shows a retryable error when a detail route cannot load its MCP server', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers/mcpsrv_missing');
  let detailCalls = 0;
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    if (url.endsWith('/mcpsrv_missing')) {
      detailCalls++;
      return Response.json({ error: { message: 'MCP server not found' } }, { status: 404 });
    }
    return Response.json({ data: [], next_page: null });
  });

  renderPage(<MCPServerDetailPage />);

  expect(await screen.findByText('MCP server not found')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await waitFor(() => expect(detailCalls).toBe(2));
});

test('waits for the route workspace before loading MCP servers', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/wrkspc_beta/mcp-servers');
  const requests: string[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    requests.push(String(input));
    return Response.json({ data: [], next_page: null });
  });

  renderPage(<MCPServersPage />);

  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(requests).toEqual([]);
  expect(screen.queryByText('No MCP servers match this view.')).toBeNull();
});

function mcpServer(overrides: Record<string, unknown> = {}) {
  return {
    id: 'mcpsrv_test',
    type: 'mcp_server',
    name: 'internal-docs',
    transport_type: 'url',
    url: 'https://docs.example/mcp',
    status: 'active',
    created_at: '2026-08-13T00:00:00Z',
    updated_at: '2026-08-13T00:00:00Z',
    archived_at: null,
    ...overrides,
  };
}

function renderPage(children: ReactNode, workspaceOverrides: Partial<WorkspaceContextValue> = {}) {
  const rootRoute = createRootRoute();
  const pageRoute = createRoute({ getParentRoute: () => rootRoute, path: '$' });
  const router = createRouter({
    history: createBrowserHistory({ window }),
    routeTree: rootRoute.addChildren([pageRoute]),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  const workspace: WorkspaceContextValue = {
    orgUuid: 'org_test',
    workspaces: [defaultWorkspace],
    activeWorkspace: defaultWorkspace,
    activeWorkspaceId: defaultWorkspace.id,
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => defaultWorkspace,
    refreshWorkspaces: async () => undefined,
    ...workspaceOverrides,
  };
  const auth: AuthContextValue = {
    account: null,
    status: 'authenticated',
    csrfToken: 'csrf_test',
    refresh: async () => undefined,
    logout: async () => undefined,
  };
  return render(
    <RouterContextProvider router={router}>
      <I18nProvider initialLocale="en">
        <AuthContext.Provider value={auth}>
          <WorkspaceContext.Provider value={workspace}>
            <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    </RouterContextProvider>,
  );
}
