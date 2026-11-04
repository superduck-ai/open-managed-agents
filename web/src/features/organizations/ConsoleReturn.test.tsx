import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { AuthProvider } from '../../shared/auth/AuthProvider';
import { useAuth } from '../../shared/auth/context';
import { useOrganizations } from '../../shared/organizations/context';
import { WorkspaceProvider } from '../../shared/workspaces/WorkspaceProvider';
import { useWorkspace } from '../../shared/workspaces/context';
import { getConsoleRequestContext, setConsoleRequestContext } from '../../shared/api/client';
import { resetTestDom } from '../../test/setup';
import { InvitationEntryGate } from './InvitationEntryGate';

const { act, cleanup, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const originalScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo');
afterEach(() => {
  cleanup();
  if (originalScrollTo) Object.defineProperty(globalThis, 'scrollTo', originalScrollTo);
  else Reflect.deleteProperty(globalThis, 'scrollTo');
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.sessionStorage.clear();
  window.localStorage.clear();
});

function ScopeProbe() {
  const organizations = useOrganizations()!;
  const { activeWorkspaceId } = useWorkspace();
  const { account } = useAuth();
  return (
    <>
      <output data-testid="scope">
        {account?.memberships?.[0]?.organization?.name}:{activeWorkspaceId}
      </output>
      <output data-testid="state">
        {organizations.switching ? 'loading' : organizations.error ? 'error' : 'ready'}
      </output>
      <button onClick={() => void organizations.retry()}>重试组织</button>
      <button onClick={() => void organizations.exitOrganization?.()}>退出平台组织</button>
    </>
  );
}

for (const { fail, back, initialOrganization } of [
  { fail: true, back: true, initialOrganization: false },
  { fail: false, back: true, initialOrganization: false },
  { fail: false, back: false, initialOrganization: true },
]) {
  test(`${back ? '浏览器后退' : '返回工作台链接'}重新加载组织，${fail ? '失败可重试' : '恢复组织名和工作区'}`, async () => {
    resetTestDom('https://oma.duck.ai/platform-admin');
    Object.assign(globalThis, { scrollTo: () => {} });
    let hasOrganization = initialOrganization;
    let organizationName = initialOrganization ? '旧组织名' : '我的组织';
    const expectedWorkspace = initialOrganization ? 'ws-preferred' : 'ws';
    if (initialOrganization) {
      window.sessionStorage.setItem('oma.organization.operator', 'org');
      window.localStorage.setItem('oma.workspace.operator.org', expectedWorkspace);
    }
    let offline = false;
    const fetchMock = mock(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === '/api/bootstrap') {
        if (offline) return new Response('{}', { status: 503 });
        return Response.json({
          account: {
            uuid: 'operator',
            email_address: 'operator@example.com',
            memberships: hasOrganization
              ? [{ organization: { uuid: 'org', name: organizationName }, role: 'admin' }]
              : [],
          },
          csrf_token: 'csrf',
        });
      }
      if (path === '/api/invitations') return Response.json({ data: [] });
      return Response.json([
        { id: 'ws', type: 'workspace', name: 'Default', is_default: true, effective_role: 'workspace_admin' },
        { id: 'ws-preferred', type: 'workspace', name: '自选工作区', effective_role: 'workspace_admin' },
      ]);
    });
    globalThis.fetch = fetchMock as typeof fetch;
    const root = createRootRoute({
      component: () => (
        <>
          <ScopeProbe />
          <InvitationEntryGate />
        </>
      ),
    });
    const consoleRoute = createRoute({ getParentRoute: () => root, path: '/', component: () => <h1>工作台</h1> });
    const admin = createRoute({
      getParentRoute: () => root,
      path: '/platform-admin',
      component: () => <h1>平台管理</h1>,
    });
    const router = createRouter({
      routeTree: root.addChildren([consoleRoute, admin]),
      history: createMemoryHistory({ initialEntries: ['/', '/platform-admin'], initialIndex: 1 }),
    });
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={queries}>
        <AuthProvider>
          <WorkspaceProvider>
            <RouterProvider router={router} />
          </WorkspaceProvider>
        </AuthProvider>
      </QueryClientProvider>,
    );
    await screen.findByText('平台管理');
    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('ready'));
    expect(screen.getByTestId('scope').textContent).toBe(initialOrganization ? '旧组织名:ws-preferred' : ':');
    if (initialOrganization) {
      await act(async () => screen.getByRole('button', { name: '退出平台组织' }).click());
      expect(screen.getByTestId('scope').textContent).toBe(':');
    }
    hasOrganization = true;
    organizationName = '我的组织';
    offline = fail;
    await act(async () => {
      if (back) router.history.back();
      else await router.navigate({ to: '/' });
    });
    await screen.findByText('工作台');
    if (fail) {
      await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('error'));
      offline = false;
      await act(async () => screen.getByRole('button', { name: '重试组织' }).click());
    }
    await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe(`我的组织:${expectedWorkspace}`));
    expect(getConsoleRequestContext()).toEqual({
      organizationUuid: 'org',
      workspaceId: expectedWorkspace,
      csrfToken: 'csrf',
    });
    const callCount = fetchMock.mock.calls.length;
    await act(async () => router.navigate({ to: '/' }));
    expect(fetchMock.mock.calls.length).toBe(callCount);
    cleanup();
    queries.clear();
  });
}
