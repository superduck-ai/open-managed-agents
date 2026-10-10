import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { WorkspaceMembersPage } from './WorkspaceMembersPage';

const { render, screen, cleanup, waitFor } = await import('@testing-library/react');
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
    async () => new Response(JSON.stringify({ data: [{ user_id: 'user_cached', workspace_role: 'workspace_user' }] })),
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

test('普通空间按真实 ID 请求全部分页，不请求组织成员', async () => {
  const paths: string[] = [];
  globalThis.fetch = mock(async (input: string | URL | Request) => {
    paths.push(String(input));
    const next = paths.length === 1;
    return new Response(
      JSON.stringify({
        data: [
          {
            user_id: next ? 'user_admin' : 'user_billing',
            workspace_role: next ? 'workspace_admin' : 'workspace_user',
          },
        ],
        has_more: next,
        last_id: next ? 'user_admin' : 'user_billing',
      }),
    );
  }) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_other', is_default: false });
  await waitFor(() => expect(screen.getByText('user_billing')).toBeTruthy());
  expect(screen.getByText('user_admin')).toBeTruthy();
  expect(paths).toEqual([
    '/v1/organizations/workspaces/wrkspc_other/members?limit=100',
    '/v1/organizations/workspaces/wrkspc_other/members?limit=100&after_id=user_admin',
  ]);
});
