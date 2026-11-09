import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderPlatformAdmin as render } from '../../../test/platform-admin';
import { resetTestDom } from '../../../test/setup';
import { PlatformAdminClient } from '../../../shared/api/platform-admin';
import { PlatformAdminContext } from '../context';
import { OAuthProvidersPage } from './OAuthProvidersPage';

const { act, cleanup, screen, fireEvent, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const operator = { uuid: 'operator', email: 'operator@example.com', name: '操作员' };
const clients: QueryClient[] = [];

afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  globalThis.fetch = originalFetch;
});

function mount() {
  resetTestDom('https://oma.duck.ai/platform-admin/oauth');
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
            platform_access: { role: 'super_admin', permissions: ['platform:access'] },
          },
        }}
      >
        <OAuthProvidersPage />
      </PlatformAdminContext.Provider>
    </QueryClientProvider>,
  );
}

const provider = {
  id: 'poap_test',
  provider_key: 'custom:poap_test',
  builtin: false,
  protected: false,
  configured: true,
  has_client_id: true,
  default_secret_name: '',
  default_allowed_hosts: [],
  version: 1,
  name: 'Example OAuth',
  authorize_endpoint: 'https://auth.example.test/authorize',
  token_endpoint: 'https://auth.example.test/token',
  scope: 'read',
  client_id: 'client',
  has_client_secret: true,
  token_endpoint_auth_method: 'client_secret_post',
  token_endpoint_encoding: 'form',
  created_at: '2026-11-06T00:00:00Z',
  updated_at: '2026-11-06T00:00:00Z',
};

test('展示 OAuth 管理页与已注册客户端', async () => {
  globalThis.fetch = mock(async () => Response.json([provider])) as typeof fetch;
  mount();
  expect(screen.getByRole('heading', { name: 'OAuth 管理' })).toBeTruthy();
  expect(await screen.findByText('Example OAuth')).toBeTruthy();
  expect(screen.getByText('https://auth.example.test/token')).toBeTruthy();
  expect(screen.queryByText('内置服务方')).toBeNull();
});

test('注册客户端提交完整字段', async () => {
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (input, options) => {
    if (options?.method === 'POST') {
      writes.push(options);
      return Response.json(provider, { status: 201 });
    }
    return Response.json([]);
  }) as typeof fetch;
  mount();
  await screen.findByText('暂无 OAuth 客户端');
  fireEvent.click(screen.getByRole('button', { name: '注册 OAuth 客户端' }));
  fireEvent.change(await screen.findByLabelText('名称'), { target: { value: 'Example OAuth' } });
  fireEvent.change(screen.getByLabelText('客户端 ID'), { target: { value: 'client' } });
  fireEvent.change(screen.getByLabelText('授权端点'), { target: { value: 'https://auth.example.test/authorize' } });
  fireEvent.change(screen.getByLabelText('令牌端点'), { target: { value: 'https://auth.example.test/token' } });
  fireEvent.change(screen.getByLabelText('客户端密钥'), { target: { value: 'secret' } });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(new Headers(writes[0].headers).get('X-CSRF-Token')).toBe('test-csrf');
  expect(JSON.parse(String(writes[0].body))).toEqual({
    name: 'Example OAuth',
    authorize_endpoint: 'https://auth.example.test/authorize',
    token_endpoint: 'https://auth.example.test/token',
    scope: '',
    client_id: 'client',
    token_endpoint_auth_method: 'client_secret_post',
    token_endpoint_encoding: 'form',
    client_secret: 'secret',
  });
});

const builtin = {
  ...provider,
  id: 'poap_FeishuBuiltinOAuthClient',
  provider_key: 'builtin:feishu',
  builtin: true,
  protected: true,
  name: 'Feishu',
  scope: 'offline_access',
  default_secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
  default_allowed_hosts: ['open.feishu.cn', 'accounts.feishu.cn'],
};

test('飞书作为列表行展示内置标识且不提供删除按钮', async () => {
  globalThis.fetch = mock(async () => Response.json([builtin])) as typeof fetch;
  mount();
  expect(await screen.findByText('飞书')).toBeTruthy();
  expect(screen.getByText('内置')).toBeTruthy();
  expect(screen.getByText('已配置')).toBeTruthy();
  expect(screen.queryByRole('button', { name: '删除' })).toBeNull();
  expect(screen.queryByText('暂无 OAuth 客户端')).toBeNull();
});

test('飞书首次配置固定字段只读并按最小字段提交', async () => {
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (input, options) => {
    if (options?.method === 'PATCH') {
      writes.push(options);
      return Response.json(builtin);
    }
    return Response.json([
      { ...builtin, configured: false, client_id: '', has_client_id: false, has_client_secret: false },
    ]);
  }) as typeof fetch;
  mount();
  fireEvent.click(await screen.findByText('飞书'));
  const name = (await screen.findByLabelText('名称')) as HTMLInputElement;
  expect(name.readOnly).toBe(true);
  expect((screen.getByLabelText('授权端点') as HTMLInputElement).readOnly).toBe(true);
  expect((screen.getByLabelText('令牌端点') as HTMLInputElement).readOnly).toBe(true);
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('客户端 ID'), { target: { value: 'new-client' } });
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText('客户端密钥'), { target: { value: 'new-secret' } });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(String(writes[0].body))).toEqual({
    expected_version: 1,
    scope: 'offline_access',
    client_id: 'new-client',
    client_secret: 'new-secret',
  });
});

test('飞书保留旧密钥且更换应用时要求新密钥', async () => {
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (input, options) => {
    if (options?.method === 'PATCH') {
      writes.push(options);
      return Response.json(builtin);
    }
    return Response.json([builtin]);
  }) as typeof fetch;
  mount();
  fireEvent.click(await screen.findByText('飞书'));
  const client = await screen.findByLabelText('客户端 ID');
  expect((screen.getByLabelText('客户端密钥') as HTMLInputElement).value).toBe('');
  fireEvent.change(client, { target: { value: 'different' } });
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByText('修改客户端 ID 必须同时填写新密钥，已有令牌可能需要重新授权。')).toBeTruthy();
  fireEvent.change(client, { target: { value: builtin.client_id } });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(String(writes[0].body))).toEqual({
    expected_version: 1,
    scope: 'offline_access',
    client_id: 'client',
  });
});

test('并发版本冲突显示本地化提示', async () => {
  globalThis.fetch = mock(async (input, options) =>
    options?.method === 'PATCH'
      ? Response.json({ error: { code: 'oauth_provider_version_conflict' } }, { status: 409 })
      : Response.json([builtin]),
  ) as typeof fetch;
  mount();
  fireEvent.click(await screen.findByText('飞书'));
  await screen.findByLabelText('客户端 ID');
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  expect(await screen.findByText('服务方已被修改，请关闭并重新打开表单。')).toBeTruthy();
});

test('历史同名自定义 Feishu 与内置飞书保留独立类型标识', async () => {
  globalThis.fetch = mock(async () => Response.json([builtin, { ...provider, name: 'Feishu' }])) as typeof fetch;
  mount();
  expect(await screen.findByText('飞书')).toBeTruthy();
  expect(screen.getByText('Feishu')).toBeTruthy();
  expect(screen.getByText('内置')).toBeTruthy();
  expect(screen.getByText('自定义')).toBeTruthy();
  expect(screen.getAllByRole('button', { name: '删除' })).toHaveLength(1);
});
