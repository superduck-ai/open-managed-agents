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

test('工作区渠道页读取工作区渠道接口并支持新增跳转', async () => {
  const urls: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = String(input);
    urls.push(url);
    if (url.includes('/v1/channels')) {
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
            agent_id: 'agent-1',
            agent_name: '客服助手',
            environment_id: 'env_1',
            created_at: '2026-11-05T15:09:01Z',
          },
        ],
        incomplete: true,
        truncated: false,
      });
    }
    return Response.json({ data: [] });
  }) as typeof fetch;

  const navigated = mount();
  expect(await screen.findByRole('heading', { name: '渠道' })).toBeTruthy();
  expect(screen.queryByText('让智能体进入日常工作')).toBeNull();
  expect(screen.queryByText('管理智能体在飞书中的接入渠道')).toBeNull();
  expect(await screen.findByText('飞书渠道')).toBeTruthy();
  expect(screen.getByText('已连通')).toBeTruthy();
  expect(screen.getByText('客服助手')).toBeTruthy();
  expect(screen.getByRole('columnheader', { name: '创建时间' })).toBeTruthy();
  expect(
    screen.getByText(
      new Intl.DateTimeFormat('zh-CN', { month: 'short', day: 'numeric' }).format(new Date('2026-11-05T15:09:01Z')),
    ),
  ).toBeTruthy();
  expect(screen.queryByText('部分智能体的渠道暂时无法读取，列表可能不完整。')).toBeNull();
  expect(screen.getByText('1 / 1')).toBeTruthy();
  expect(urls.filter((url) => url.includes('/v1/channels'))).toHaveLength(1);
  expect(
    urls.filter((url) => url.includes('/v1/channels')).some((url) => url.includes('limit=') || url.includes('page=')),
  ).toBe(false);
  expect(urls.some((url) => url.includes('/v1/channels'))).toBe(true);
  expect(urls.some((url) => url.includes('/v1/agents/'))).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: /新增渠道/ }));
  expect(navigated.at(-1)).toContain('/workspaces/workspace-test/channels/new');
});
