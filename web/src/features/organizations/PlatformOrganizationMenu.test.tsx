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
import { I18nProvider } from '../../shared/i18n';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../../shared/ui/dropdown-menu';
import { PlatformOrganizationMenu } from './PlatformOrganizationMenu';
import { resetTestDom } from '../../test/setup';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);
test('没有标签进入状态时不显示平台进入菜单', () => {
  const view = render(<PlatformOrganizationMenu />);
  expect(view.container.textContent).toBe('');
});

for (const target of ['', 'target']) {
  for (const locale of ['zh-CN', 'en'] as const) {
    test(`返回平台管理使用退出前目标且无目标回退：${target || '无'}，${locale}`, async () => {
      resetTestDom('https://oma.duck.ai/');
      Object.assign(globalThis, { scrollTo: () => {} });
      const platformScope = { accountUuid: 'account', targetOrganizationUuid: target };
      const exit = mock(async () => {
        platformScope.targetOrganizationUuid = '';
        return true;
      });
      const root = createRootRoute();
      const menu = createRoute({
        getParentRoute: () => root,
        path: '/',
        component: () => (
          <DropdownMenu>
            <DropdownMenuTrigger>账号</DropdownMenuTrigger>
            <DropdownMenuContent>
              <PlatformOrganizationMenu />
            </DropdownMenuContent>
          </DropdownMenu>
        ),
      });
      const router = createRouter({
        routeTree: root.addChildren([
          menu,
          createRoute({ getParentRoute: () => root, path: '/platform-admin', component: () => <h1>平台管理</h1> }),
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
              organizationAccess: {
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
      fireEvent.click(await screen.findByRole('button', { name: '账号' }));
      expect(
        await screen.findByText(locale === 'zh-CN' ? '平台管理员进入 · 目标组织' : 'Platform administrator · 目标组织'),
      ).toBeTruthy();
      fireEvent.click(
        screen.getByRole('menuitem', {
          name: locale === 'zh-CN' ? '返回平台管理' : 'Return to platform administration',
        }),
      );
      await waitFor(() =>
        expect(router.state.location.pathname).toBe(
          target ? `/platform-admin/organizations/${target}` : '/platform-admin',
        ),
      );
      expect(exit).toHaveBeenCalledTimes(1);
    });
  }
}
