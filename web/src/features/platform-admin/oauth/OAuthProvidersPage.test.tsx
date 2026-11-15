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
  archived: false,
  enabled: true,
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
  fireEvent.change(screen.getByLabelText(/^客户端 ID/), { target: { value: 'client' } });
  fireEvent.change(screen.getByLabelText('授权端点'), { target: { value: 'https://auth.example.test/authorize' } });
  fireEvent.change(screen.getByLabelText('令牌端点'), { target: { value: 'https://auth.example.test/token' } });
  fireEvent.change(screen.getByLabelText(/^客户端密钥/), { target: { value: 'secret' } });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(new Headers(writes[0].headers).get('X-CSRF-Token')).toBe('test-csrf');
  expect(JSON.parse(String(writes[0].body))).toEqual({
    name: 'Example OAuth',
    logo_url: '',
    authorize_endpoint: 'https://auth.example.test/authorize',
    token_endpoint: 'https://auth.example.test/token',
    scope: '',
    default_secret_name: '',
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
  name: '飞书',
  scope: 'offline_access',
  default_secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
  default_allowed_hosts: ['open.feishu.cn', 'accounts.feishu.cn'],
};

test.each([
  { record: undefined, method: 'client_secret_post', encoding: 'form' },
  { record: { ...builtin, token_endpoint_encoding: 'json' }, method: 'client_secret_post', encoding: 'json' },
  {
    record: { ...builtin, name: 'Friday', token_endpoint_auth_method: 'client_secret_basic' },
    method: 'client_secret_basic',
    encoding: 'form',
  },
])('高级配置默认折叠且保留新建或已存协议配置：%s', async ({ record, method, encoding }) => {
  const writes: Record<string, unknown>[] = [];
  globalThis.fetch = mock(async (_input, options) => {
    if (options?.method === 'POST' || options?.method === 'PATCH') {
      writes.push(JSON.parse(String(options.body)));
      return Response.json(record ?? provider);
    }
    return Response.json(record ? [record] : []);
  }) as typeof fetch;
  mount();
  if (record) fireEvent.click(await screen.findByText(record.name));
  else {
    await screen.findByText('暂无 OAuth 客户端');
    fireEvent.click(screen.getByRole('button', { name: '注册 OAuth 客户端' }));
    fireEvent.change(await screen.findByLabelText('名称'), { target: { value: 'New OAuth' } });
    fireEvent.change(screen.getByLabelText('授权端点'), { target: { value: provider.authorize_endpoint } });
    fireEvent.change(screen.getByLabelText('令牌端点'), { target: { value: provider.token_endpoint } });
  }
  const advanced = await screen.findByRole('button', { name: '高级配置' });
  expect(advanced.getAttribute('aria-expanded')).toBe('false');
  expect(screen.queryByRole('combobox', { name: '令牌端点认证' })).toBeNull();
  fireEvent.click(advanced);
  expect((await screen.findByRole('combobox', { name: '令牌端点认证' })).textContent).toContain(method);
  expect(screen.getByRole('combobox', { name: '令牌编码' }).textContent).toContain(encoding);
  fireEvent.click(advanced);
  fireEvent.click(screen.getByRole('button', { name: '保存' }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({ token_endpoint_auth_method: method, token_endpoint_encoding: encoding });
});

test('飞书客户端通过更多操作统一提供停用和删除', async () => {
  globalThis.fetch = mock(async () => Response.json([builtin])) as typeof fetch;
  mount();
  expect(await screen.findByText('飞书')).toBeTruthy();
  expect(screen.queryByText('内置')).toBeNull();
  expect(screen.queryByText('已配置')).toBeNull();
  expect(screen.getByRole('columnheader', { name: '状态' })).toBeTruthy();
  expect(screen.getByText('启用')).toBeTruthy();
  expect(screen.queryByRole('button', { name: '删除' })).toBeNull();
  expect(screen.queryByText('暂无 OAuth 客户端')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: '飞书的更多操作' }));
  expect(await screen.findByRole('menuitem', { name: '删除' })).toBeTruthy();
  expect(screen.queryByRole('menuitem', { name: '编辑 OAuth 客户端' })).toBeNull();
  expect(screen.queryByRole('dialog')).toBeNull();
});

test('飞书首次配置字段可编辑并提交完整配置', async () => {
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
  expect(name.readOnly).toBe(false);
  expect((screen.getByLabelText('授权端点') as HTMLInputElement).readOnly).toBe(false);
  expect((screen.getByLabelText('令牌端点') as HTMLInputElement).readOnly).toBe(false);
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(false);
  fireEvent.change(screen.getByLabelText(/^客户端 ID/), { target: { value: 'new-client' } });
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText(/^客户端密钥/), { target: { value: 'new-secret' } });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(String(writes[0].body))).toEqual({
    expected_version: 1,
    default_secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
    logo_url: '',
    scope: 'offline_access',
    name: builtin.name,
    authorize_endpoint: builtin.authorize_endpoint,
    token_endpoint: builtin.token_endpoint,
    token_endpoint_auth_method: builtin.token_endpoint_auth_method,
    token_endpoint_encoding: builtin.token_endpoint_encoding,
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
  const client = await screen.findByLabelText(/^客户端 ID/);
  expect((screen.getByLabelText(/^客户端密钥/) as HTMLInputElement).value).toBe('');
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
    default_secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
    logo_url: '',
    scope: 'offline_access',
    name: builtin.name,
    authorize_endpoint: builtin.authorize_endpoint,
    token_endpoint: builtin.token_endpoint,
    token_endpoint_auth_method: builtin.token_endpoint_auth_method,
    token_endpoint_encoding: builtin.token_endpoint_encoding,
    client_id: 'client',
  });
});

