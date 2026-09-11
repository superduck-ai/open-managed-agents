import { canManageMembers } from '../permissions/members';
import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthContext, useAuth, type AuthContextValue } from '../auth/context';
import { useOrganizations, type OrganizationContextValue } from '../organizations/context';
import { useWorkspace } from './context';
import { WorkspaceProvider } from './WorkspaceProvider';
import { getConsoleRequestContext, reportApiAuthFailure, setConsoleRequestContext } from '../api/client';
import type { AuthAccount } from '../auth/api';
import { useScopeUnsavedChanges } from '../organizations/unsaved';
import { useScopeConfirmation } from '../../features/organizations/useScopeConfirmation';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { workspaceSwitchPath } from './presentation';

const { act, cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
let organizations: OrganizationContextValue;
const account: AuthAccount = {
  uuid: 'account',
  email_address: 'a@example.com',
  default_organization_uuid: 'a',
  memberships: [
    { organization: { uuid: 'a' }, role: 'admin', user_id: 'self-a' },
    { organization: { uuid: 'b' }, role: 'user', user_id: 'self-b' },
    { organization: { uuid: 'c' }, role: 'billing', user_id: 'self-c' },
  ],
};
const workspaces = (org: string) => [
  { id: `ws-${org}`, name: org, type: 'workspace', is_default: true, effective_role: 'workspace_developer' },
];

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.localStorage.clear();
  window.sessionStorage.clear();
});

function Probe({ dirty = false }: { dirty?: boolean }) {
  organizations = useOrganizations()!;
  const workspace = useWorkspace();
  const auth = useAuth();
  useScopeUnsavedChanges(dirty);
  const confirmation = useScopeConfirmation();
  return (
    <>
      <output data-testid="scope">
        {organizations.orgUuid}:{workspace.activeWorkspaceId}:{auth.account?.memberships?.[0]?.user_id}
      </output>
      <output data-testid="state">
        {organizations.switching ? 'loading' : organizations.error ? 'error' : 'ready'}
      </output>
      <output data-testid="workspace-name">{workspace.activeWorkspace.name}</output>
      <button onClick={() => confirmation.request(() => void organizations.switchOrganization('b'))}>切换 B</button>
      <button onClick={() => confirmation.request(() => workspace.selectWorkspace('ws-next'))}>切换工作区</button>
      {confirmation.dialog}
    </>
  );
}

function mount({
  refresh,
  navigate = mock(async () => {}),
  initialWorkspaceId,
  dirty = false,
  initialAccount = account,
}: {
  refresh?: AuthContextValue['refresh'];
  navigate?: (workspaceId: string) => Promise<void>;
  initialWorkspaceId?: string;
  dirty?: boolean;
  initialAccount?: AuthAccount;
} = {}) {
  resetTestDom('https://oma.duck.ai/workspaces/ws-a/agents/agent-detail');
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const auth: AuthContextValue = {
    account: initialAccount,
    csrfToken: 'csrf',
    status: 'authenticated',
    logout: async () => {},
    refresh: refresh ?? (async () => ({ account: initialAccount })),
  };
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={auth}>
        <WorkspaceProvider navigateScope={navigate} initialWorkspaceId={initialWorkspaceId}>
          <Probe dirty={dirty} />
        </WorkspaceProvider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
  return { queryClient, navigate };
}

function mockWorkspaces() {
  globalThis.fetch = mock(async (input) => Response.json(workspaces(String(input).split('/').at(-2)!))) as typeof fetch;
}

test('保留前置分支的 Default 展示名但不改写真实工作区标识', async () => {
  mockWorkspaces();
  mount();
  await waitFor(() => expect(screen.getByTestId('workspace-name').textContent).toBe('Default'));
  expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a');
});

test('取消未保存确认前不触碰 context、不刷新 bootstrap、不取消旧查询', async () => {
  mockWorkspaces();
  const refresh = mock(async () => ({ account }));
  const { queryClient } = mount({ refresh, dirty: true });
  await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('ready'));
  queryClient.setQueryData(['files', 'ws-a'], ['existing']);
  fireEvent.click(screen.getByRole('button', { name: '切换 B' }));
  expect(screen.getByRole('alertdialog')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '继续编辑' }));
  expect(refresh).not.toHaveBeenCalled();
  expect(getConsoleRequestContext().organizationUuid).toBe('a');
  expect(queryClient.getQueryData(['files', 'ws-a'])).toEqual(['existing']);
});

