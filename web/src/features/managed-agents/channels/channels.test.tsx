import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
const { render, screen, fireEvent, waitFor, cleanup } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { AgentChannels } = await import('./AgentChannels');
const { setConsoleRequestContext } = await import('../../../shared/api/client');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});
function mount(locale: 'zh-CN' | 'en' = 'zh-CN') {
  resetTestDom('/');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale={locale}>
        <AgentChannels agentId="agent-test" workspaceId="workspace-test" canEdit />
      </I18nProvider>
    </QueryClientProvider>,
  );
}
test('渠道服务失败时显示错误并禁用创建', async () => {
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ error: { message: '桥接服务不可用' } }), { status: 503 })) as typeof fetch;
  mount();
  await waitFor(() => expect(screen.getByText('桥接服务不可用')).toBeTruthy());
  expect(screen.getByRole('button', { name: '创建机器人' }).hasAttribute('disabled')).toBe(true);
  expect(screen.queryByText('还没有连接机器人')).toBeNull();
  expect(screen.queryByText(/0 \/ 5/)).toBeNull();
});
test('保存凭据、模式和话题策略并携带当前工作区及 CSRF', async () => {
  let posted: Record<string, unknown> | undefined;
  globalThis.fetch = (async (input, init) => {
    expect(String(input)).toContain('/v1/agents/agent-test/channels');
    const headers = new Headers(init?.headers);
    expect(headers.get('x-workspace-id')).toBe('workspace-test');
    if (init?.method === 'POST') {
      expect(headers.get('x-csrf-token')).toBe('csrf-test');
      posted = JSON.parse(String(init.body)) as Record<string, unknown>;
      return Response.json({ ...posted, id: 'channel-test', binding_status: 'bound', connected: false });
    }
    return Response.json({ data: [] });
  }) as typeof fetch;
  setConsoleRequestContext({ csrfToken: 'csrf-test' });
  mount();
  await waitFor(() => expect(screen.getByRole('button', { name: '创建机器人' }).hasAttribute('disabled')).toBe(false));
  fireEvent.click(screen.getByRole('button', { name: '创建机器人' }));
  fireEvent.change(await screen.findByLabelText('名称 *'), { target: { value: '客服助手' } });
  fireEvent.click(screen.getByRole('radio', { name: '配对模式' }));
  fireEvent.click(screen.getByRole('radio', { name: '配置凭据' }));
  fireEvent.change(screen.getByLabelText('App ID'), { target: { value: 'cli_test' } });
  fireEvent.change(screen.getByLabelText('App Secret'), { target: { value: 'test-secret' } });
  fireEvent.click(screen.getByRole('switch', { name: '话题归属' }));
  fireEvent.click(screen.getByRole('button', { name: '保存', exact: true }));
  await waitFor(() => expect(posted?.name).toBe('客服助手'));
  expect(posted?.mode).toBe('pairing');
  expect(posted?.app_secret).toBe('test-secret');
  expect(posted?.reply_in_thread).toBe(true);
});

test('英文界面翻译渠道列表、表单和校验提示', async () => {
  globalThis.fetch = (async () => Response.json({ data: [] })) as typeof fetch;
  mount('en');
  await waitFor(() => expect(screen.getByRole('button', { name: 'Create bot' }).hasAttribute('disabled')).toBe(false));
  expect(screen.getByRole('heading', { name: 'IM connections' })).toBeTruthy();
  expect(screen.getByText('No bots connected')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Create bot' }));
  expect(
    screen.getByText('Users can chat with the bot without approval. The mode cannot be changed after creation.'),
  ).toBeTruthy();
  fireEvent.click(screen.getByRole('radio', { name: 'Pairing mode' }));
  expect(
    screen.getByText(
      'Direct or group chats require administrator approval. The mode cannot be changed after creation.',
    ),
  ).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Generate QR code' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Enter a bot name'));
});
