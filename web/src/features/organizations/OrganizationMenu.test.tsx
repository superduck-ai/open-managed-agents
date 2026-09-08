import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { AuthContext } from '../../shared/auth/context';
import { OrganizationContext } from '../../shared/organizations/context';
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from '../../shared/ui/dropdown-menu';
import { Button } from '../../shared/ui/button';
import { OrganizationMenu } from './OrganizationMenu';
import { setConsoleRequestContext } from '../../shared/api/client';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const invitation = {
  id: 'invite',
  organization_uuid: 'new-org',
  organization_name: '受邀组织',
  role: 'developer',
  invited_at: '2026-01-01',
  expires_at: '2027-01-01',
};
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

function mount() {
  resetTestDom('https://oma.duck.ai/dashboard');
  const switchOrganization = mock(async () => {});
  const refresh = mock(async () => ({ account: null }));
  const account = {
    uuid: 'account',
    email_address: 'a@example.com',
    memberships: [{ organization: { uuid: 'current', name: '当前组织' }, role: 'admin' }],
  };
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={{ account, status: 'authenticated', refresh, logout: async () => {} }}>
        <OrganizationContext.Provider
          value={{
            memberships: account.memberships,
            orgUuid: 'current',
            switching: false,
            error: null,
            switchOrganization,
            retry: async () => {},
          }}
        >
          <DropdownMenu defaultOpen>
            <DropdownMenuTrigger render={<Button />}>账号</DropdownMenuTrigger>
            <DropdownMenuContent>
              <OrganizationMenu />
            </DropdownMenuContent>
          </DropdownMenu>
        </OrganizationContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );
  return { switchOrganization, refresh, queryClient };
}

test('邀请加载失败可以重试，不自动循环', async () => {
  const fetchMock = mock(async () => Response.json({ message: 'offline' }, { status: 503 }));
  globalThis.fetch = fetchMock as typeof fetch;
  mount();
  fireEvent.click(await screen.findByRole('menuitem', { name: /组织邀请/ }));
  await screen.findByText('加载邀请失败。');
  globalThis.fetch = mock(async () => Response.json({ data: [invitation] })) as typeof fetch;
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  expect(await screen.findByText('受邀组织')).toBeTruthy();
});

test('接受邀请减少待处理数量并刷新bootstrap，不自动切换，进入组织由用户触发', async () => {
  let accepted = false;
  globalThis.fetch = mock(async (input) => {
    if (String(input).endsWith('/accept')) {
      accepted = true;
      return Response.json({ id: 'invite', status: 'accepted', organization_uuid: 'new-org' });
    }
    return Response.json({ data: accepted ? [] : [invitation] });
  }) as typeof fetch;
  const { switchOrganization, refresh } = mount();
  fireEvent.click(await screen.findByRole('menuitem', { name: '组织邀请 (1)' }));
  fireEvent.click(await screen.findByRole('button', { name: '接受', exact: true }));
  await screen.findByText('已加入 受邀组织');
  expect(switchOrganization).not.toHaveBeenCalled();
  expect(refresh).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole('button', { name: '接受', exact: true })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '进入组织' }));
  await waitFor(() => expect(switchOrganization).toHaveBeenCalledWith('new-org'));
});

test('拒绝邀请后保持当前组织，失败操作仍可重试', async () => {
  let attempts = 0;
  globalThis.fetch = mock(async (input) => {
    if (String(input).endsWith('/decline')) {
      attempts++;
      return attempts === 1
        ? Response.json({ message: 'try again' }, { status: 500 })
        : Response.json({ id: 'invite', status: 'declined', organization_uuid: 'new-org' });
    }
    return Response.json({ data: attempts >= 2 ? [] : [invitation] });
  }) as typeof fetch;
  const { switchOrganization, refresh } = mount();
  fireEvent.click(await screen.findByRole('menuitem', { name: '组织邀请 (1)' }));
  fireEvent.click(await screen.findByRole('button', { name: '拒绝' }));
  await screen.findByText('操作失败，请重试。');
  fireEvent.click(screen.getByRole('button', { name: '拒绝' }));
  await screen.findByText('没有待处理邀请。');
  expect(switchOrganization).not.toHaveBeenCalled();
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
});

test('组织菜单显示当前 membership 的中文角色，邀请卡片也显示中文角色', async () => {
  globalThis.fetch = mock(async () => Response.json({ data: [invitation] })) as typeof fetch;
  mount();
  expect(await screen.findByRole('menuitemradio', { name: '当前组织 管理员' })).toBeTruthy();
  fireEvent.click(await screen.findByRole('menuitem', { name: '组织邀请 (1)' }));
  expect(await screen.findByText(/开发者 · 有效期至/)).toBeTruthy();
});

test.each([
  [401, 'Unauthorized', '登录已失效，请重新登录。'],
  [403, 'Forbidden', '你无权处理此邀请。'],
  [404, 'Not found', '此邀请不存在或已不可用。'],
  [409, 'Invitation has expired', '此邀请已过期，请联系组织管理员重新邀请。'],
  [409, 'Invitation can no longer be processed', '此邀请已处理，无需重复操作。'],
  [409, 'Invitation has been revoked', '此邀请已被撤销，请联系组织管理员。'],
])('邀请操作错误 %s/%s 显示对应说明', async (status, message, expected) => {
  globalThis.fetch = mock(async (input) =>
    String(input).endsWith('/accept')
      ? Response.json({ message }, { status: Number(status) })
      : Response.json({ data: [invitation] }),
  ) as typeof fetch;
  const { refresh, switchOrganization } = mount();
  fireEvent.click(await screen.findByRole('menuitem', { name: '组织邀请 (1)' }));
  fireEvent.click(await screen.findByRole('button', { name: '接受', exact: true }));
  expect(await screen.findByText(String(expected))).toBeTruthy();
  expect(refresh).not.toHaveBeenCalled();
  expect(switchOrganization).not.toHaveBeenCalled();
});

test('拒绝后刷新使用服务端列表，迟到旧列表不会复活已处理邀请', async () => {
  let completeOld!: (response: Response) => void;
  let reads = 0;
  let processed = false;
  globalThis.fetch = mock(async (input) => {
    if (String(input).endsWith('/decline')) {
      processed = true;
      return Response.json({ id: invitation.id, status: 'declined', organization_uuid: invitation.organization_uuid });
    }
    reads++;
    if (reads === 2)
      return new Promise<Response>((resolve) => {
        completeOld = resolve;
      });
    return Response.json({ data: processed ? [] : [invitation] });
  }) as typeof fetch;
  const { refresh, queryClient } = mount();
  fireEvent.click(await screen.findByRole('menuitem', { name: '组织邀请 (1)' }));
  await waitFor(() => expect(completeOld).toBeDefined());
  fireEvent.click(await screen.findByRole('button', { name: '拒绝' }));
  await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
  await waitFor(() => expect(reads).toBe(3));
  completeOld(Response.json({ data: [invitation] }));
  await waitFor(() => expect(queryClient.getQueryData(['invitations', 'account'])).toEqual({ data: [] }));
  expect(screen.queryByRole('button', { name: '拒绝' })).toBeNull();
});