test('初始化保留深链并选用该组织真实工作区，显式切换才导航', async () => {
  mockWorkspaces();
  const { navigate } = mount({ initialWorkspaceId: 'ws-a' });
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a'));
  expect(navigate).not.toHaveBeenCalled();
  await act(async () => organizations.switchOrganization('b'));
  expect(navigate).toHaveBeenCalledWith('ws-b');
  expect(screen.getByTestId('scope').textContent).toBe('b:ws-b:self-b');
  expect(window.sessionStorage.getItem('oma.organization.account')).toBe('b');
  expect(window.localStorage.getItem('oma.workspace.account.b')).toBe('ws-b');
  expect(window.localStorage.getItem('oma.organization.account')).toBeNull();
});

test('连续快速切换时旧组织迟到响应不能覆盖最后选择', async () => {
  let completeB!: (response: Response) => void;
  globalThis.fetch = mock(async (input) => {
    const org = String(input).split('/').at(-2)!;
    if (org === 'b')
      return new Promise<Response>((resolve) => {
        completeB = resolve;
      });
    return Response.json(workspaces(org));
  }) as typeof fetch;
  mount();
  await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('ready'));
  let first!: Promise<boolean>;
  await act(async () => {
    first = organizations.switchOrganization('b');
  });
  await waitFor(() => expect(completeB).toBeDefined());
  await act(async () => organizations.switchOrganization('c'));
  await act(async () => {
    completeB(Response.json(workspaces('b')));
    expect(await first).toBe(false);
  });
  expect(screen.getByTestId('scope').textContent).toBe('c:ws-c:self-c');
  expect(getConsoleRequestContext().organizationUuid).toBe('c');
});

test('组织列表为空时清除旧 scope，缺省工作区缺失时稳定为空', async () => {
  globalThis.fetch = mock(async () => Response.json([])) as typeof fetch;
  setConsoleRequestContext({ organizationUuid: 'stale', workspaceId: 'stale-ws' });
  mount({ initialAccount: { ...account, memberships: [] } });
  await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('ready'));
  expect(getConsoleRequestContext().organizationUuid).toBeUndefined();
  expect(getConsoleRequestContext().workspaceId).toBeUndefined();
});

test('普通权限403不切换，之后被移出组织仍可恢复到有效默认组织', async () => {
  mockWorkspaces();
  let latest = account;
  const refresh = mock(async () => ({ account: latest }));
  mount({ refresh });
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a'));
  await act(async () => {
    reportApiAuthFailure(403, { organizationUuid: 'a', workspaceId: 'ws-a' });
  });
  expect(refresh).toHaveBeenCalledTimes(1);
  expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a');
  latest = {
    ...account,
    default_organization_uuid: 'c',
    memberships: account.memberships?.filter((item) => item.organization?.uuid !== 'a'),
  };
  await act(async () => {
    reportApiAuthFailure(403, { organizationUuid: 'a', workspaceId: 'ws-a' });
  });
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('c:ws-c:self-c'));
});

test('网络失败显示可重试状态且不会自动循环', async () => {
  const fetchMock = mock(async () => {
    throw new TypeError('offline');
  });
  globalThis.fetch = fetchMock as typeof fetch;
  mount();
  await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('error'));
  expect(fetchMock).toHaveBeenCalledTimes(1);
  mockWorkspaces();
  await act(async () => organizations.retry());
  expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a');
});

test('组织不匹配或缺少组织标识的403不触发恢复', async () => {
  mockWorkspaces();
  const refresh = mock(async () => ({ account }));
  mount({ refresh });
  await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a'));
  await act(async () => {
    reportApiAuthFailure(403, { workspaceId: 'ws-a' });
    reportApiAuthFailure(403, { organizationUuid: 'b', workspaceId: 'ws-a' });
  });
  expect(refresh).not.toHaveBeenCalled();
});

for (const workspaceId of [undefined, '']) {
  test(`空工作区403允许同组织恢复：${String(workspaceId)}`, async () => {
    globalThis.fetch = mock(async () => Response.json([])) as typeof fetch;
    const refresh = mock(async () => ({ account }));
    mount({ refresh });
    await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a::self-a'));
    mockWorkspaces();
    await act(async () => reportApiAuthFailure(403, { organizationUuid: 'a', workspaceId }));
    await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a'));
    expect(refresh).toHaveBeenCalledTimes(1);
  });
}

