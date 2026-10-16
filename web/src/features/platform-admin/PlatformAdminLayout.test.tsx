import { afterEach, expect, mock, test } from 'bun:test';
import { StrictMode } from 'react';
import { renderPlatformAdmin as render } from '../../test/platform-admin';
import { resetTestDom } from '../../test/setup';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { AuthContext } from '../../shared/auth/context';
import { PlatformAdminLayout } from './PlatformAdminLayout';
import { OrganizationDetailPage } from './OrganizationDetailPage';
import { organizationSection, type OrganizationSection } from './organizationNavigation';

const { cleanup, screen, fireEvent } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function mount(entry = '/platform-admin', locale: 'en' | 'zh-CN' = 'zh-CN') {
  resetTestDom('https://oma.duck.ai/platform-admin');
  Object.assign(globalThis, { scrollTo: () => {} });
  const root = createRootRoute();
  const admin = createRoute({ getParentRoute: () => root, path: '/platform-admin', component: PlatformAdminLayout });
  const index = createRoute({ getParentRoute: () => admin, path: '/', component: () => <h1>受保护组织目录</h1> });
  const detail = createRoute({
    getParentRoute: () => admin,
    path: 'organizations/$organizationUuid',
    component: OrganizationDetailPage,
    validateSearch: (search: Record<string, unknown>): { view?: OrganizationSection } => ({
      view: organizationSection(search.view),
    }),
  });
  const login = createRoute({ getParentRoute: () => root, path: '/login', component: () => <h1>登录页面</h1> });
  const router = createRouter({
    routeTree: root.addChildren([admin.addChildren([index, detail]), login]),
    history: createMemoryHistory({ initialEntries: [entry] }),
  });
  render(
    <StrictMode>
      <AuthContext.Provider
        value={{
          status: 'authenticated',
          account: { uuid: 'operator', email_address: 'operator@example.com', memberships: [] },
          refresh: async () => undefined,
          logout: async () => {},
        }}
      >
        <RouterProvider router={router} />
      </AuthContext.Provider>
    </StrictMode>,
    locale,
  );
  return router;
}

test('后台拒绝授权时不渲染组织目录，也不跳转登录', async () => {
  globalThis.fetch = mock(async () => Response.json({ error: { code: 'denied' } }, { status: 403 })) as typeof fetch;
  const router = mount();
  expect((await screen.findByRole('alert')).textContent).toContain('当前账号无后台权限');
  expect(screen.queryByText('受保护组织目录')).toBeNull();
  expect(router.state.location.pathname).toBe('/platform-admin');
});

test('后台会话失效时跳转登录并保留后台返回地址', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({ error: { code: 'unauthenticated' } }, { status: 401 }),
  ) as typeof fetch;
  const router = mount();
  expect(await screen.findByText('登录页面')).toBeTruthy();
  expect(screen.queryByText('受保护组织目录')).toBeNull();
  expect(router.state.location.search).toEqual({ returnTo: '/platform-admin' });
});

test('严格模式重挂载后可重试授权故障并显示无组织账号的后台', async () => {
  let failed = true;
  globalThis.fetch = mock(async () =>
    failed
      ? Response.json({ error: { code: 'unavailable' } }, { status: 503 })
      : Response.json({
          account: { uuid: 'operator', email: 'operator@example.com', name: '' },
          platform_access: { role: 'super_admin', permissions: ['platform:access'] },
          csrf_token: 'verified-csrf',
        }),
  ) as typeof fetch;
  mount();
  expect((await screen.findByRole('alert')).textContent).toContain('管理服务暂不可用');
  expect(screen.queryByText('受保护组织目录')).toBeNull();
  failed = false;
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  expect(await screen.findByText('受保护组织目录')).toBeTruthy();
  expect(screen.getByText('operator@example.com')).toBeTruthy();
  expect(screen.getByRole('navigation', { name: '后台导航' })).toBeTruthy();
  expect(screen.getByRole('link', { name: '组织管理' }).getAttribute('aria-current')).toBe('page');
  expect(screen.queryByRole('link', { name: '审计记录' })).toBeNull();
});

