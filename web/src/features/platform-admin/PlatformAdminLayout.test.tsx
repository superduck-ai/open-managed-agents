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
import { SuperAdministratorsPage } from './SuperAdministratorsPage';
import { PlatformAdminLayout } from './PlatformAdminLayout';
import { OrganizationDetailPage } from './OrganizationDetailPage';
import { organizationSection, type OrganizationSection } from './organizationNavigation';

const { act, cleanup, screen, fireEvent, waitFor } = await import('@testing-library/react');
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
  const administrators = createRoute({
    getParentRoute: () => admin,
    path: 'administrators',
    component: SuperAdministratorsPage,
  });
  const login = createRoute({ getParentRoute: () => root, path: '/login', component: () => <h1>登录页面</h1> });
  const router = createRouter({
    routeTree: root.addChildren([admin.addChildren([index, detail, administrators]), login]),
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
  globalThis.fetch = mock(async () =>
    Response.json({ error: { code: 'platform_access_denied' } }, { status: 403 }),
  ) as typeof fetch;
  const router = mount();
  expect((await screen.findByRole('alert')).textContent).toContain('当前账号无后台权限');
  expect(screen.queryByText('受保护组织目录')).toBeNull();
  expect(router.state.location.pathname).toBe('/platform-admin');
});

test('后台会话失效时跳转登录并保留后台返回地址', async () => {
  globalThis.fetch = mock(async () =>
    Response.json({ error: { code: 'unauthenticated' } }, { status: 401 }),
  ) as typeof fetch;
  const router = mount('/platform-admin/organizations/target?view=invitations');
  expect(await screen.findByText('登录页面')).toBeTruthy();
  expect(screen.queryByText('受保护组织目录')).toBeNull();
  expect(router.state.location.search).toEqual({ returnTo: '/platform-admin/organizations/target?view=invitations' });
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
  expect(screen.getAllByRole('main')).toHaveLength(1);
  expect(screen.getByRole('main').parentElement?.closest('main')).toBeNull();
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
  expect(await screen.findByText('暂无组织成员。')).toBeTruthy();
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.getByRole('link', { name: '组织成员' }).getAttribute('aria-current')).toBe('page');
  expect(requests.some((path) => path.includes('/workspaces') || path.includes('/invitations'))).toBe(false);

  fireEvent.click(screen.getByRole('link', { name: '工作区' }));
  expect(await screen.findByRole('heading', { name: '工作区目录' })).toBeTruthy();
  expect(await screen.findByText('暂无工作区。')).toBeTruthy();
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.queryByRole('heading', { name: '组织成员' })).toBeNull();
  expect(screen.getByRole('link', { name: '工作区' }).getAttribute('aria-current')).toBe('page');
  expect(router.state.location.search).toEqual({ view: 'workspaces' });

  fireEvent.click(screen.getByRole('link', { name: '成员邀请' }));
  expect(await screen.findByRole('heading', { name: '成员邀请' })).toBeTruthy();
  expect(await screen.findByText('暂无成员邀请。')).toBeTruthy();
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.queryByRole('heading', { name: '工作区目录' })).toBeNull();

  router.history.back();
  expect(await screen.findByRole('heading', { name: '工作区目录' })).toBeTruthy();
  fireEvent.click(screen.getByRole('link', { name: '基础资料' }));
  expect(await screen.findByRole('button', { name: '编辑资料' })).toBeTruthy();
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.getByRole('link', { name: '基础资料' }).getAttribute('aria-current')).toBe('page');
});

for (const [locale, label] of [
  ['en', 'Organizations'],
  ['zh-CN', '组织管理'],
] as const) {
  test(`后台沿用 ${locale} 语言偏好且不提供临时切换入口`, async () => {
    globalThis.fetch = mock(async () =>
      Response.json({
        account: { uuid: 'operator', email: 'operator@example.com', name: '' },
        platform_access: { role: 'super_admin', permissions: ['platform:access'] },
        csrf_token: 'verified-csrf',
      }),
    ) as typeof fetch;
    mount('/platform-admin', locale);
    expect(await screen.findByRole('link', { name: label })).toBeTruthy();
    expect(screen.queryByRole('combobox', { name: /Language|语言/ })).toBeNull();
    expect(document.documentElement.lang).toBe(locale);
  });
}

for (const [locale, label] of [
  ['zh-CN', '默认'],
  ['en', 'Default'],
] as const) {
  test(`${locale} 默认工作区状态本地化`, async () => {
    globalThis.fetch = mock(async (url: RequestInfo | URL) => {
      if (String(url).endsWith('/bootstrap'))
        return Response.json({
          account: { uuid: 'operator', email: 'admin@example.com', name: '' },
          platform_access: { permissions: ['platform:access'], role: 'super_admin' },
          csrf_token: 'csrf',
        });
      if (String(url).includes('/workspaces?'))
        return Response.json({
          data: [{ uuid: 'workspace', id: 'wrkspc_default', name: 'Workspace', is_default: true, archived: false }],
          next_cursor: '',
        });
      return Response.json({
        uuid: 'target',
        name: 'Organization',
        version: 1,
        admin_count: 1,
        member_count: 1,
        workspace_count: 1,
      });
    }) as typeof fetch;
    mount('/platform-admin/organizations/target?view=workspaces', locale);
    expect(await screen.findByRole('cell', { name: label, exact: true })).toBeTruthy();
  });
}

