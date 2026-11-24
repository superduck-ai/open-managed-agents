import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, mock, test } from 'bun:test';
import { OrganizationContext } from '../../shared/organizations/context';
import { AuthContext } from '../../shared/auth/context';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { useScopeUnsavedChanges } from '../../shared/organizations/unsaved';
import { ConsoleShell } from '../../app/layout/ConsoleLayout';
import { resetTestDom, setTestViewport } from '../../test/setup';

const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function mount({
  target,
  permissions = ['platform:access'],
  locale = 'zh-CN',
  dirty = false,
  exitResult = true,
}: {
  target?: string;
  permissions?: string[];
  locale?: Locale;
  dirty?: boolean;
  exitResult?: boolean;
} = {}) {
  resetTestDom('https://oma.duck.ai/');
  setTestViewport(1280, 900);
  Object.assign(globalThis, { scrollTo: () => {} });
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  const account = { uuid: 'account', display_name: '用户名称', email_address: 'a@example.com', memberships: [] };
  const platformScope = target === undefined ? undefined : { accountUuid: 'account', targetOrganizationUuid: target };
  const exit = mock(async () => {
    if (platformScope && exitResult) platformScope.targetOrganizationUuid = '';
    return exitResult;
  });
  const onNavigate = mock(async (path: string) => {
    await router.navigate({ href: path });
  });
  const root = createRootRoute();
  const home = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => {
      useScopeUnsavedChanges(dirty);
      return (
        <ConsoleShell account={account} onLogout={() => {}} onNavigate={onNavigate}>
          <p>控制台</p>
        </ConsoleShell>
      );
    },
  });
  const router = createRouter({
    routeTree: root.addChildren([
      home,
      createRoute({ getParentRoute: () => root, path: '/platform-admin', component: () => <h1>平台后台</h1> }),
      createRoute({
        getParentRoute: () => root,
        path: '/platform-admin/organizations/$organizationUuid',
        component: () => <h1>组织详情</h1>,
      }),
    ]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  });
  render(
    <I18nProvider initialLocale={locale}>
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <AuthContext.Provider
          value={{
            account,
            status: 'authenticated',
            refresh: async () => undefined,
            logout: async () => {},
            platformAccess: { role: null, permissions },
            organizationAccess:
              target === undefined
                ? undefined
                : {
                    source: 'platform_admin',
                    organization: { uuid: 'target', name: '目标组织' },
                    organization_role: 'admin',
                    workspace_role: 'workspace_admin',
                  },
          }}
        >
          <OrganizationContext.Provider
            value={{
              platformScope,
              memberships: [],
              switching: false,
              error: null,
              switchOrganization: async () => false,
              retry: async () => false,
              exitOrganization: exit,
            }}
          >
            <RouterProvider router={router} />
          </OrganizationContext.Provider>
        </AuthContext.Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { router, exit, onNavigate };
}

async function openAccountMenu() {
  const trigger = await screen.findByRole('button', { name: /^用户名称/ });
  fireEvent.click(trigger);
  return screen.findByRole('menu');
}

async function selectAdmin() {
  const menu = await screen.findByRole('menu');
  fireEvent.click(within(menu).getByRole('menuitem', { name: '超管后台', exact: true }));
}

test('退出失败保留当前页与菜单入口', async () => {
  const { router, exit } = mount({ target: 'target', exitResult: false });
  await openAccountMenu();
  await selectAdmin();
  await waitFor(() => expect(exit).toHaveBeenCalledTimes(1));
  expect(router.state.location.pathname).toBe('/');
  expect(screen.getByRole('menuitem', { name: '超管后台', exact: true })).toBeTruthy();
});

test('未保存内容先确认，取消保留当前组织，确认后才退出', async () => {
  const { router, exit } = mount({ target: 'target', dirty: true });
  await openAccountMenu();
  await selectAdmin();
  expect(await screen.findByRole('alertdialog')).toBeTruthy();
  expect(exit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '继续编辑' }));
  expect(router.state.location.pathname).toBe('/');
  if (!screen.queryByRole('menu')) await openAccountMenu();
  await selectAdmin();
  fireEvent.click(screen.getByRole('button', { name: '放弃更改并切换' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/platform-admin/organizations/target'));
  expect(exit).toHaveBeenCalledTimes(1);
});

test('普通成员没有超管入口', async () => {
  mount({ permissions: [] });
  const menu = await openAccountMenu();
  expect(within(menu).queryByRole('menuitem', { name: '超管后台' })).toBeNull();
  expect(screen.queryByRole('button', { name: '返回超管后台' })).toBeNull();
});

for (const target of [undefined, '', 'target']) {
  for (const locale of ['zh-CN', 'en'] as const) {
    test(`账号菜单的超管入口导航到正确位置 ${String(target)} ${locale}`, async () => {
      const { router, exit } = mount({ target, locale });
      const menu = await openAccountMenu();
      expect(screen.queryByRole('button', { name: /返回超管后台|Return to super admin console/ })).toBeNull();
      const item = within(menu).getByRole('menuitem', {
        name: locale === 'zh-CN' ? '超管后台' : 'Super admin console',
        exact: true,
      });
      fireEvent.click(item);
      await waitFor(() =>
        expect(router.state.location.pathname).toBe(
          target ? '/platform-admin/organizations/target' : '/platform-admin',
        ),
      );
      expect(exit).toHaveBeenCalledTimes(target === undefined ? 0 : 1);
    });
  }
}
