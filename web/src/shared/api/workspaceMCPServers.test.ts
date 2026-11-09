import { afterEach, expect, mock, test } from 'bun:test';
import { setConsoleRequestContext } from './client';
import { createWorkspaceMCPServer, listAllWorkspaceMCPServers, listWorkspaceMCPServers } from './workspaceMCPServers';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

test('binds workspace MCP requests to the route workspace instead of stale global context', async () => {
  setConsoleRequestContext({ organizationUuid: 'org_test', workspaceId: 'wrkspc_old' });
  let captured: { path: string; request?: RequestInit } | undefined;
  globalThis.fetch = mock(async (input: RequestInfo | URL, request?: RequestInit) => {
    captured = { path: String(input), request };
    return Response.json({ data: [], next_page: null });
  });

  await listWorkspaceMCPServers('org_test', 'wrkspc_route');

  expect(captured?.path).toBe('/api/console/organizations/org_test/workspaces/wrkspc_route/mcp_servers');
  expect(new Headers(captured?.request?.headers).get('X-Workspace-ID')).toBe('wrkspc_route');
});

test('follows cursors when loading every workspace MCP server', async () => {
  const paths: string[] = [];
  const signals: AbortSignal[] = [];
  const controller = new AbortController();
  globalThis.fetch = mock(async (input: RequestInfo | URL, request?: RequestInit) => {
    const path = String(input);
    paths.push(path);
    signals.push(request?.signal as AbortSignal);
    if (path.includes('page=next')) {
      return Response.json({
        data: [{ id: 'mcp_second', name: 'second', url: 'https://second.example.com/mcp' }],
        next_page: null,
      });
    }
    return Response.json({
      data: [{ id: 'mcp_first', name: 'first', url: 'https://first.example.com/mcp' }],
      next_page: 'next',
    });
  });

  const page = await listAllWorkspaceMCPServers('org_test', 'wrkspc_route', controller.signal);
  controller.abort();

  expect(paths).toHaveLength(2);
  expect(paths[1]).toContain('page=next');
  expect(signals).toHaveLength(2);
  expect(signals.every((signal) => signal.aborted)).toBe(true);
  expect(page.data.map((server) => server.id)).toEqual(['mcp_first', 'mcp_second']);
});

test('rejects a repeated workspace MCP server cursor', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({
      data: [{ id: 'mcp_first', name: 'first', url: 'https://first.example.com/mcp' }],
      next_page: 'same',
    }),
  );

  await expect(listAllWorkspaceMCPServers('org_test', 'wrkspc_route')).rejects.toThrow('repeated cursor');
});

test('sends the route workspace and CSRF token when creating a workspace MCP server', async () => {
  let captured: RequestInit | undefined;
  globalThis.fetch = mock(async (_input: RequestInfo | URL, request?: RequestInit) => {
    captured = request;
    return Response.json({ id: 'mcpsrv_test' });
  });

  await createWorkspaceMCPServer(
    'org_test',
    'wrkspc_route',
    { name: 'docs', url: 'https://docs.example.com/mcp' },
    'csrf_test',
  );

  const headers = new Headers(captured?.headers);
  expect(captured?.method).toBe('POST');
  expect(headers.get('X-Workspace-ID')).toBe('wrkspc_route');
  expect(headers.get('X-CSRF-Token')).toBe('csrf_test');
});
