import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';

const { render, screen, fireEvent, cleanup } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { ChannelsPageContent } = await import('./ChannelsPage');

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function mount() {
  resetTestDom('/');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const navigated: string[] = [];
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelsPageContent workspaceId="workspace-test" onNavigate={(href) => navigated.push(href)} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  return navigated;
}

test('工作区渠道页展示火山风格列表并支持新增跳转', async () => {
  globalThis.fetch = (async (input) => {
    const url = String(input);
    if (url.includes('/v1/agents?') || url.includes('/v1/agents&') || url.includes('/agents?')) {
      return Response.json({
        data: [{ id: 'agent-1', name: '客服助手' }],
        next_page: null,
      });
    }
    if (url.includes('/channels/settings')) {
      return Response.json({ environment_id: 'env_1' });
    }
    if (url.includes('/channels')) {
      return Response.json({
        data: [
          {
            id: 'ch_1',
            name: '飞书渠道',
            mode: 'open',
            source: 'console',
            enabled: true,
            connected: true,
            binding_status: 'bound',
            app_id: 'cli_test',
            include_thinking: true,
            include_tool_calls: true,
            reply_in_thread: true,
            pending_pairings: 0,
          },
        ],
      });
    }
    return Response.json({ data: [] });
  }) as typeof fetch;

  const navigated = mount();
  expect(await screen.findByRole('heading', { name: '渠道' })).toBeTruthy();
  expect(screen.getByText('让智能体进入日常工作')).toBeTruthy();
  expect(await screen.findByText('飞书渠道')).toBeTruthy();
  expect(screen.getByText('已连通')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: /新增渠道/ }));
  expect(navigated.at(-1)).toContain('/workspaces/workspace-test/channels/new');
});
