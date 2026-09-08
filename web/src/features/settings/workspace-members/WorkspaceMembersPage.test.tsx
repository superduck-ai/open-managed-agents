import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { AuthContext } from '../../../shared/auth/context';
import { WorkspaceMembersContent } from './WorkspaceMembersPage';
import type { WorkspaceMemberDirectory } from './api';

const { cleanup, fireEvent, render, screen, within } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function renderDirectory(options: Partial<WorkspaceMemberDirectory> = {}, status = 200) {
  resetTestDom('https://oma.duck.ai/settings/workspaces/workspace_one/members');
  const directory: WorkspaceMemberDirectory = {
    workspace_id: 'workspace_one',
    is_default: false,
    can_manage_members: true,
    members: [
      {
        user_id: 'admin',
        name: 'Admin',
        email: 'admin@example.com',
        organization_role: 'admin',
        workspace_role: 'workspace_admin',
        role_source: 'organization',
        can_edit: false,
        can_remove: false,
      },
      {
        user_id: 'member',
        name: 'Member',
        email: 'member@example.com',
        organization_role: 'user',
        workspace_role: 'workspace_user',
        role_source: 'explicit',
        can_edit: true,
        can_remove: true,
      },
    ],
    ...options,
  };
  globalThis.fetch = (async (url) =>
    Response.json(String(url).includes('member-candidates') ? [] : directory, { status })) as typeof fetch;
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <AuthContext.Provider
        value={{
          account: null,
          status: 'authenticated',
          csrfToken: 'csrf',
          refresh: async () => undefined,
          logout: async () => {},
        }}
      >
        <WorkspaceMembersContent orgUuid="org_one" workspaceId="workspace_one" />
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
}

test('无管理权限时不显示管理入口', async () => {
  renderDirectory({
    can_manage_members: false,
    members: [
      {
        user_id: 'member',
        name: 'Member',
        email: 'member@example.com',
        organization_role: 'user',
        workspace_role: 'workspace_user',
        role_source: 'explicit',
        can_edit: false,
        can_remove: false,
      },
    ],
  });
  await screen.findByText('member@example.com');
  expect(screen.queryByRole('button', { name: 'Add to Workspace' }) === null).toBe(true);
  expect(screen.queryByRole('combobox', { name: 'Role for Member' }) === null).toBe(true);
});

test('Default 只展示组织继承说明', async () => {
  renderDirectory({ is_default: true });
  expect(
    await screen.findByText('Default Workspace members and permissions are inherited from organization roles.'),
  ).toBeTruthy();
  expect(screen.getByRole('link', { name: 'View organization members' }).getAttribute('href')).toBe(
    '/settings/members',
  );
  expect(screen.queryByRole('button', { name: 'Add to Workspace' }) === null).toBe(true);
  expect(screen.queryByRole('table') === null).toBe(true);
});

test('继承管理员不可编辑，显式成员支持角色与移除', async () => {
  renderDirectory();
  await screen.findByText('member@example.com');
  expect(screen.queryByRole('combobox', { name: 'Role for Admin' }) === null).toBe(true);
  expect(screen.getByRole('combobox', { name: 'Role for Member' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'More actions for Admin' }) === null).toBe(true);
  expect(screen.getByRole('button', { name: 'More actions for Member' })).toBeTruthy();
});

test('按姓名和邮箱搜索成员', async () => {
  renderDirectory();
  await screen.findByText('member@example.com');
  fireEvent.change(screen.getByRole('textbox', { name: 'Search members' }), { target: { value: 'admin@' } });
  const table = screen.getByRole('table');
  expect(within(table).queryByText('member@example.com') === null).toBe(true);
  expect(within(table).getByText('admin@example.com')).toBeTruthy();
});

test('无可添加组织成员时禁用提交', async () => {
  renderDirectory();
  fireEvent.click(await screen.findByRole('button', { name: 'Add to Workspace' }));
  await screen.findByText('No eligible organization members found.');
  expect((screen.getByRole('button', { name: 'Add member' }) as HTMLButtonElement).disabled).toBe(true);
});
