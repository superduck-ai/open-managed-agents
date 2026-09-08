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
      <button onClick={() => confirmation.request(() => void organizations.switchOrganization('b'))}>切换 B</button>
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
  let first!: Promise<void>;
  await act(async () => {
    first = organizations.switchOrganization('b');
  });
  await waitFor(() => expect(completeB).toBeDefined());
  await act(async () => organizations.switchOrganization('c'));
  await act(async () => {
    completeB(Response.json(workspaces('b')));
    await first;
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
