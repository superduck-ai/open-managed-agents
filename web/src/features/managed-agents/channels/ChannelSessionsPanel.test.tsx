import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { SessionApiResponse } from '../types';
import type { Channel } from './api';

const { render, screen, cleanup } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { ChannelSessionsPanel } = await import('./ChannelSessionsPanel');

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

const channel: Channel = {
  id: 'ch-1',
  name: '飞书渠道',
  mode: 'open',
  source: 'console',
  enabled: true,
  connected: true,
  binding_status: 'bound',
  app_id: 'cli_test',
  include_thinking: true,
  include_tool_calls: true,
  reply_in_thread: false,
  pending_pairings: 0,
};

function session(patch: Partial<SessionApiResponse> & { id: string }): SessionApiResponse {
  return {
    type: 'session',
    title: patch.title ?? patch.id,
    status: 'idle',
    created_at: '2026-11-05T15:09:01Z',
    updated_at: '2026-11-05T15:09:18Z',
    archived_at: null,
    environment_id: 'env',
    resources: [],
    agent: { id: 'agent-1', type: 'agent', version: 1 },
    ...patch,
  };
}

function mount() {
  resetTestDom('/');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="zh-CN">
        <ChannelSessionsPanel workspaceId="workspace-test" agentId="agent-1" channel={channel} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

test('会话管理展示桥接会话统计、来源和详情链接', async () => {
  globalThis.fetch = (async () =>
    Response.json({
      data: [
        session({ id: 'console-session', title: '控制台会话', metadata: { source: 'console' } }),
        session({
          id: 'sesn-bridge',
          title: '飞书私聊',
          metadata: {
            source: 'lark-channel-bridge',
            bridge_binding_id: 'bind-1',
            open_id: 'ou_user',
            chat_type: 'p2p',
            channel_id: 'ch-1',
          },
        }),
        session({
          id: 'sesn-other',
          title: '其他渠道',
          metadata: { source: 'lark-channel-bridge', channel_id: 'ch-2', open_id: 'ou_other' },
        }),
      ],
      next_page: null,
    })) as typeof fetch;
  mount();
  expect(await screen.findByText('飞书私聊')).toBeTruthy();
  expect(screen.queryByText('控制台会话')).toBeNull();
  expect(screen.queryByText('其他渠道')).toBeNull();
  expect(screen.getByText('ou_user')).toBeTruthy();
  expect(screen.getByText('飞书渠道')).toBeTruthy();
  expect(screen.getByText('· 单聊')).toBeTruthy();
  expect(screen.getByText('会话数据同步中')).toBeTruthy();
  expect(screen.getByText('V1')).toBeTruthy();
  expect(screen.getByText('空闲')).toBeTruthy();
  expect(screen.getAllByText('2026-11-05 15:09:18').length).toBeGreaterThan(1);
  expect(screen.getByText('2026-11-05 15:09:01')).toBeTruthy();
  expect(screen.getByText('共 1 条')).toBeTruthy();
  expect(screen.queryByText('暂无按渠道维度的会话同步接口；桥接会话仍可在「会话」页按智能体查看。')).toBeNull();
  const link = screen.getByRole('link', { name: '查看详情' });
  expect(link.getAttribute('href')).toBe('/workspaces/workspace-test/sessions/sesn-bridge');
});

test('没有桥接会话时展示空态而不是接口缺失说明', async () => {
  globalThis.fetch = (async () => Response.json({ data: [], next_page: null })) as typeof fetch;
  mount();
  expect(await screen.findByText('暂无会话')).toBeTruthy();
  expect(screen.getByText('共 0 条')).toBeTruthy();
  expect(screen.queryByRole('link', { name: '查看详情' })).toBeNull();
});

test('会话加载失败时展示错误', async () => {
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ error: { message: '会话读取失败' } }), { status: 503 })) as typeof fetch;
  mount();
  expect(await screen.findByText('会话读取失败')).toBeTruthy();
  expect(screen.queryByText('暂无会话')).toBeNull();
});
