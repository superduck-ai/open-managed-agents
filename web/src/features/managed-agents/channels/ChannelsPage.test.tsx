import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';

const { render, screen, fireEvent, cleanup, waitFor } = await import('@testing-library/react');
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
  const client = new QueryClient();
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

test('未配置渠道服务时直接显示空态，不显示错误或重试', async () => {
  let channelRequests = 0;
  globalThis.fetch = (async (input) => {
    if (String(input).includes('/v1/channels')) {
      channelRequests++;
      return Response.json({ error: { message: '飞书渠道服务不可用，请检查桥接服务配置' } }, { status: 503 });
    }
    return Response.json({ data: [] });
  }) as typeof fetch;

  mount();
  expect(await screen.findByText('暂无渠道')).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(screen.queryByRole('button', { name: '重试' })).toBeNull();
  expect(screen.getByRole('table').getAttribute('aria-busy')).toBe('false');
  expect(channelRequests).toBe(1);
});

test('权限错误不伪装成空列表，手动重试后可恢复', async () => {
  let channelRequests = 0;
  globalThis.fetch = (async (input) => {
    if (String(input).includes('/v1/channels')) {
      channelRequests++;
      if (channelRequests === 1) {
        return Response.json({ error: { message: '无权读取渠道' } }, { status: 403 });
      }
    }
    return Response.json({ data: [] });
  }) as typeof fetch;

  mount();
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(screen.getByText('无权读取渠道')).toBeTruthy();
  expect(screen.queryByText('暂无渠道')).toBeNull();
  expect(channelRequests).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: '重试' }));
  expect(await screen.findByText('暂无渠道')).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(channelRequests).toBe(2);
});

test('加载中保留表头和三行骨架，请求结束后展示空态', async () => {
  let finishRequest!: (response: Response) => void;
  globalThis.fetch = (async (input) => {
    if (String(input).includes('/v1/channels')) {
      return new Promise<Response>((resolve) => {
        finishRequest = resolve;
      });
    }
    return Response.json({ data: [] });
  }) as typeof fetch;

  mount();
  const table = screen.getByRole('table');
  expect(table.getAttribute('aria-busy')).toBe('true');
  expect(screen.getByRole('columnheader', { name: '渠道' })).toBeTruthy();
  expect(table.querySelectorAll('tbody tr')).toHaveLength(3);
  expect(table.querySelectorAll('[data-slot="skeleton"]')).toHaveLength(21);
  await waitFor(() => expect(finishRequest).toBeDefined());
  finishRequest(Response.json({ data: [] }));
  expect(await screen.findByText('暂无渠道')).toBeTruthy();
  expect(screen.getByRole('table').getAttribute('aria-busy')).toBe('false');
});

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