test('组织菜单每次只加载对应列表，直达与返回保留当前菜单', async () => {
  const requests: string[] = [];
  globalThis.fetch = mock(async (url: RequestInfo | URL) => {
    const path = String(url);
    requests.push(path);
    if (path.endsWith('/bootstrap'))
      return Response.json({
        account: { uuid: 'operator', email: 'operator@example.com', name: '' },
        platform_access: { role: 'super_admin', permissions: ['platform:access'] },
        csrf_token: 'verified-csrf',
      });
    if (path.endsWith('/organizations/target'))
      return Response.json({
        uuid: 'target',
        name: '目标组织',
        version: 1,
        admin_count: 1,
        member_count: 2,
        workspace_count: 1,
        website: '',
        industry: '',
        created_at: '',
      });
    return Response.json({ data: [], next_cursor: '' });
  }) as unknown as typeof fetch;
  const router = mount('/platform-admin/organizations/target?view=members');
  expect(await screen.findByRole('heading', { name: '组织成员' })).toBeTruthy();
  expect(await screen.findByRole('table')).toBeTruthy();
  expect(screen.getAllByRole('table')).toHaveLength(1);
  expect(screen.getByRole('link', { name: '组织成员' }).getAttribute('aria-current')).toBe('page');
  expect(requests.some((path) => path.includes('/workspaces') || path.includes('/invitations'))).toBe(false);

  fireEvent.click(screen.getByRole('link', { name: '工作区' }));
  expect(await screen.findByRole('heading', { name: '工作区目录' })).toBeTruthy();
  expect(await screen.findByRole('table')).toBeTruthy();
  expect(screen.getAllByRole('table')).toHaveLength(1);
  expect(screen.queryByRole('heading', { name: '组织成员' })).toBeNull();
  expect(screen.getByRole('link', { name: '工作区' }).getAttribute('aria-current')).toBe('page');
  expect(router.state.location.search).toEqual({ view: 'workspaces' });

  fireEvent.click(screen.getByRole('link', { name: '成员邀请' }));
  expect(await screen.findByRole('heading', { name: '成员邀请' })).toBeTruthy();
  expect(await screen.findByRole('table')).toBeTruthy();
  expect(screen.getAllByRole('table')).toHaveLength(1);
  expect(screen.queryByRole('heading', { name: '工作区目录' })).toBeNull();

  router.history.back();
  expect(await screen.findByRole('heading', { name: '工作区目录' })).toBeTruthy();
  fireEvent.click(screen.getByRole('link', { name: '基础资料' }));
  expect(await screen.findByRole('button', { name: '编辑资料' })).toBeTruthy();
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.getByRole('link', { name: '基础资料' }).getAttribute('aria-current')).toBe('page');
});

test('后台英文与中文切换同步菜单及权限提示', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({
      account: { uuid: 'operator', email: 'operator@example.com', name: '' },
      platform_access: { role: 'super_admin', permissions: ['platform:access'] },
      csrf_token: 'verified-csrf',
    }),
  ) as typeof fetch;
  mount('/platform-admin', 'en');
  expect(await screen.findByRole('link', { name: 'Organizations' })).toBeTruthy();
  expect(screen.queryByRole('link', { name: '组织管理' })).toBeNull();
  const language = screen.getByRole('combobox', { name: 'Language' });
  language.focus();
  fireEvent.keyDown(language, { key: 'ArrowDown' });
  fireEvent.keyDown(await screen.findByRole('option', { name: '简体中文' }), { key: 'Enter' });
  expect(await screen.findByRole('link', { name: '组织管理' })).toBeTruthy();
  expect(document.documentElement.lang).toBe('zh-CN');
});
