import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin as render } from '../../test/platform-admin';
import { resetTestDom } from '../../test/setup';
import { PlatformAdminClient } from '../../shared/api/platform-admin';
import { PlatformAdminContext } from './context';
import { SuperAdministratorsPage } from './SuperAdministratorsPage';

const { act, cleanup, screen, fireEvent, waitFor, within } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const operator = { uuid: 'operator', email: 'operator@example.com', name: '操作员' };
const target = { uuid: 'target', email: 'target@example.com', name: '目标账号' };
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  globalThis.fetch = originalFetch;
});

function mount(locale: 'zh-CN' | 'en' = 'zh-CN') {
  resetTestDom('https://oma.duck.ai/platform-admin/administrators');
  const client = new PlatformAdminClient(() => {});
  client.csrfToken = 'test-csrf';
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  clients.push(queries);
  render(
    <QueryClientProvider client={queries}>
      <PlatformAdminContext.Provider
        value={{
          client,
          bootstrap: {
            account: operator,
            csrf_token: 'test-csrf',
            platform_access: {
              role: 'super_admin',
              permissions: ['platform:access', 'platform:administrators:manage'],
            },
          },
        }}
      >
        <SuperAdministratorsPage />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
    locale,
  );
}

test.each([
  ['zh-CN', '超级管理员', '添加超级管理员'],
  ['en', 'Super administrators', 'Add super administrator'],
] as const)('%s 展示独立的超级管理员目录和右上角操作', async (locale, title, add) => {
  globalThis.fetch = mock(async () => Response.json({ data: [operator], next_cursor: '' })) as typeof fetch;
  mount(locale);
  expect(screen.getByRole('heading', { name: title })).toBeTruthy();
  expect(screen.getByRole('button', { name: add })).toBeTruthy();
  expect(await screen.findByText(operator.email)).toBeTruthy();
});

test('添加授权要求选择账号，失败保留选择并允许重试，成功刷新目录', async () => {
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (input, options) => {
    if (String(input).includes('/accounts?')) return Response.json({ data: [target], next_cursor: '' });
    if (options?.method === 'POST') {
      writes.push(options);
      return writes.length === 1
        ? Response.json({ error: { code: 'unavailable' } }, { status: 503 })
        : Response.json({ granted: true });
    }
    return Response.json({ data: writes.length > 1 ? [operator, target] : [operator], next_cursor: '' });
  }) as typeof fetch;
  mount();
  await screen.findByText(operator.email);
  fireEvent.click(screen.getByRole('button', { name: '添加超级管理员' }));
  expect(screen.getByRole('button', { name: '确认变更' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('搜索已注册账号'), { target: { value: 'target' } });
  fireEvent.click(screen.getByRole('button', { name: '查找' }));
  fireEvent.click(await screen.findByRole('checkbox'));
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '确认变更' }));
  });
  expect((await screen.findByRole('alert')).textContent).toContain('管理服务暂不可用');
  expect(screen.getByLabelText('已选管理员').textContent).toContain(target.email);
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '确认变更' }));
  });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(await screen.findByText(target.email)).toBeTruthy();
  expect(writes).toHaveLength(2);
  expect(JSON.parse(String(writes[1].body))).toEqual({ account_uuid: target.uuid });
  expect(new Headers(writes[1].headers).get('X-CSRF-Token')).toBe('test-csrf');
});

test('撤销需要确认，最后管理员错误保留对话框，成功后刷新目录', async () => {
  let attempts = 0;
  globalThis.fetch = mock(async (input, options) => {
    if (options?.method === 'DELETE') {
      expect(String(input)).toBe('/api/platform-admin/administrators/target');
      expect(new Headers(options.headers).get('X-CSRF-Token')).toBe('test-csrf');
      attempts++;
      return attempts === 1
        ? Response.json({ error: { code: 'last_platform_admin' } }, { status: 409 })
        : Response.json({ granted: false });
    }
    return Response.json({ data: attempts > 1 ? [operator] : [operator, target], next_cursor: '' });
  }) as typeof fetch;
  mount();
  const row = (await screen.findByText(target.email)).closest('tr')!;
  fireEvent.click(within(row).getByRole('button', { name: '撤销超级管理员' }));
  expect(attempts).toBe(0);
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '确认变更' }));
  });
  expect((await screen.findByRole('alert')).textContent).toContain('必须保留至少一位超级管理员');
  expect(screen.getByRole('dialog')).toBeTruthy();
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '确认变更' }));
  });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  await waitFor(() => expect(screen.queryByText(target.email)).toBeNull());
});
