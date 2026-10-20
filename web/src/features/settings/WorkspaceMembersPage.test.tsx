import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import type { WorkspaceMemberDirectory } from './workspace-members/api';
import { WorkspaceMembersPage } from './WorkspaceMembersPage';

const { render, screen, fireEvent, within, cleanup, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function renderMembers(
  workspace = defaultWorkspace,
  permissions = ['workspace:members:manage'],
  locale: 'en' | 'zh-CN' = 'en',
) {
  resetTestDom('https://oma.duck.ai/members');
  const value: WorkspaceContextValue = {
    orgUuid: 'org_test',
    activeWorkspace: workspace,
    activeWorkspaceId: workspace.id,
    workspaces: [workspace],
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => workspace,
    refreshWorkspaces: async () => undefined,
  };
  const auth: AuthContextValue = {
    account: { uuid: 'account_test', email_address: 'member@example.test', permissions },
    status: 'authenticated',
    csrfToken: 'csrf-test',
    refresh: async () => undefined,
    logout: async () => undefined,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const content = (nextPermissions: string[]) => (
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale={locale}>
        <AuthContext.Provider
          value={{
            ...auth,
            account: { uuid: 'account_test', email_address: 'member@example.test', permissions: nextPermissions },
          }}
        >
          <WorkspaceContext.Provider value={value}>
            <WorkspaceMembersPage />
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    </QueryClientProvider>
  );
  const view = render(content(permissions));
  return { ...view, rerenderPermissions: (next: string[]) => view.rerender(content(next)) };
}

test('普通成员显示中文无权限且不请求成员列表', () => {
  const fetchMock = mock(async () => new Response('{}'));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_denied', is_default: false }, [], 'zh-CN');
  expect(screen.getByText('无权限访问')).toBeTruthy();
  expect(screen.getByText('只有工作区管理员或组织管理员可以查看和管理工作区成员。请联系管理员。')).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.queryByRole('table')).toBeNull();
  expect(fetchMock).not.toHaveBeenCalled();
});

test('管理员上下文遇到403显示无权限而非原始报错', async () => {
  globalThis.fetch = mock(
    async () => new Response(JSON.stringify({ error: { message: 'Action not allowed' } }), { status: 403 }),
  ) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_denied', is_default: false });
  expect(await screen.findByText('Access denied')).toBeTruthy();
  expect(screen.queryByText('Action not allowed')).toBeNull();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.queryByRole('table')).toBeNull();
});

test('真正的服务故障保留错误状态而非无权限', async () => {
  globalThis.fetch = mock(
    async () => new Response(JSON.stringify({ error: { message: 'Service unavailable' } }), { status: 500 }),
  ) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_failed', is_default: false });
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(screen.getByText('Service unavailable')).toBeTruthy();
  expect(screen.queryByText('Access denied')).toBeNull();
  expect(screen.queryByRole('table')).toBeNull();
});

test('当前工作区撤权后立即隐藏已有管理员成员缓存', async () => {
  const fetchMock = mock(
    async () =>
      new Response(
        JSON.stringify(
          directoryResponse({
            members: [
              {
                user_id: 'user_cached',
                name: 'user_cached',
                email: 'cached@example.com',
                organization_role: 'user',
                workspace_role: 'workspace_user',
                role_source: 'membership',
                can_edit: true,
                can_remove: true,
              },
            ],
          }),
        ),
      ),
  );
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  const view = renderMembers({ ...defaultWorkspace, id: 'wrkspc_cached', is_default: false });
  expect(await screen.findByText('user_cached')).toBeTruthy();
  expect(screen.getByRole('table')).toBeTruthy();
  view.rerenderPermissions([]);
  expect(screen.getByText('Access denied')).toBeTruthy();
  expect(screen.queryByText('user_cached')).toBeNull();
  expect(screen.queryByRole('table')).toBeNull();
  expect(fetchMock).toHaveBeenCalledTimes(1);
});

test('Default 显示组织管理提示与跳转，不查询或修改成员', () => {
  const fetchMock = mock(async () => new Response('{}'));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_real_default' }, []);
  expect(screen.getByText('Members for the default workspace are managed at the organization level.')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Go to organization settings' }).getAttribute('href')).toBe(
    '/settings/members',
  );
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.queryByRole('button', { name: /Invite/ })).toBeNull();
  expect(fetchMock).not.toHaveBeenCalled();
});

