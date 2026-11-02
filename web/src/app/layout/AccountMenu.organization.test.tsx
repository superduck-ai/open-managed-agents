import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../test/setup';
import { AuthContext, useAuth } from '../../shared/auth/context';
import type { AuthAccount } from '../../shared/auth/api';
import { setConsoleRequestContext } from '../../shared/api/client';
import { I18nProvider } from '../../shared/i18n';
import { SidebarProvider } from '../../shared/ui/sidebar';
import { WorkspaceProvider } from '../../shared/workspaces/WorkspaceProvider';
import { useWorkspace } from '../../shared/workspaces/context';
import { AccountMenu } from './ConsoleLayout';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const account: AuthAccount = {
  uuid: 'account',
  email_address: 'guanpeixian@example.com',
  display_name: 'guanpeixian',
  default_organization_uuid: 'a',
  memberships: [
    { organization: { uuid: 'a', name: '组织 A' }, role: 'admin' },
    { organization: { uuid: 'b', name: '组织 B' }, role: 'user' },
  ],
};
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.localStorage.clear();
  window.sessionStorage.clear();
});
function ScopedMenu({ collapsed }: { collapsed?: boolean }) {
  const auth = useAuth();
  const workspace = useWorkspace();
  return (
    <>
      <output data-testid="scope">{workspace.activeWorkspaceId}</output>
      <SidebarProvider>
        <AccountMenu account={auth.account} collapsed={collapsed} onLogout={() => {}} />
      </SidebarProvider>
    </>
  );
}
function mount(initialAccount = account, collapsed = false) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider initialLocale="zh-CN">
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider
          value={{
            account: initialAccount,
            status: 'authenticated',
            logout: async () => {},
            refresh: async () => ({ account: initialAccount }),
          }}
        >
          <WorkspaceProvider>
            <ScopedMenu collapsed={collapsed} />
          </WorkspaceProvider>
        </AuthContext.Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
}
test('切换组织和刷新恢复后保留用户名，角色工作区副标题与组织菜单保持一致', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  globalThis.fetch = mock(async (input) => {
    const url = String(input);
    if (!url.endsWith('/workspaces')) return Response.json({ data: [] });
    const org = url.split('/').at(-2);
    return Response.json([
      {
        id: `ws-${org}`,
        name: 'Default',
        type: 'workspace',
        is_default: true,
        effective_role: org === 'a' ? 'workspace_admin' : 'workspace_user',
      },
    ]);
  }) as typeof fetch;
  const view = mount();
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('ws-a'));
  fireEvent.click(screen.getByRole('button', { name: 'guanpeixian 管理员 默认' }));
  expect(screen.getByText(account.email_address)).toBeTruthy();
  const trigger = screen.getByRole('button', { name: 'guanpeixian 管理员 默认' });
  const title = trigger.querySelector('.text-sm');
  expect(title?.textContent).toBe('guanpeixian');
  expect(title?.nextElementSibling?.textContent).toBe('管理员默认');
  fireEvent.click(await screen.findByRole('menuitemradio', { name: '组织 B 用户' }));
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('ws-b'));
  expect(trigger.querySelector('.text-sm')?.textContent).toBe('guanpeixian');
  expect(trigger.querySelector('.text-xs')?.textContent).toBe('用户默认');
  expect(screen.getByRole('menuitemradio', { name: '组织 B 用户' }).getAttribute('aria-checked')).toBe('true');
  view.unmount();
  mount();
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('ws-b'));
  expect(screen.getByRole('button', { name: 'guanpeixian 用户 默认' })).toBeTruthy();
});
test('折叠菜单保持账号名称，不依赖当前组织是否命名或存在', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  globalThis.fetch = mock(async () =>
    Response.json([{ id: 'ws-a', name: 'Default', is_default: true }]),
  ) as typeof fetch;
  const view = mount({ ...account, memberships: [{ organization: { uuid: 'a' }, role: 'admin' }] }, true);
  expect(await screen.findByRole('button', { name: 'guanpeixian', exact: true })).toBeTruthy();
  view.unmount();
  mount({ ...account, memberships: [] }, true);
  expect(await screen.findByRole('button', { name: 'guanpeixian', exact: true })).toBeTruthy();
});
