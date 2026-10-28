import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { afterEach, expect, mock, test } from 'bun:test';
import { OrganizationContext } from '../../shared/organizations/context';
import { AuthContext } from '../../shared/auth/context';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../../shared/ui/dropdown-menu';
import { PlatformAdminMenuItem } from '../platform-admin/PlatformAdminMenuItem';
import { resetTestDom } from '../../test/setup';

const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);

function mount({
  target,
  permissions = ['platform:access'],
  locale = 'zh-CN',
  theme = 'light',
}: { target?: string; permissions?: string[]; locale?: Locale; theme?: string } = {}) {
  resetTestDom('https://oma.duck.ai/');
  Object.assign(globalThis, { scrollTo: () => {} });
  document.documentElement.classList.toggle('dark', theme === 'dark');
  const platformScope = target === undefined ? undefined : { accountUuid: 'account', targetOrganizationUuid: target };
  const exit = mock(async () => {
    if (platformScope) platformScope.targetOrganizationUuid = '';
    return true;
  });
  const onNavigate = mock(async (path: string) => {
    expect(path).toBe('/platform-admin');
    await router.navigate({ to: '/platform-admin' });
  });
  const root = createRootRoute();
  const menu = createRoute({
    getParentRoute: () => root,
    path: '/',
    component: () => (
      <DropdownMenu>
        <DropdownMenuTrigger>账号</DropdownMenuTrigger>
        <DropdownMenuContent>
          <PlatformAdminMenuItem onNavigate={onNavigate} />
        </DropdownMenuContent>
      </DropdownMenu>
    ),
  });
  const router = createRouter({
    routeTree: root.addChildren([
      menu,
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
      <AuthContext.Provider
        value={{
          account: { uuid: 'account', email_address: 'a@example.com', memberships: [] },
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
    </I18nProvider>,
  );
  return { router, exit, onNavigate };
}

async function openAccountMenu() {
  const trigger = await screen.findByRole('button', { name: '账号' });
  trigger.focus();
  fireEvent.keyDown(trigger, { key: 'ArrowDown', code: 'ArrowDown' });
  return trigger;
}

async function openPlatformMenu(locale: Locale = 'zh-CN') {
  const parent = await screen.findByRole('menuitem', {
    name: locale === 'zh-CN' ? '平台管理' : 'Platform administration',
    exact: true,
  });
  expect(parent.getAttribute('aria-haspopup')).toBe('menu');
  expect(parent.querySelector('svg')?.classList.contains('lucide-shield')).toBe(true);
  fireEvent.keyDown(parent, { key: 'ArrowRight', code: 'ArrowRight' });
  const open = await screen.findByRole('menuitem', {
    name: locale === 'zh-CN' ? '打开平台管理' : 'Open platform administration',
    exact: true,
  });
  await waitFor(() => expect(document.activeElement).toBe(open));
  return { parent, open };
}

for (const target of [undefined, 'target']) {
  test(`无平台访问权限不显示一级入口或子菜单，进入状态：${Boolean(target)}`, async () => {
    mount({ permissions: [], target });
    await openAccountMenu();
    expect(screen.queryByRole('menuitem')).toBeNull();
    expect(screen.queryByText(/平台管理员进入/)).toBeNull();
  });
}

for (const locale of ['zh-CN', 'en'] as const) {
  test(`未进入组织仅显示打开后台，父项不导航且子项打开后台：${locale}`, async () => {
    const { router, exit, onNavigate } = mount({ locale });
    await openAccountMenu();
    const { open } = await openPlatformMenu(locale);
    expect(router.state.location.pathname).toBe('/');
    expect(onNavigate).not.toHaveBeenCalled();
    const submenu = open.closest('[role="menu"]')!;
    expect(within(submenu as HTMLElement).getAllByRole('menuitem')).toHaveLength(1);
    expect(submenu.querySelector('[data-slot="dropdown-menu-label"]')).toBeNull();
    expect(submenu.querySelector('[role="separator"]')).toBeNull();
    fireEvent.keyDown(open, { key: 'Enter', code: 'Enter' });
    await waitFor(() => expect(router.state.location.pathname).toBe('/platform-admin'));
    expect(onNavigate).toHaveBeenCalledTimes(1);
    expect(exit).not.toHaveBeenCalled();
  });
}

for (const target of ['', 'target']) {
  for (const theme of ['light', 'dark']) {
    for (const locale of ['zh-CN', 'en'] as const) {
      test(`二级返回项使用退出前目标且无目标回退：${target || '无'}，${locale}，${theme}`, async () => {
        const { router, exit } = mount({ target, theme, locale });
        const trigger = await openAccountMenu();
        expect(screen.queryByText(/平台管理员进入|Platform administrator/)).toBeNull();
        expect(screen.queryByRole('menuitem', { name: /返回平台管理|Return to platform/ })).toBeNull();
        const { parent, open } = await openPlatformMenu(locale);
        const description = await screen.findByText(
          locale === 'zh-CN' ? '平台管理员进入 · 目标组织' : 'Platform administrator · 目标组织',
        );
        const item = screen.getByRole('menuitem', {
          name: locale === 'zh-CN' ? '返回平台管理' : 'Return to platform administration',
        });
        const submenu = item.closest('[role="menu"]')!;
        expect(submenu).toBe(open.closest('[role="menu"]'));
        expect(submenu).not.toBe(parent.closest('[role="menu"]'));
        expect(submenu.classList.contains('max-w-[calc(100vw-2rem)]')).toBe(true);
        expect(item.classList.contains('h-8')).toBe(true);
        expect(item.classList.contains('gap-3')).toBe(true);
        expect(item.classList.contains('px-3')).toBe(true);
        expect(item.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
        expect(item.querySelector('span')?.className).toBe('min-w-0 flex-1 truncate');
        expect(description.classList.contains('truncate')).toBe(true);
        fireEvent.keyDown(open, { key: 'ArrowDown', code: 'ArrowDown' });
        await waitFor(() => expect(document.activeElement).toBe(item));
        fireEvent.keyDown(item, { key: 'Escape', code: 'Escape' });
        await waitFor(() => expect(document.activeElement).toBe(parent));
        fireEvent.keyDown(parent, { key: 'Escape', code: 'Escape' });
        await waitFor(() => expect(document.activeElement).toBe(trigger));
        fireEvent.keyDown(trigger, { key: 'ArrowDown', code: 'ArrowDown' });
        const reopened = await openPlatformMenu(locale);
        fireEvent.keyDown(reopened.open, { key: 'ArrowDown', code: 'ArrowDown' });
        const returnItem = screen.getByRole('menuitem', {
          name: locale === 'zh-CN' ? '返回平台管理' : 'Return to platform administration',
        });
        await waitFor(() => expect(document.activeElement).toBe(returnItem));
        fireEvent.keyDown(returnItem, { key: 'Enter', code: 'Enter' });
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
