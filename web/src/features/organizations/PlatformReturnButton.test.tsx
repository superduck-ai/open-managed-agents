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
import { resetTestDom } from '../../test/setup';

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
  theme = 'light',
  dirty = false,
  exitResult = true,
}: {
  target?: string;
  permissions?: string[];
  locale?: Locale;
  theme?: string;
  dirty?: boolean;
  exitResult?: boolean;
} = {}) {
  resetTestDom('https://oma.duck.ai/');
  Object.assign(globalThis, { scrollTo: () => {} });
  globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
  document.documentElement.classList.toggle('dark', theme === 'dark');
  const account = { uuid: 'account', display_name: '用户名称', email_address: 'a@example.com', memberships: [] };
  const platformScope = target === undefined ? undefined : { accountUuid: 'account', targetOrganizationUuid: target };
  const exit = mock(async () => {
    if (platformScope && exitResult) platformScope.targetOrganizationUuid = '';
    return exitResult;
  });
  const onNavigate = mock(async (path: string) => {
    expect(path).toBe('/platform-admin');
    await router.navigate({ to: '/platform-admin' });
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

test('退出失败不导航且保留返回入口', async () => {
  const { router, exit } = mount({ target: 'target', exitResult: false });
  fireEvent.click(await screen.findByRole('button', { name: '返回平台管理' }));
  await waitFor(() => expect(exit).toHaveBeenCalledTimes(1));
  expect(router.state.location.pathname).toBe('/');
  expect(screen.getByRole('button', { name: '返回平台管理' })).toBeTruthy();
});

test('未保存内容先确认，取消保持当前组织，确认后才退出', async () => {
  const { router, exit } = mount({ target: 'target', dirty: true });
  fireEvent.click(await screen.findByRole('button', { name: '返回平台管理' }));
  expect(await screen.findByRole('alertdialog')).toBeTruthy();
  expect(exit).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', { name: '继续编辑' }));
  expect(router.state.location.pathname).toBe('/');
  fireEvent.click(screen.getByRole('button', { name: '返回平台管理' }));
  fireEvent.click(screen.getByRole('button', { name: '放弃更改并切换' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/platform-admin/organizations/target'));
  expect(exit).toHaveBeenCalledTimes(1);
});

for (const permissions of [[], ['platform:access']]) {
  test(`未进入时无返回按钮，平台菜单按权限显示：${permissions.length}`, async () => {
    mount({ permissions });
    const menu = await openAccountMenu();
    expect(screen.queryByRole('button', { name: '返回平台管理' })).toBeNull();
    expect(within(menu).queryByRole('menuitem', { name: '平台管理', exact: true }) !== null).toBe(
      permissions.length > 0,
    );
    expect(within(menu).queryByText('平台管理员')).toBeNull();
    expect(within(menu).queryByText('返回平台管理')).toBeNull();
  });
}

for (const locale of ['zh-CN', 'en'] as const) {
  test(`账号菜单只保留直接打开平台后台的一级入口：${locale}`, async () => {
    const { router, exit, onNavigate } = mount({ locale, target: 'target' });
    const menu = await openAccountMenu();
    const parent = within(menu).getByRole('menuitem', {
      name: locale === 'zh-CN' ? '平台管理' : 'Platform administration',
      exact: true,
    });
    expect(parent.getAttribute('aria-haspopup')).toBeNull();
    expect(within(menu).queryByText(/平台管理员|Platform administrator/)).toBeNull();
    expect(within(menu).queryByText(/返回平台管理|Return to platform administration/)).toBeNull();
    fireEvent.click(parent);
    await waitFor(() => expect(router.state.location.pathname).toBe('/platform-admin'));
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(exit).not.toHaveBeenCalled();
  });
}

for (const target of ['', 'target']) {
  for (const theme of ['light', 'dark']) {
    for (const locale of ['zh-CN', 'en'] as const) {
      test(`返回按钮位于账号区上方且导航退出前目标：${target || '无'}，${locale}，${theme}`, async () => {
        const { router, exit } = mount({ target, theme, locale });
        const button = await screen.findByRole('button', {
          name: locale === 'zh-CN' ? '返回平台管理' : 'Return to platform administration',
        });
        const footer = button.closest('[data-slot="sidebar-footer"]')!;
        const account = within(footer as HTMLElement).getByRole('button', { name: /^用户名称/ });
        expect(button.parentElement).toBe(footer);
        expect(button.compareDocumentPosition(account) & Node.DOCUMENT_POSITION_FOLLOWING).not.toBe(0);
        expect(button.classList.contains('w-full')).toBe(true);
        expect(button.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
        expect(button.querySelector('span')?.classList.contains('truncate')).toBe(true);
        fireEvent.click(button);
        await waitFor(() =>
          expect(router.state.location.pathname).toBe(
            target ? `/platform-admin/organizations/${target}` : '/platform-admin',
          ),
        );
        expect(exit).toHaveBeenCalledTimes(1);
      });
    }
  }
}
