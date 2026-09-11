import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { type ReactNode } from 'react';

import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import type { McpTunnel } from './api';
import { McpTunnelDetailContent } from './McpTunnelDetailPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;
const originalFetch = globalThis.fetch;
const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard');

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  restoreClipboard();
});

describe('MCP tunnel detail page', () => {
  test('renders overview, connector setup, readiness, and channel URLs from one detail request', async () => {
    resetTestDom(detailURL);
    const api = mockTunnelDetailApi(activeTunnel);
    renderDetail();

    expect(await screen.findByRole('heading', { name: 'Private tools' })).toBeTruthy();
    expect(screen.getAllByText('Ready').length).toBeGreaterThan(0);
    expect(screen.getByText('Connector setup')).toBeTruthy();
    expect(screen.getByText('2 connector instances and 2 live channels.')).toBeTruthy();
    expect(screen.getByText(`${activeTunnel.mcp_url}/secondary`)).toBeTruthy();
    expect(screen.getByText('••••••••••••••••••••••••')).toBeTruthy();
    expect(api.requests[0].method).toBe('GET');
    expect(api.requests[0].url).toEndWith(`/mcp_tunnels/${activeTunnel.id}`);
  });

  test('uses the shared inline copy feedback without showing a toast', async () => {
    resetTestDom(detailURL);
    mockTunnelDetailApi(activeTunnel);
    const clipboardWrite = mock(async (_value: string) => undefined);
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      configurable: true,
      value: { writeText: clipboardWrite },
    });
    renderDetail();

    const copyButton = await screen.findByRole('button', { name: 'Copy Canonical MCP URL' });
    fireEvent.click(copyButton);

    await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith(activeTunnel.mcp_url));
    expect(await screen.findByRole('button', { name: 'Copied' })).toBeTruthy();
    expect(screen.queryByText('MCP URL copied')).toBeNull();
  });

  test('renders the Console API detail when the single-resource request fails', async () => {
    resetTestDom(detailURL);
    globalThis.fetch = mock(async () =>
      jsonResponse({ error: 'unavailable', message: 'Connection snapshot is unavailable' }, 503),
    );
    renderDetail();

    expect(await screen.findByText('Could not load this MCP tunnel')).toBeTruthy();
    expect(screen.getByText('Connection snapshot is unavailable')).toBeTruthy();
  });

  test('auto-reveals after creation without putting the token in query data', async () => {
    resetTestDom(detailURL);
    const api = mockTunnelDetailApi(activeTunnel);
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const consumed = mock(() => undefined);
    renderDetail(queryClient, { autoReveal: true, onAutoRevealConsumed: consumed });

    expect(await screen.findByText('secret-created')).toBeTruthy();
    expect(consumed).toHaveBeenCalled();
    expect(api.requests.some((request) => request.url.endsWith('/reveal_token'))).toBe(true);
    expect(
      JSON.stringify(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ),
    ).not.toContain('secret-created');
  });

  test('reveals, hides, and rotates the token with CSRF protection', async () => {
    resetTestDom(detailURL);
    const api = mockTunnelDetailApi(activeTunnel);
    renderDetail();

    await screen.findByText('Connector setup');
    fireEvent.click(screen.getByRole('button', { name: 'View token' }));
    expect(await screen.findByText('secret-created')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Hide' }));
    expect(screen.queryByText('secret-created')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Actions for Private tools' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Rotate token' }));
    const dialog = screen.getByRole('alertdialog', { name: 'Rotate tunnel token?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Rotate token' }));
    expect(await screen.findByText('secret-rotated')).toBeTruthy();

    const mutations = api.requests.filter((request) => request.method === 'POST');
    expect(mutations.length).toBe(2);
    expect(mutations.every((request) => request.headers.get('x-csrf-token') === 'csrf_test')).toBe(true);
  });

  test('does not expose a revealed token after the route switches to another tunnel', async () => {
    resetTestDom(detailURL);
    mockTunnelDetailApi(activeTunnel);
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const rendered = renderDetail(queryClient);

    await screen.findByText('Connector setup');
    fireEvent.click(screen.getByRole('button', { name: 'View token' }));
    expect(await screen.findByText('secret-created')).toBeTruthy();

    rendered.rerender(
      <TunnelHarness queryClient={queryClient}>
        <McpTunnelDetailContent routeWorkspaceId="default" tunnelId="tunnel_fedcba9876543210fedcba9876543210" />
      </TunnelHarness>,
    );

    expect(screen.queryByText('secret-created')).toBeNull();
  });

  test('probes the selected live channel and displays the discovered tools', async () => {
    resetTestDom(detailURL);
    const api = mockTunnelDetailApi(activeTunnel);
    renderDetail();

    await screen.findByText('secondary');
    const secondaryRow = screen.getByText('secondary').closest('tr');
    if (!secondaryRow) throw new Error('secondary row was not rendered');
    fireEvent.click(within(secondaryRow).getByRole('button', { name: 'Test' }));

    const dialog = await screen.findByRole('dialog', { name: 'MCP connection test passed' });
    expect(within(dialog).getByText('echo')).toBeTruthy();
    const probeRequest = api.requests.find((request) => request.url.endsWith('/probe'));
    expect(probeRequest?.body).toEqual({ channel: 'secondary' });
  });

  test('archives permanently and renders the detail as read-only', async () => {
    resetTestDom(detailURL);
    mockTunnelDetailApi(activeTunnel);
    renderDetail();

    await screen.findByText('Danger zone');
    fireEvent.click(screen.getByRole('button', { name: 'Archive' }));
    const dialog = screen.getByRole('alertdialog', { name: 'Archive MCP tunnel?' });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive' }));

    expect((await screen.findAllByText('Archived')).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'View token' }).hasAttribute('disabled')).toBe(true);
    expect(screen.getByRole('button', { name: 'Archive' }).hasAttribute('disabled')).toBe(true);
  });

  test.each([
    ['disconnected', [], 'Waiting for connector'],
    ['unknown', [], 'Status unavailable'],
    ['connected', [], 'Connected, no channels'],
  ] as const)('maps %s connection facts to %s', async (state, channels, expected) => {
    resetTestDom(detailURL);
    mockTunnelDetailApi({ ...activeTunnel, connection: { state, instance_count: 0, channels } });
    renderDetail();
    expect((await screen.findAllByText(expected)).length).toBeGreaterThan(0);
  });
});

