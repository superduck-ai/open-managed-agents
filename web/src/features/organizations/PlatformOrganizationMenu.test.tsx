import { afterEach, expect, mock, test } from 'bun:test';
import { OrganizationContext } from '../../shared/organizations/context';
import { AuthContext } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../../shared/ui/dropdown-menu';
import { PlatformOrganizationMenu } from './PlatformOrganizationMenu';
import { resetTestDom } from '../../test/setup';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');
afterEach(cleanup);
test('没有标签进入状态时不显示平台进入菜单', () => {
  const view = render(<PlatformOrganizationMenu />);
  expect(view.container.textContent).toBe('');
});

for (const hasReturn of [false, true]) {
  test(`用户菜单显示平台来源并退出，原组织存在：${hasReturn}`, async () => {
    resetTestDom('https://oma.duck.ai/');
    const exit = mock(async () => true);
    render(
      <I18nProvider initialLocale="zh-CN">
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
              platformScope: {
                accountUuid: 'account',
                targetOrganizationUuid: 'target',
                returnOrganizationUuid: hasReturn ? 'home' : undefined,
              },
              memberships: [],
              switching: false,
              error: null,
              switchOrganization: async () => false,
              retry: async () => false,
              exitOrganization: exit,
            }}
          >
            <DropdownMenu>
              <DropdownMenuTrigger>账号</DropdownMenuTrigger>
              <DropdownMenuContent>
                <PlatformOrganizationMenu />
              </DropdownMenuContent>
            </DropdownMenu>
          </OrganizationContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: '账号' }));
    expect(await screen.findByText('平台管理员进入 · 目标组织')).toBeTruthy();
    fireEvent.click(screen.getByRole('menuitem', { name: hasReturn ? '退出并返回原组织' : '退出组织工作台' }));
    expect(exit).toHaveBeenCalledTimes(1);
  });
}
