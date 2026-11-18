import { afterEach, expect, mock, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
const { render, screen, fireEvent, cleanup, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { ChannelDetailContent } = await import('./ChannelDetailPage');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  mock.restore();
});

test('连接检测重新请求后端并更新已失效的连接状态', async () => {
  resetTestDom('/');
  let connected = true;
  let requests = 0;
  globalThis.fetch = (async (input) => {
    const path = String(input).split('?')[0];
    if (path.endsWith('/check'))
      return Response.json({
        ok: connected,
        execution_verified: false,
        checks: [{ code: 'connection', ok: connected, message: '渠道连接未建立' }],
      });
    if (path.endsWith('/channels')) {
      requests++;
      return Response.json({
        data: [
          {
            id: 'ch-test',
            name: '连接验收',
            enabled: true,
            connected,
            binding_status: 'bound',
            mode: 'open',
            include_thinking: true,
            include_tool_calls: true,
            reply_in_thread: true,
            pending_pairings: 0,
          },
        ],
      });
    }
    if (path.endsWith('/settings')) return Response.json({ environment_id: 'env-test' });
    if (path.endsWith('/agent-test')) return Response.json({ id: 'agent-test', name: '验收助手' });
    return Response.json({ data: [] });
  }) as typeof fetch;
  render(
    <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelDetailContent
          workspaceId="workspace-test"
          agentId="agent-test"
          channelId="ch-test"
          tab="basic"
          onTabChange={() => {}}
          onNavigate={() => {}}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  await screen.findByText('已连通');
  const before = requests;
  connected = false;
  fireEvent.click(screen.getByRole('button', { name: '连接检测' }));
  await waitFor(() => expect(requests).toBeGreaterThan(before));
  await screen.findByText('异常');
});
