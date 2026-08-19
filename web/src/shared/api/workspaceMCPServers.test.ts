import { afterEach, expect, mock, test } from 'bun:test';
import { setConsoleRequestContext } from './client';
import { createWorkspaceMCPServer, listWorkspaceMCPServers } from './workspaceMCPServers';

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