type RecordedRequest = {
  url: string;
  method: string;
  headers: Headers;
  body?: Record<string, unknown>;
};

function mockTunnelDetailApi(initialTunnel: McpTunnel) {
  let tunnel = initialTunnel;
  const requests: RecordedRequest[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    const body = typeof init?.body === 'string' ? (JSON.parse(init.body) as Record<string, unknown>) : undefined;
    requests.push({ url, method, headers, body });
    if (method === 'GET') return jsonResponse(tunnel);
    if (url.endsWith('/reveal_token')) {
      return jsonResponse({ id: 'tnltok_created', type: 'tunnel_token', tunnel_token: 'secret-created' });
    }
    if (url.endsWith('/rotate_token')) {
      return jsonResponse({ id: 'tnltok_rotated', type: 'tunnel_token', tunnel_token: 'secret-rotated' });
    }
    if (url.endsWith('/probe')) {
      return jsonResponse({
        status: 'ok',
        channel: String(body?.channel || 'main'),
        protocol_version: '2025-06-18',
        server_name: 'stub-server',
        server_version: '1.0.0',
        tools: [{ name: 'echo', description: 'Echo input' }],
      });
    }
    if (url.endsWith('/archive')) {
      tunnel = {
        ...tunnel,
        archived_at: '2026-08-27T00:00:00Z',
        connection: { state: 'disconnected', instance_count: 0, channels: [] },
      };
      return jsonResponse(tunnel);
    }
    return jsonResponse({ error: 'not_found', message: 'Not found' }, 404);
  });
  return { requests };
}

function renderDetail(
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } }),
  options: { autoReveal?: boolean; onAutoRevealConsumed?: () => void } = {},
) {
  return render(
    <TunnelHarness queryClient={queryClient}>
      <McpTunnelDetailContent
        routeWorkspaceId="default"
        tunnelId={activeTunnel.id}
        autoReveal={options.autoReveal}
        onAutoRevealConsumed={options.onAutoRevealConsumed}
      />
    </TunnelHarness>,
  );
}

function TunnelHarness({ children, queryClient }: { children: ReactNode; queryClient: QueryClient }) {
  return (
    <I18nProvider initialLocale="en">
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
    channels: [
      { name: 'main', process_affinity: true, instance_count: 2 },
      { name: 'secondary', process_affinity: false, instance_count: 1 },
    ],
  },
};

function restoreClipboard() {
  if (originalClipboardDescriptor) {
    Object.defineProperty(globalThis.navigator, 'clipboard', originalClipboardDescriptor);
    return;
  }
  delete (globalThis.navigator as unknown as Record<string, unknown>).clipboard;
}

const detailURL = `https://oma.duck.ai/settings/workspaces/default/mcp-tunnels/${activeTunnel.id}`;