function directoryResponse(overrides: Partial<WorkspaceMemberDirectory> = {}) {
  return {
    workspace_id: 'wrkspc_other',
    is_default: false,
    can_manage_members: true,
    members: [
      {
        user_id: 'user_admin',
        name: 'Admin',
        email: 'admin@example.com',
        organization_role: 'admin',
        workspace_role: 'workspace_admin',
        role_source: 'organization',
        can_edit: false,
        can_remove: false,
      },
      {
        user_id: 'user_member',
        name: 'Member',
        email: 'member@example.com',
        organization_role: 'user',
        workspace_role: 'workspace_user',
        role_source: 'membership',
        can_edit: true,
        can_remove: true,
      },
    ],
    ...overrides,
  };
}

test('普通空间目录显示姓名邮箱角色，继承管理员不可操作', async () => {
  globalThis.fetch = mock(async (input: string | URL | Request) => {
    const path = String(input);
    if (path.includes('member-candidates')) return new Response(JSON.stringify([]));
    return new Response(JSON.stringify(directoryResponse()));
  }) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', external_id: 'wrkspc_other', is_default: false });
  expect(await screen.findByRole('button', { name: 'Add to Workspace' })).toBeTruthy();
  expect(await screen.findByText('admin@example.com')).toBeTruthy();
  expect(screen.queryByRole('combobox', { name: 'Role for Admin' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'More actions for Admin' })).toBeNull();
  expect(screen.getByRole('combobox', { name: 'Role for Member' })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'More actions for Member' })).toBeTruthy();
});

test('无管理权限时隐藏添加入口与行操作', async () => {
  globalThis.fetch = mock(async (input: string | URL | Request) => {
    const path = String(input);
    if (path.includes('member-candidates')) return new Response(JSON.stringify([]));
    return new Response(
      JSON.stringify(
        directoryResponse({
          can_manage_members: false,
          members: [
            {
              user_id: 'user_member',
              name: 'Member',
              email: 'member@example.com',
              organization_role: 'user',
              workspace_role: 'workspace_user',
              role_source: 'membership',
              can_edit: false,
              can_remove: false,
            },
          ],
        }),
      ),
    );
  }) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', external_id: 'wrkspc_other', is_default: false }, [
    'workspace:members:view',
  ]);
  await screen.findByText('member@example.com');
  expect(screen.queryByRole('button', { name: 'Add to Workspace' })).toBeNull();
  expect(screen.queryByRole('combobox', { name: 'Role for Member' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'More actions for Member' })).toBeNull();
});

test('按姓名和邮箱搜索成员', async () => {
  globalThis.fetch = mock(async (input: string | URL | Request) => {
    const path = String(input);
    if (path.includes('member-candidates')) return new Response(JSON.stringify([]));
    return new Response(JSON.stringify(directoryResponse()));
  }) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', external_id: 'wrkspc_other', is_default: false }, [
    'workspace:members:view',
  ]);
  await screen.findByText('member@example.com');
  fireEvent.change(screen.getByRole('textbox', { name: 'Search members' }), { target: { value: 'admin@' } });
  const table = screen.getByRole('table');
  expect(within(table).queryByText('member@example.com')).toBeNull();
  expect(within(table).getByText('admin@example.com')).toBeTruthy();
});

test('无可添加组织成员时禁用提交', async () => {
  globalThis.fetch = mock(async (input: string | URL | Request) => {
    const path = String(input);
    if (path.includes('member-candidates')) return new Response(JSON.stringify([]));
    return new Response(JSON.stringify(directoryResponse()));
  }) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', external_id: 'wrkspc_other', is_default: false });
  const addButton = await screen.findByRole('button', { name: 'Add to Workspace' });
  fireEvent.click(addButton);
  await screen.findByText('No eligible organization members found.');
  expect((screen.getAllByRole('button', { name: 'Add to Workspace' }).at(-1) as HTMLButtonElement).disabled).toBe(true);
});

test('移除确认明确显示目标工作区名称', async () => {
  globalThis.fetch = mock(async () => Response.json(directoryResponse())) as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', name: 'Project Alpha', is_default: false });
  fireEvent.click(await screen.findByRole('button', { name: 'More actions for Member' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove member' }));
  expect(
    await screen.findByText('Are you sure you want to remove member@example.com from workspace Project Alpha?'),
  ).toBeTruthy();
});
