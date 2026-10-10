import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthContext } from '../auth/context';
import { canManageMembers } from '../permissions/members';
import { resetTestDom } from '../../test/setup';
import { WorkspaceProvider } from './WorkspaceProvider';
import { useWorkspace } from './context';

const { render, screen, cleanup, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const account = {
  uuid: 'user',
  email_address: 'test@example.local',
  memberships: [{ role: 'admin', organization: { uuid: 'org' } }],
};
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  window.localStorage.clear();
});
function ActiveWorkspace() {
  const { activeWorkspaceId, error } = useWorkspace();
  return <p>{error ? 'failed' : activeWorkspaceId || 'loading'}</p>;
}

test('显式空权限不回退旧管理员身份，缺失字段才兼容角色', () => {
  expect(canManageMembers({ ...account, permissions: [] })).toBe(false);
  expect(canManageMembers({ ...account, permissions: ['workspaces:view'] })).toBe(false);
  expect(canManageMembers({ ...account, permissions: ['members:manage'] })).toBe(true);
  expect(canManageMembers(account)).toBe(true);
});

for (const fail of [true, false]) {
  test(`工作区加载及${fail ? '失败' : '成功'}期间保留偏好`, async () => {
    resetTestDom('https://oma.duck.ai/');
    window.localStorage.setItem('oma.activeWorkspaceId', 'wrkspc_saved');
    let finish!: (response: Response) => void;
    globalThis.fetch = mock(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    ) as typeof fetch;
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={client}>
        <AuthContext.Provider
          value={{ account, status: 'authenticated', refresh: async () => undefined, logout: async () => undefined }}
        >
          <WorkspaceProvider>
            <ActiveWorkspace />
          </WorkspaceProvider>
        </AuthContext.Provider>
      </QueryClientProvider>,
    );
    expect(screen.getByText('loading')).toBeTruthy();
    expect(window.localStorage.getItem('oma.activeWorkspaceId')).toBe('wrkspc_saved');
    await waitFor(() => expect(finish).toBeDefined());
    finish(
      fail
        ? new Response('{}', { status: 500 })
        : Response.json([{ id: 'wrkspc_saved', type: 'workspace', name: 'Saved' }]),
    );
    await waitFor(() => expect(screen.getByText(fail ? 'failed' : 'wrkspc_saved')).toBeTruthy());
    expect(window.localStorage.getItem('oma.activeWorkspaceId')).toBe('wrkspc_saved');
    client.clear();
  });
}