for (const path of ['/workspaces/ws-a/agents', '/workspaces/ws-a/agents/agent-detail']) {
  test(`真实Provider切换工作区只导航一次并移除详情标识：${path}`, async () => {
    resetTestDom(`https://oma.duck.ai${path}`);
    const scrollDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo');
    Object.assign(globalThis, { scrollTo: () => {} });
    globalThis.fetch = mock(async () =>
      Response.json([...workspaces('a'), { ...workspaces('a')[0], id: 'ws-next', is_default: false }]),
    ) as typeof fetch;
    const root = createRootRoute();
    const route = createRoute({
      getParentRoute: () => root,
      path: '/workspaces/$workspaceId/agents/{-$agentId}',
      component: () => null,
    });
    const router = createRouter({
      routeTree: root.addChildren([route]),
      history: createMemoryHistory({ initialEntries: [path] }),
    });
    await router.load();
    const navigate = mock(async (workspaceId: string) => {
      await router.navigate({
        href: workspaceSwitchPath(router.state.location.pathname, workspaceId),
        replace: true,
        ignoreBlocker: true,
      });
    });
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={queryClient}>
        <AuthContext.Provider
          value={{
            account,
            status: 'authenticated',
            csrfToken: 'csrf',
            refresh: async () => ({ account }),
            logout: async () => {},
          }}
        >
          <WorkspaceProvider navigateScope={navigate} initialWorkspaceId="ws-a">
            <Probe />
            <RouterProvider router={router} />
          </WorkspaceProvider>
        </AuthContext.Provider>
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a'));
    expect(navigate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: '切换工作区' }));
    await waitFor(() => expect(screen.getByTestId('scope').textContent).toBe('a:ws-next:self-a'));
    expect(navigate).toHaveBeenCalledTimes(1);
    expect(router.state.location.pathname).toBe('/workspaces/ws-next/agents');
    expect(getConsoleRequestContext().workspaceId).toBe('ws-next');
    cleanup();
    if (scrollDescriptor) Object.defineProperty(globalThis, 'scrollTo', scrollDescriptor);
    else Reflect.deleteProperty(globalThis, 'scrollTo');
  });
}

test('403恢复失败后不受后续403触发，显式重试才能恢复', async () => {
  mockWorkspaces();
  const refresh = mock(async (): Promise<{ account: AuthAccount }> => {
    throw new TypeError('offline');
  });
  mount({ refresh });
  await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('ready'));
  const context = { organizationUuid: 'a', workspaceId: 'ws-a' };
  await act(async () => reportApiAuthFailure(403, context));
  expect(screen.getByTestId('state').textContent).toBe('error');
  await act(async () => reportApiAuthFailure(403, context));
  expect(refresh).toHaveBeenCalledTimes(1);
  refresh.mockImplementation(async () => ({ account }));
  await act(async () => expect(await organizations.retry()).toBe(true));
  expect(screen.getByTestId('state').textContent).toBe('ready');
});

test('目标组织加载失败返回false，不提交新scope，重试成功返回true', async () => {
  mockWorkspaces();
  mount();
  await waitFor(() => expect(screen.getByTestId('state').textContent).toBe('ready'));
  globalThis.fetch = mock(async () => {
    throw new TypeError('offline');
  }) as typeof fetch;
  await act(async () => expect(await organizations.switchOrganization('b')).toBe(false));
  expect(screen.getByTestId('scope').textContent).toBe('a:ws-a:self-a');
  mockWorkspaces();
  await act(async () => expect(await organizations.retry()).toBe(true));
  expect(screen.getByTestId('scope').textContent).toBe('b:ws-b:self-b');
});

test('显式空权限不回退管理员身份，缺失字段才兼容角色', () => {
  expect(canManageMembers({ ...account, permissions: [] })).toBe(false);
  expect(canManageMembers({ ...account, permissions: ['workspaces:view'] })).toBe(false);
  expect(canManageMembers({ ...account, permissions: ['members:manage'] })).toBe(true);
  expect(canManageMembers(account)).toBe(true);
});

for (const fail of [true, false]) {
  test(`工作区加载及${fail ? '失败' : '成功'}期间保留按组织隔离的偏好`, async () => {
    let finish!: (response: Response) => void;
    globalThis.fetch = mock(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    ) as typeof fetch;
    const { queryClient } = mount();
    const key = 'oma.workspace.account.a';
    window.localStorage.setItem(key, 'ws-saved');
    await waitFor(() => expect(finish).toBeDefined());
    expect(window.localStorage.getItem(key)).toBe('ws-saved');
    await act(async () => {
      finish(
        fail
          ? new Response('{}', { status: 500 })
          : Response.json([
              ...workspaces('a'),
              { id: 'ws-saved', name: 'Saved', type: 'workspace', effective_role: 'workspace_developer' },
            ]),
      );
    });
    await waitFor(() => expect(screen.getByTestId('state').textContent).toBe(fail ? 'error' : 'ready'));
    expect(window.localStorage.getItem(key)).toBe('ws-saved');
    if (!fail) expect(screen.getByTestId('scope').textContent).toBe('a:ws-saved:self-a');
    queryClient.clear();
  });
}
