import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
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
        <WorkspaceContext.Provider value={value}>
          <WorkspaceMembersPage />
        </WorkspaceContext.Provider>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

test('普通空间权限拒绝时不回退显示组织成员', async () => {
  globalThis.fetch = mock(
    async () => new Response(JSON.stringify({ error: { message: 'Access denied' } }), { status: 403 }),
  ) as unknown as typeof fetch;
  renderMembers({ ...defaultWorkspace, id: 'wrkspc_denied', is_default: false });
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
            workspace_role: next ? 'workspace_admin' : 'workspace_billing',
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
