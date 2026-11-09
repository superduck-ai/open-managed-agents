import { afterEach, expect, test } from 'bun:test';
import { listWorkspaceChannels } from './workspaceChannels';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test('工作区渠道列表跟随 next_page 并保留不完整标记', async () => {
  const pages: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    pages.push(url.search);
    const cursor = url.searchParams.get('page');
    if (!cursor) {
      return Response.json({
        data: [
          {
            id: 'ch-1',
            name: '第一页',
            mode: 'open',
            source: 'console',
            enabled: true,
            connected: true,
            binding_status: 'bound',
            include_thinking: true,
            include_tool_calls: true,
            reply_in_thread: false,
            pending_pairings: 0,
            agent_id: 'agent-1',
            agent_name: '客服',
            environment_id: 'env-1',
          },
        ],
        incomplete: true,
        truncated: false,
        next_page: '1',
      });
    }
    return Response.json({
      data: [
        {
          id: 'ch-2',
          name: '第二页',
          mode: 'open',
          source: 'console',
          enabled: false,
          connected: false,
          binding_status: 'unbound',
          include_thinking: false,
          include_tool_calls: false,
          reply_in_thread: false,
          pending_pairings: 0,
          agent_id: 'agent-2',
          agent_name: '助手',
          environment_id: null,
        },
      ],
      incomplete: false,
      truncated: true,
      next_page: null,
    });
  }) as typeof fetch;

  const result = await listWorkspaceChannels('workspace-test');
  expect(result.channels.map((channel) => channel.id)).toEqual(['ch-1', 'ch-2']);
  expect(result.channels[0]?.agentName).toBe('客服');
  expect(result.channels[1]?.environmentId).toBeNull();
  expect(result.incomplete).toBe(true);
  expect(result.truncated).toBe(true);
  expect(pages[0]).toContain('limit=100');
  expect(pages[1]).toContain('page=1');
});