test('并发版本冲突后重新打开使用最新版本并可保存', async () => {
  let version = 1;
  const writes: number[] = [];
  globalThis.fetch = mock(async (input, options) => {
    if (options?.method === 'PATCH') {
      writes.push(JSON.parse(String(options.body)).expected_version);
      version = 2;
      return writes.length === 1
        ? Response.json({ error: { code: 'oauth_provider_version_conflict' } }, { status: 409 })
        : Response.json({ ...builtin, version });
    }
    return Response.json([{ ...builtin, version }]);
  }) as typeof fetch;
  mount();
  fireEvent.click(await screen.findByText('飞书'));
  await screen.findByLabelText(/^客户端 ID/);
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: '保存' }));
  });
  expect(await screen.findByText('服务方已被修改，请关闭并重新打开表单。')).toBeTruthy();
  await waitFor(() => expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '取消' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(await screen.findByText('飞书'));
  await screen.findByLabelText(/^客户端 ID/);
  fireEvent.click(screen.getByRole('button', { name: '保存' }));
  await waitFor(() => expect(writes).toEqual([1, 2]));
});

test('所有平台客户端统一提供更多操作', async () => {
  globalThis.fetch = mock(async () => Response.json([builtin, { ...provider, name: 'Feishu' }])) as typeof fetch;
  mount();
  expect(await screen.findByText('飞书')).toBeTruthy();
  expect(screen.getByText('Feishu')).toBeTruthy();
  expect(screen.queryByText('内置')).toBeNull();
  expect(screen.queryByText('自定义')).toBeNull();
  expect(screen.getAllByRole('button', { name: /的更多操作$/ })).toHaveLength(2);
});

test('Friday 与飞书分别展示，Friday 使用独立端点首配', async () => {
  const friday = {
    ...builtin,
    id: 'poap_FridayBuiltinOAuthClient',
    provider_key: 'builtin:friday',
    name: 'Friday',
    default_secret_name: 'FRIDAY_4A_TOKEN',
    authorize_endpoint: 'https://friday-tx.seewo.com/open/api/oauth/authorize',
    token_endpoint: 'https://friday-tx.seewo.com/open/api/oauth/token',
    token_endpoint_auth_method: 'client_secret_basic',
    token_endpoint_encoding: 'form',
    scope: 'read',
    configured: false,
    client_id: '',
    has_client_id: false,
    has_client_secret: false,
  };
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (input, options) => {
    if (options?.method === 'PATCH') {
      expect(String(input)).toContain(friday.id);
      writes.push(options);
      return Response.json({ ...friday, configured: true });
    }
    return Response.json([builtin, friday]);
  }) as typeof fetch;
  mount();
  expect(await screen.findByText('飞书')).toBeTruthy();
  fireEvent.click(await screen.findByText('Friday'));
  expect(((await screen.findByLabelText('名称')) as HTMLInputElement).value).toBe('Friday');
  expect((screen.getByLabelText('授权端点') as HTMLInputElement).value).toBe(friday.authorize_endpoint);
  expect((screen.getByLabelText('令牌端点') as HTMLInputElement).readOnly).toBe(false);
  expect(screen.queryByRole('button', { name: '删除' })).toBeNull();
  fireEvent.change(screen.getByLabelText(/^客户端 ID/), { target: { value: 'synthetic-friday-id' } });
  fireEvent.change(screen.getByLabelText(/^客户端密钥/), { target: { value: 'synthetic-friday-secret' } });
  fireEvent.change(screen.getByLabelText(/^Scope/), { target: { value: 'offline_access' } });
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(false);
  fireEvent.change(screen.getByLabelText(/^Scope/), { target: { value: 'read' } });
  await act(async () => fireEvent.click(screen.getByRole('button', { name: '保存' })));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(String(writes[0].body))).toEqual({
    expected_version: 1,
    default_secret_name: 'FRIDAY_4A_TOKEN',
    logo_url: '',
    scope: 'read',
    name: friday.name,
    authorize_endpoint: friday.authorize_endpoint,
    token_endpoint: friday.token_endpoint,
    token_endpoint_auth_method: friday.token_endpoint_auth_method,
    token_endpoint_encoding: friday.token_endpoint_encoding,
    client_id: 'synthetic-friday-id',
    client_secret: 'synthetic-friday-secret',
  });
});

