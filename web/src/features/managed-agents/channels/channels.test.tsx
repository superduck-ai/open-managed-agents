import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
const { render, screen, fireEvent, waitFor, cleanup } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { AgentChannels } = await import('./AgentChannels');
const { setConsoleRequestContext } = await import('../../../shared/api/client');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});
function mount() {
  resetTestDom('/');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <AgentChannels agentId="agent-test" workspaceId="workspace-test" canEdit />
    </QueryClientProvider>,
  );
}
test('渠道服务失败时显示错误并禁用创建', async () => {
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ error: { message: '桥接服务不可用' } }), { status: 503 })) as typeof fetch;
  mount();
  await waitFor(() => expect(screen.getByText('桥接服务不可用')).toBeTruthy());
  expect(screen.getByRole('button', { name: '创建机器人' }).hasAttribute('disabled')).toBe(true);
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
      return new Response(JSON.stringify({ ...posted, id: 'channel-test', binding_status: 'bound', connected: false }));
    }
    return new Response(JSON.stringify({ data: [] }));
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
