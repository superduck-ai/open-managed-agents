import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { AuthContext, type AuthStatus } from '../../shared/auth/context';
import { OrganizationContext } from '../../shared/organizations/context';
import { InvitationEntryGate } from './InvitationEntryGate';
import { InvitationsPage } from './InvitationsPage';
import { invitationReturnTo } from './api';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});
const invitation = {
  id: 'invite-first',
  organization_uuid: 'new-org',
  organization_name: '受邀组织',
  role: 'developer',
  invited_at: '2026-09-01',
  expires_at: '2027-01-01',
};

function mount(path = '/agents/original', status: AuthStatus = 'authenticated') {
  resetTestDom('https://oma.duck.ai' + path);
  Object.assign(globalThis, { scrollTo: () => {} });
  const refresh = mock(async () => undefined);
  const switchOrganization = mock(async () => {});
  const root = createRootRoute({ component: InvitationEntryGate });
  const routes = [
    createRoute({ getParentRoute: () => root, path: '/', component: () => <h1>控制台首页</h1> }),
    createRoute({ getParentRoute: () => root, path: '/agents/original', component: () => <h1>原始资源页面</h1> }),
    createRoute({ getParentRoute: () => root, path: '/login', component: () => <h1>登录页面</h1> }),
    createRoute({
      getParentRoute: () => root,
      path: '/invites',
      component: InvitationsPage,
      validateSearch: (search: Record<string, unknown>) => ({
        returnTo: invitationReturnTo(typeof search.returnTo === 'string' ? search.returnTo : undefined),
      }),
    }),
  ];
  const router = createRouter({
    routeTree: root.addChildren(routes),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          status,
          account:
            status === 'authenticated' ? { uuid: 'account', email_address: 'test@example.com', memberships: [] } : null,
          refresh,
          logout: async () => {},
        }}
      >
        <OrganizationContext.Provider
          value={{
            memberships: [],
            switching: false,
            error: new Error('旧组织已失效'),
            switchOrganization,
            retry: async () => {},
          }}
        >
          <RouterProvider router={router} />
        </OrganizationContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
  return { router, refresh, switchOrganization, queryClient };
}

test('查询失败不会渲染业务页面，可重试进入邀请页', async () => {
  let failed = true;
  globalThis.fetch = mock(async () =>
    failed ? Response.json({}, { status: 503 }) : Response.json({ data: [invitation] }),
  ) as typeof fetch;
  mount();
  await screen.findByText('组织邀请检查失败。');
  expect(screen.queryByText('原始资源页面')).toBeNull();
  failed = false;
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  await screen.findByRole('heading', { name: '接受组织邀请' });
});

test('查询失败可继续，不会在业务导航中重复检查', async () => {
  globalThis.fetch = mock(async () => Response.json({}, { status: 503 })) as typeof fetch;
  mount();
  fireEvent.click(await screen.findByRole('button', { name: '继续控制台' }));
  await screen.findByText('原始资源页面');
});

test('未登录直达邀请页先登录，保留返回邀请页的目标', async () => {
  const fetchMock = mock(async () => Response.json({ data: [] }));
  globalThis.fetch = fetchMock as typeof fetch;
  const { router } = mount('/invites', 'anonymous');
  await screen.findByText('登录页面');
  expect(router.state.location.search).toEqual({ returnTo: '/invites?returnTo=%2F' });
  expect(fetchMock).not.toHaveBeenCalled();
});

test('有邀请时主动呈现独立页面，不依赖旧组织权限，也不自动接受', async () => {
  const fetchMock = mock(async (_input: RequestInfo | URL, _options?: RequestInit) =>
    Response.json({ data: [invitation] }),
  );
  globalThis.fetch = fetchMock as typeof fetch;
  const { router, switchOrganization } = mount();
  await screen.findByText('管理员邀请你加入 受邀组织');
  expect(router.state.location.pathname).toBe('/invites');
  expect(screen.queryByText('原始资源页面')).toBeNull();
  expect(fetchMock.mock.calls.every(([, options]) => !options?.method)).toBe(true);
  expect(switchOrganization).not.toHaveBeenCalled();
});

test('没有邀请时保留原目标页面', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  const { router } = mount();
  await screen.findByText('原始资源页面');
  expect(router.state.location.pathname).toBe('/agents/original');
});

test('稍后处理返回原页面，本次打开不会反复跳转；重新打开会再次提示', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [invitation] })) as typeof fetch;
  const { router } = mount();
  fireEvent.click(await screen.findByRole('button', { name: '稍后处理，返回控制台' }));
  await screen.findByText('原始资源页面');
  await router.navigate({ href: '/' });
  await screen.findByText('控制台首页');
  cleanup();
  mount('/');
  await screen.findByRole('heading', { name: '接受组织邀请' });
});

test('多份邀请独立处理；接受后不切换，点击进入组织才到首页', async () => {
  let pending = [invitation, { ...invitation, id: 'invite-second', organization_name: '另一组织' }];
  globalThis.fetch = mock(async (input) => {
    const path = String(input);
    if (path.endsWith('/accept') || path.endsWith('/decline')) {
      const id = path.split('/')[3];
      pending = pending.filter((item) => item.id !== id);
      return Response.json({
        id,
        organization_uuid: 'new-org',
        status: path.endsWith('/accept') ? 'accepted' : 'declined',
      });
    }
    return Response.json({ data: pending });
  }) as typeof fetch;
  const { router, switchOrganization, refresh } = mount('/invites');
  await screen.findByText('管理员邀请你加入 另一组织');
  fireEvent.click(screen.getAllByRole('button', { name: '拒绝', exact: true })[1]);
  await waitFor(() => expect(pending.map((item) => item.id)).toEqual(['invite-first']));
  await waitFor(() => expect(Boolean(screen.queryByText('管理员邀请你加入 另一组织'))).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '接受', exact: true }));
  await screen.findByText('已加入 受邀组织');
  expect(router.state.location.pathname).toBe('/invites');
  expect(switchOrganization).not.toHaveBeenCalled();
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(2));
  fireEvent.click(screen.getByRole('button', { name: '进入组织' }));
  await screen.findByText('控制台首页');
  expect(switchOrganization).toHaveBeenCalledWith('new-org');
});

test('拒绝最后邀请呈现空态；不接受外部或自循环返回地址', async () => {
  let declined = false;
  globalThis.fetch = mock(async (input) => {
    if (String(input).endsWith('/decline')) {
      declined = true;
      return Response.json({ id: invitation.id });
    }
    return Response.json({ data: declined ? [] : [invitation] });
  }) as typeof fetch;
  mount('/invites?returnTo=https://evil.example');
  fireEvent.click(await screen.findByRole('button', { name: '拒绝', exact: true }));
  await screen.findByText('没有待处理邀请。');
  fireEvent.click(screen.getByRole('button', { name: '返回控制台', exact: true }));
  await screen.findByText('控制台首页');
  expect(invitationReturnTo('/invites?returnTo=/invites')).toBe('/');
});
