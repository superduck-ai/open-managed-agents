import '../../test/setup';
import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { screen, fireEvent, waitFor, cleanup, within } from '@testing-library/react';
import { renderPlatformAdmin } from '../../test/platform-admin';
import { PlatformAdminContext } from './context';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { OrganizationMembers } from './OrganizationMembers';
import type { Organization } from './types';
const originalFetch = globalThis.fetch;
const queryClients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  queryClients.splice(0).forEach((client) => client.clear());
  globalThis.fetch = originalFetch;
});
const organization: Organization = {
  uuid: '018dbbd1-25d8-7000-8000-000000000001',
  name: '目标组织',
  version: 5,
  member_count: 1,
  admin_count: 1,
  workspace_count: 1,
  website: '',
  industry: '',
  created_at: '2026-11-02T00:00:00Z',
};
function Harness() {
  const [org, setOrg] = useState(organization);
  const [queries] = useState(() => {
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    queryClients.push(queries);
    return queries;
  });
  const client = new PlatformAdminClient(() => {});
  client.setCSRFToken('csrf-test');
  return (
    <QueryClientProvider client={queries}>
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: { uuid: 'operator', name: '超管', email: 'operator@example.test' },
            platform_access: { role: 'super_admin', permissions: [] },
            csrf_token: 'csrf-test',
          },
        }}
      >
        <OrganizationMembers organization={org} onChanged={setOrg} />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>
  );
}
test('后台直接指定账号并默认添加普通成员，不调用Console或邀请接口', async () => {
  const requests: Array<{ url: string; init?: RequestInit }> = [];
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    requests.push({ url, init });
    if (url.includes('/accounts?'))
      return Response.json({
        data: [{ uuid: '018dbbd1-25d8-7000-8000-000000000009', name: '新成员', email: 'new@example.test' }],
        next_cursor: '',
      });
    if (init?.method === 'POST') return Response.json({ ...organization, version: 6, member_count: 2 });
    return Response.json({
      data: [{ uuid: 'member-admin', name: '现有管理员', email: 'admin@example.test', role: 'admin' }],
      next_cursor: '',
    });
  }) as typeof fetch;
  renderPlatformAdmin(<Harness />);
  await screen.findByText('现有管理员');
  expect(screen.queryByRole('button', { name: '交接' })).toBeNull();
  expect(screen.queryByRole('button', { name: '邀请', exact: true })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '添加成员' }));
  const dialog = await screen.findByRole('dialog', { name: '添加成员' });
  fireEvent.change(within(dialog).getByRole('textbox', { name: '搜索已注册账号' }), { target: { value: 'new' } });
  fireEvent.click(within(dialog).getByRole('button', { name: '查找' }));
  fireEvent.click(await within(dialog).findByRole('checkbox', { name: '选择 new@example.test' }));
  fireEvent.click(within(dialog).getByRole('button', { name: '添加', exact: true }));
  await waitFor(() => expect(requests.some((r) => r.init?.method === 'POST')).toBe(true));
  const request = requests.find((r) => r.init?.method === 'POST')!;
  expect(JSON.parse(String(request.init?.body))).toEqual({
    account_uuid: '018dbbd1-25d8-7000-8000-000000000009',
    role: 'user',
    expected_version: 5,
  });
  expect(new Headers(request.init?.headers).get('X-CSRF-Token')).toBe('csrf-test');
  expect(new Headers(request.init?.headers).has('X-Organization-UUID')).toBe(false);
  expect(requests.every((r) => r.url.startsWith('/api/platform-admin/'))).toBe(true);
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});
test('后台筛选提交到服务端，搜索变化清除分页游标', async () => {
  const urls: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = String(input);
    urls.push(url);
    return Response.json({
      data: [{ uuid: 'member', name: '成员', email: 'member@example.test', role: 'user' }],
      next_cursor: url.includes('cursor=next') ? '' : 'next',
    });
  }) as typeof fetch;
  renderPlatformAdmin(<Harness />);
  await screen.findByText('成员', { selector: 'span[title]' });
  fireEvent.click(screen.getByRole('button', { name: '下一页' }));
  await waitFor(() => expect(urls.some((u) => u.includes('cursor=next'))).toBe(true));
  fireEvent.change(screen.getByRole('textbox', { name: '搜索成员' }), { target: { value: '不存在的姓名' } });
  await waitFor(() =>
    expect(
      urls.some(
        (u) =>
          new URL(u, 'http://test').searchParams.get('query') === '不存在的姓名' &&
          new URL(u, 'http://test').searchParams.get('cursor') === '',
      ),
    ).toBe(true),
  );
});