test('Logo 可选且客户端凭据可先留空，支持编辑清除 Logo', async () => {
  const writes: Record<string, unknown>[] = [];
  let record = {
    ...provider,
    logo_url: '',
    client_id: '',
    configured: false,
    has_client_id: false,
    has_client_secret: false,
  };
  globalThis.fetch = mock(async (_input, options) => {
    if (options?.method === 'POST' || options?.method === 'PATCH') {
      const body = JSON.parse(String(options.body));
      writes.push(body);
      record = { ...record, ...body, name: 'Logo client' };
      return Response.json(record);
    }
    return Response.json(writes.length ? [record] : []);
  }) as typeof fetch;
  mount();
  await screen.findByText('暂无 OAuth 客户端');
  fireEvent.click(screen.getByRole('button', { name: '注册 OAuth 客户端' }));
  fireEvent.change(await screen.findByLabelText('名称'), { target: { value: 'Logo client' } });
  fireEvent.change(screen.getByLabelText('授权端点'), { target: { value: provider.authorize_endpoint } });
  fireEvent.change(screen.getByLabelText('令牌端点'), { target: { value: provider.token_endpoint } });
  fireEvent.change(screen.getByLabelText(/^客户端 ID/), { target: { value: 'half-client' } });
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(true);
  fireEvent.change(screen.getByLabelText(/^客户端 ID/), { target: { value: '' } });
  expect(screen.getByRole('button', { name: '保存' }).hasAttribute('disabled')).toBe(false);
  fireEvent.change(screen.getByLabelText(/^Logo URL/), { target: { value: 'https://example.test/logo.png' } });
  fireEvent.click(screen.getByRole('button', { name: '保存' }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({ client_id: '', logo_url: 'https://example.test/logo.png' });
  expect(writes[0]).not.toHaveProperty('client_secret');
  fireEvent.click(await screen.findByText('Logo client'));
  expect(((await screen.findByLabelText(/^Logo URL/)) as HTMLInputElement).value).toBe('https://example.test/logo.png');
  fireEvent.change(screen.getByLabelText(/^Logo URL/), { target: { value: '' } });
  fireEvent.click(screen.getByRole('button', { name: '保存' }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1].logo_url).toBe('');
  expect(writes[1]).not.toHaveProperty('client_id');
});

test('默认变量名可编辑并保存', async () => {
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (_input, options) => {
    if (options?.method === 'PATCH') {
      writes.push(options);
      return Response.json(builtin);
    }
    return Response.json([builtin]);
  }) as typeof fetch;
  mount();
  fireEvent.click(await screen.findByText('飞书'));
  const input = await screen.findByLabelText(/^默认变量名/);
  expect((input as HTMLInputElement).readOnly).toBe(false);
  fireEvent.change(input, { target: { value: 'TEAM_FEISHU_TOKEN' } });
  fireEvent.click(screen.getByRole('button', { name: '保存' }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(String(writes[0].body)).default_secret_name).toBe('TEAM_FEISHU_TOKEN');
});

test.each([true, false])('客户端启停确认并仅提交状态和版本：%s', async (enabled) => {
  const writes: RequestInit[] = [];
  globalThis.fetch = mock(async (_input, options) => {
    if (options?.method === 'PATCH') {
      writes.push(options);
      return Response.json({ ...provider, enabled: !enabled });
    }
    return Response.json([{ ...provider, enabled }]);
  }) as typeof fetch;
  mount();
  await screen.findByText('Example OAuth');
  expect(screen.queryByRole('button', { name: enabled ? '停用' : '启用' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Example OAuth的更多操作' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: enabled ? '停用' : '启用' }));
  if (enabled)
    expect(
      screen.getByText('从工作区可选服务方中隐藏。已有凭证和会话继续可用；停用不会撤销授权或自动刷新能力。'),
    ).toBeTruthy();
  expect(writes).toHaveLength(0);
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(String(writes[0].body))).toEqual({ enabled: !enabled, expected_version: provider.version });
});

test('状态筛选区分活跃、启用、停用和归档客户端', async () => {
  const records = [
    provider,
    { ...provider, id: 'disabled', name: 'Disabled client', enabled: false },
    { ...provider, id: 'archived', name: 'Archived client', archived: true },
  ];
  globalThis.fetch = mock(async () => Response.json(records)) as typeof fetch;
  mount();
  await screen.findByText('Example OAuth');
  expect(screen.getByText('Disabled client')).toBeTruthy();
  expect(screen.queryByText('Archived client')).toBeNull();
  for (const [label, visible, hidden] of [
    ['启用', 'Example OAuth', 'Disabled client'],
    ['停用', 'Disabled client', 'Example OAuth'],
    ['已归档', 'Archived client', 'Example OAuth'],
    ['全部', 'Example OAuth', 'not-a-client'],
  ]) {
    fireEvent.click(screen.getByRole('combobox', { name: /^状态 / }));
    fireEvent.click(await screen.findByRole('option', { name: label }));
    await screen.findByText(visible);
    expect(screen.queryByText(hidden)).toBeNull();
  }
  expect(screen.getByText('Archived client')).toBeTruthy();
  expect(screen.getByText('Disabled client')).toBeTruthy();
});

test('归档确认后隐藏客户端，恢复保留启用状态且仅提交归档状态和版本', async () => {
  let record = { ...provider };
  const writes: Record<string, unknown>[] = [];
  globalThis.fetch = mock(async (_input, options) => {
    if (options?.method === 'PATCH') {
      const body = JSON.parse(String(options.body));
      writes.push(body);
      record = { ...record, ...body, version: record.version + 1 };
      return Response.json(record);
    }
    return Response.json([record]);
  }) as typeof fetch;
  mount();
  await screen.findByText('Example OAuth');
  fireEvent.click(screen.getByRole('button', { name: 'Example OAuth的更多操作' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: '归档' }));
  expect(writes).toHaveLength(0);
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  await screen.findByText('没有符合筛选条件的 OAuth 客户端');
  expect(writes[0]).toEqual({ archived: true, expected_version: 1 });
  fireEvent.click(screen.getByRole('combobox', { name: /^状态 / }));
  fireEvent.click(await screen.findByRole('option', { name: '已归档' }));
  await screen.findByText('Example OAuth');
  fireEvent.click(screen.getByRole('button', { name: 'Example OAuth的更多操作' }));
  expect(screen.queryByRole('menuitem', { name: '停用' })).toBeNull();
  fireEvent.click(await screen.findByRole('menuitem', { name: '恢复' }));
  fireEvent.click(screen.getByRole('button', { name: '确认' }));
  await screen.findByText('没有符合筛选条件的 OAuth 客户端');
  expect(writes[1]).toEqual({ archived: false, expected_version: 2 });
  fireEvent.click(screen.getByRole('combobox', { name: /^状态 / }));
  fireEvent.click(await screen.findByRole('option', { name: '活跃' }));
  await screen.findByText('Example OAuth');
  expect(screen.getByText('启用')).toBeTruthy();
});

test('更多操作保留可聚焦的编辑入口', async () => {
  globalThis.fetch = mock(async () => Response.json([provider])) as typeof fetch;
  mount();
  const trigger = await screen.findByRole('button', { name: 'Example OAuth的更多操作' });
  trigger.focus();
  fireEvent.keyDown(trigger, { key: 'ArrowDown' });
  const edit = await screen.findByRole('menuitem', { name: '编辑' });
  edit.focus();
  expect(document.activeElement).toBe(edit);
  fireEvent.keyDown(edit, { key: 'Enter' });
  expect(await screen.findByRole('dialog', { name: '编辑 OAuth 客户端' })).toBeTruthy();
});
