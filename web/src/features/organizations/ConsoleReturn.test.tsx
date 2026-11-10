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
import { AuthContext, useAuth } from '../../shared/auth/context';
import { OrganizationContext, useOrganizations } from '../../shared/organizations/context';
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
  const { activeWorkspaceId, workspaces } = useWorkspace();
  const { account } = useAuth();
  return (
    <>
      <output data-testid="scope">
        {account?.memberships?.[0]?.organization?.name}:{activeWorkspaceId}
      </output>
      <output data-testid="workspaces">{workspaces.map((workspace) => workspace.name).join(',')}</output>
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
    let returning = false;
    const fetchMock = mock(async (input: RequestInfo | URL) => {
      const path = String(input);
      if (path === '/api/bootstrap') {
        if (offline) return new Response('{}', { status: 503 });
        return Response.json({
          account: {
            uuid: 'operator',
            email_address: 'operator@example.com',
            memberships: hasOrganization
              ? [
                  ...(initialOrganization
                    ? [{ organization: { uuid: 'first', name: '第一个组织' }, role: 'admin' }]
                    : []),
                  { organization: { uuid: 'org', name: organizationName }, role: 'admin' },
                ]
              : [],
          },
          csrf_token: 'csrf',
        });
      }
      if (path === '/api/invitations') return Response.json({ data: [] });
      return Response.json([
        { id: 'ws', type: 'workspace', name: 'Default', is_default: true, effective_role: 'workspace_admin' },
        {
          id: 'ws-preferred',
          type: 'workspace',
          name: returning ? '更新工作区' : '自选工作区',
          effective_role: 'workspace_admin',
        },
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
    const workspaceCallsBefore = fetchMock.mock.calls.filter(
      ([input]) => String(input) === '/api/console/organizations/org/workspaces',
    ).length;
    returning = true;
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
    expect(screen.getByTestId('workspaces').textContent).toContain('更新工作区');
    expect(
      fetchMock.mock.calls.filter(([input]) => String(input) === '/api/console/organizations/org/workspaces'),
    ).toHaveLength(workspaceCallsBefore + 1);
    const callCount = fetchMock.mock.calls.length;
    await act(async () => router.navigate({ to: '/' }));
    expect(fetchMock.mock.calls.length).toBe(callCount);
    cleanup();
    queries.clear();
  });
}

test('进入平台组织工作台不重复进入或触发可能拒绝的重试', async () => {
  resetTestDom('https://oma.duck.ai/platform-admin');
  Object.assign(globalThis, { scrollTo: () => {} });
  const retry = mock(async () => {
    throw new Error('不应调用平台重试');
  });
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  const root = createRootRoute({ component: InvitationEntryGate });
  const admin = createRoute({
    getParentRoute: () => root,
    path: '/platform-admin',
    component: () => <h1>平台管理</h1>,
  });
  const consoleRoute = createRoute({
    getParentRoute: () => root,
    path: '/workbench',
    component: () => <h1>目标工作台</h1>,
  });
  const router = createRouter({
    routeTree: root.addChildren([admin, consoleRoute]),
    history: createMemoryHistory({ initialEntries: ['/platform-admin'] }),
  });
  const queries = new QueryClient();
  render(
    <QueryClientProvider client={queries}>
      <AuthContext.Provider
        value={{
          status: 'authenticated',
          account: { uuid: 'operator', email_address: 'operator@example.com' },
          refresh: async () => undefined,
          logout: async () => {},
        }}
      >
        <OrganizationContext.Provider
          value={{
            platformScope: { accountUuid: 'operator', targetOrganizationUuid: 'target' },
            orgUuid: 'target',
            memberships: [],
            switching: false,
            error: null,
            switchOrganization: retry,
            retry,
          }}
        >
          <RouterProvider router={router} />
        </OrganizationContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
  await screen.findByText('平台管理');
  await act(async () => router.navigate({ to: '/workbench' }));
  await screen.findByText('目标工作台');
  expect(retry).not.toHaveBeenCalled();
  cleanup();
  queries.clear();
});
