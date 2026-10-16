import { afterEach, expect, mock, test } from 'bun:test';
import { renderPlatformAdmin as render } from '../../test/platform-admin';
import { resetTestDom } from '../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { PlatformAdminContext } from './context';
import { CreateOrganizationDialog } from './CreateOrganizationDialog';

const { cleanup, screen, fireEvent, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('创建失败保留管理员与名称，重试复用幂等键', async () => {
  resetTestDom('https://oma.duck.ai/platform-admin');
  const requests: { key: string | null; body: string }[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, options?: RequestInit) => {
    if (String(input).includes('/accounts?'))
      return Response.json({
        data: [{ uuid: 'registered-account', email: 'alice@example.com', name: 'Alice' }],
        next_cursor: '',
      });
    requests.push({ key: new Headers(options?.headers).get('Idempotency-Key'), body: String(options?.body) });
    if (requests.length === 1)
      return Response.json({ error: { code: 'unavailable', message: '临时失败' } }, { status: 503 });
    return Response.json({ uuid: 'new-organization', name: '研发组织' });
  }) as unknown as typeof fetch;
  const client = new PlatformAdminClient(() => {});
  client.csrfToken = 'test-csrf';
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const created = mock();
  render(
    <QueryClientProvider client={queries}>
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: { uuid: 'operator', name: '', email: '' },
            csrf_token: 'test-csrf',
            platform_access: { role: 'super_admin', permissions: ['platform:access'] },
          },
        }}
      >
        <CreateOrganizationDialog open onClose={() => {}} onCreated={created} />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
  fireEvent.change(screen.getByLabelText('组织名称'), { target: { value: '研发组织' } });
  expect(screen.getByRole('button', { name: '创建组织' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('搜索已注册账号'), { target: { value: 'alice' } });
  fireEvent.click(screen.getByRole('button', { name: '查找' }));
  fireEvent.click(await screen.findByRole('checkbox'));
  await waitFor(() => expect(screen.getByRole('button', { name: '创建组织' }).hasAttribute('disabled')).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '创建组织' }));
  expect((await screen.findByRole('alert')).textContent).toContain('管理服务暂不可用');
  expect(screen.queryByLabelText('操作原因')).toBeNull();
  expect(screen.getByLabelText('已选管理员').textContent).toContain('alice@example.com');
  fireEvent.click(screen.getByRole('button', { name: '创建组织' }));
  await waitFor(() => expect(created).toHaveBeenCalledTimes(1));
  expect(requests).toHaveLength(2);
  expect(requests[0].key).toBeTruthy();
  expect(requests[0]).toEqual(requests[1]);
  expect(JSON.parse(requests[0].body)).toEqual({
    name: '研发组织',

    admin_account_uuids: ['registered-account'],
  });
  queries.clear();
});
