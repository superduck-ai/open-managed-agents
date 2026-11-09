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
