import { afterEach, expect, test } from 'bun:test';
import { AuthContext } from '../../shared/auth/context';
import type { AuthAccount, OrganizationAccess } from '../../shared/auth/api';
import { WorkspaceContext } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { SidebarProvider } from '../../shared/ui/sidebar';
import { I18nProvider } from '../../shared/i18n';
import { resetTestDom } from '../../test/setup';
import { AccountMenu } from './ConsoleLayout';

const { cleanup, fireEvent, render, screen, within } = await import('@testing-library/react');
afterEach(cleanup);
const account: AuthAccount = {
  uuid: 'account',
  email_address: 'email-user@example.com',
  display_name: '显示名称',
  full_name: '完整名称',
  memberships: [{ organization: { uuid: 'current', name: '成员组织' }, role: 'admin' }],
};
function mount(value = account, organizationAccess?: OrganizationAccess) {
  resetTestDom('https://oma.duck.ai/');
  render(
    <I18nProvider initialLocale="zh-CN">
      <AuthContext.Provider
        value={{
          account: value,
          status: 'authenticated',
          organizationAccess,
          logout: async () => {},
          refresh: async () => undefined,
        }}
      >
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'current',
            workspaces: [defaultWorkspace],
            activeWorkspace: defaultWorkspace,
            activeWorkspaceId: defaultWorkspace.id,
            isLoading: false,
            error: null,
            selectWorkspace: () => {},
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          <SidebarProvider>
            <AccountMenu account={value} onLogout={() => {}} />
          </SidebarProvider>
        </WorkspaceContext.Provider>
      </AuthContext.Provider>
    </I18nProvider>,
  );
}

for (const [value, name] of [
  [account, '显示名称'],
  [{ ...account, display_name: undefined }, '完整名称'],
  [{ ...account, display_name: undefined, full_name: undefined }, 'email-user'],
] as const) {
  test(`账号上层保持用户名优先级：${name}`, () => {
    mount(value);
    const trigger = screen.getByRole('button', { name: `${name} 成员组织` });
    expect(trigger.querySelector('.text-sm')?.textContent).toBe(name);
    expect(trigger.querySelector('.text-xs')?.textContent).toBe('成员组织');
    expect(trigger.getAttribute('data-size')).toBe('lg');
    expect(trigger.classList.contains('rounded-xl')).toBe(false);
    expect(trigger.classList.contains('bg-sidebar-accent')).toBe(false);
    expect(trigger.classList.contains('text-sidebar-accent-foreground')).toBe(false);
    expect(trigger.classList.contains('data-[popup-open]:bg-sidebar-accent')).toBe(true);
    expect(trigger.classList.contains('data-[popup-open]:text-sidebar-accent-foreground')).toBe(true);
    expect(within(trigger).queryByText('管理员', { exact: true })).toBeNull();
    expect(trigger.textContent).not.toContain('默认');
    expect(within(trigger).queryByText('平台管理员')).toBeNull();
  });
}

test('下层优先有效组织名，空名称回退当前成员组织名，未命名回退组织 UUID', () => {
  const access: OrganizationAccess = {
    source: 'organization_member',
    organization: { uuid: 'current', name: '有效组织' },
    organization_role: 'admin',
    workspace_role: 'workspace_admin',
  };
  mount(account, access);
  expect(screen.getByRole('button', { name: '显示名称 有效组织' })).toBeTruthy();
  cleanup();
  mount(account, { ...access, organization: { uuid: 'current', name: '' } });
  expect(screen.getByRole('button', { name: '显示名称 成员组织' })).toBeTruthy();
  cleanup();
  mount({ ...account, memberships: [{ organization: { uuid: 'current' }, role: 'user' }] });
  expect(screen.getByRole('button', { name: '显示名称 current' })).toBeTruthy();
});

test('平台进入时显示目标组织名及紧凑平台标识，账号菜单不显示状态或返回项', () => {
  mount(account, {
    source: 'platform_admin',
    organization: { uuid: 'target', name: '很长的目标组织名称' },
    organization_role: 'admin',
    workspace_role: 'workspace_admin',
  });
  const trigger = screen.getByRole('button', { name: '显示名称 很长的目标组织名称 平台管理员' });
  const name = within(trigger).getByText('很长的目标组织名称');
  expect(name.classList.contains('truncate')).toBe(true);
  expect(name.getAttribute('title')).toBe('很长的目标组织名称');
  expect(
    within(trigger).getByText('平台管理员').closest('[data-slot="badge"]')?.classList.contains('max-w-[45%]'),
  ).toBe(true);
  expect(trigger.querySelector('.text-sm')?.textContent).toBe('显示名称');
  expect(trigger.textContent).not.toContain('默认');
  expect(within(trigger).queryByText('管理员', { exact: true })).toBeNull();
  fireEvent.click(trigger);
  const menu = screen.getByRole('menu');
  expect(within(menu).queryByText(/平台管理员|返回平台管理/)).toBeNull();
});