for (const view of ['members', 'invitations'] as const) {
  test(`${view} 版本冲突刷新后重开使用新版本`, async () => {
    let version = 1;
    let detailReads = 0;
    let directoryReads = 0;
    const writes: number[] = [];
    globalThis.fetch = mock(async (url: RequestInfo | URL, options?: RequestInit) => {
      const path = String(url);
      if (path.endsWith('/bootstrap'))
        return Response.json({
          account: { uuid: 'operator', email: 'admin@example.com', name: '' },
          platform_access: { permissions: ['platform:access'], role: 'super_admin' },
          csrf_token: 'csrf',
        });
      if (options?.method === 'POST') {
        writes.push(JSON.parse(String(options.body)).expected_version);
        if (writes.length === 1) {
          version = 2;
          return Response.json({ error: { code: 'version_conflict' } }, { status: 409 });
        }
        return Response.json({
          uuid: 'target',
          name: '组织',
          version: 3,
          admin_count: 2,
          member_count: 2,
          workspace_count: 1,
        });
      }
      if (path.endsWith('/organizations/target')) {
        detailReads++;
        return Response.json({
          uuid: 'target',
          name: '组织',
          version,
          admin_count: 1,
          member_count: 1,
          workspace_count: 1,
        });
      }
      if (path.includes('/accounts?'))
        return Response.json({ data: [{ uuid: 'account', email: 'new@example.com', name: 'New' }], next_cursor: '' });
      directoryReads++;
      return Response.json({ data: [], next_cursor: '' });
    }) as typeof fetch;
    mount(`/platform-admin/organizations/target?view=${view}`);
    const openLabel = view === 'members' ? '设置管理员' : '发送邀请';
    const submitLabel = view === 'members' ? '确认变更' : '确认';
    async function submit() {
      fireEvent.click(await screen.findByRole('button', { name: openLabel, exact: true }));
      if (view === 'members') {
        fireEvent.change(screen.getByLabelText('搜索已注册账号'), { target: { value: 'new' } });
        fireEvent.click(screen.getByRole('button', { name: '查找' }));
        fireEvent.click(await screen.findByRole('checkbox'));
      } else fireEvent.change(screen.getByLabelText('受邀邮箱'), { target: { value: 'invited@example.com' } });
      fireEvent.click(screen.getByRole('button', { name: submitLabel, exact: true }));
    }
    await submit();
    expect((await screen.findByRole('alert')).textContent).toContain('关闭并重新打开');
    expect(detailReads).toBeGreaterThanOrEqual(2);
    expect(directoryReads).toBeGreaterThanOrEqual(2);
    if (view === 'invitations')
      expect((screen.getByLabelText('受邀邮箱') as HTMLInputElement).value).toBe('invited@example.com');
    fireEvent.click(screen.getByRole('button', { name: '取消', exact: true }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    await submit();
    await waitFor(() => expect(writes).toEqual([1, 2]));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
}

test('侧栏进入超级管理员目录，撤销自己后重新鉴权并退出后台', async () => {
  let revoked = false;
  globalThis.fetch = mock(async (input, options) => {
    const path = String(input);
    if (options?.method === 'DELETE') {
      expect(path).toBe('/api/platform-admin/administrators/operator');
      revoked = true;
      return Response.json({ granted: false });
    }
    if (revoked) return Response.json({ error: { code: 'platform_access_denied' } }, { status: 403 });
    if (path.endsWith('/bootstrap'))
      return Response.json({
        account: { uuid: 'operator', email: 'operator@example.com', name: '' },
        platform_access: { role: 'super_admin', permissions: ['platform:access'] },
        csrf_token: 'csrf',
      });
    return Response.json({
      data: [{ uuid: 'operator', email: 'operator@example.com', name: '当前账号' }],
      next_cursor: '',
    });
  }) as typeof fetch;
  const router = mount();
  fireEvent.click(await screen.findByRole('link', { name: '超级管理员', exact: true }));
  expect(await screen.findByRole('heading', { name: '超级管理员' })).toBeTruthy();
  expect(router.state.location.pathname).toBe('/platform-admin/administrators');
  expect(screen.getByRole('link', { name: '组织管理' }).getAttribute('aria-current')).toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: '撤销超级管理员' }));
  expect(screen.getByText('你正在撤销自己的平台管理权限，确认后将退出管理后台。')).toBeTruthy();
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '确认变更' }));
  });
  expect((await screen.findByRole('alert')).textContent).toContain('当前账号无后台权限');
  expect(screen.queryByRole('heading', { name: '超级管理员' })).toBeNull();
});
