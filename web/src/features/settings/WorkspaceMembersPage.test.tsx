import '../../test/setup';
import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '../../shared/i18n';
import { AuthContext } from '../../shared/auth/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { WorkspaceMembersPage } from './WorkspaceMembersPage';
import type { WorkspaceMemberDirectory } from './workspace-members/api';

const { render, screen, cleanup, waitFor, within, fireEvent } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function renderMembers(workspace = defaultWorkspace) {
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
  return render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="en">
        <AuthContext.Provider
          value={{
            account: null,
            status: 'authenticated',
            csrfToken: 'csrf-test',
            refresh: async () => undefined,
            logout: async () => undefined,
          }}
        >
          <WorkspaceContext.Provider value={value}>
            <WorkspaceMembersPage />
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

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

test('普通空间权限拒绝时不回退显示组织成员', async () => {
  globalThis.fetch = mock(
    async () => new Response(JSON.stringify({ error: { message: 'Access denied' } }), { status: 403 }),
  ) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_denied', external_id: 'wrkspc_denied', is_default: false });
  await waitFor(() => expect(screen.getByRole('alert').textContent).toContain('Access denied'));
  expect(screen.queryByRole('table')).toBeNull();
});

test('Default 显示组织管理提示与跳转，不查询或修改成员', () => {
  const fetchMock = mock(async () => new Response('{}'));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_real_default' });
  expect(screen.getByText('Members for the default workspace are managed at the organization level.')).toBeTruthy();
  expect(screen.getByRole('link', { name: 'Go to organization settings' }).getAttribute('href')).toBe(
    '/settings/members',
  );
  expect(screen.queryByRole('table')).toBeNull();
  expect(screen.queryByRole('button', { name: 'Add to Workspace' })).toBeNull();
  expect(fetchMock).not.toHaveBeenCalled();
});

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
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', external_id: 'wrkspc_other', is_default: false });
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
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', external_id: 'wrkspc_other', is_default: false });
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
  expect((screen.getByRole('button', { name: 'Add member' }) as HTMLButtonElement).disabled).toBe(true);
});
