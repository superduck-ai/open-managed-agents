import { afterEach, expect, test } from 'bun:test';
import { listAllWorkspaceAgents, listWorkspaceChannels } from './workspaceChannels';
const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
});

test('工作区渠道只请求一次，完整返回超过一页的渠道及聚合标记', async () => {
  const urls: URL[] = [];
  globalThis.fetch = (async (input) => {
    urls.push(new URL(String(input), 'https://oma.duck.ai'));
    return Response.json({
      data: Array.from({ length: 250 }, (_, index) => ({
        id: `ch-${index}`,
        agent_id: 'agent-1',
        agent_name: '客服',
        environment_id: 'env-1',
      })),
      incomplete: true,
      truncated: false,
    });
  }) as typeof fetch;
  const result = await listWorkspaceChannels('workspace-test');
  expect(result.channels).toHaveLength(250);
  expect(result.channels[249].id).toBe('ch-249');
  expect(result.channels[0].agentName).toBe('客服');
  expect(result.channels[0].environmentId).toBe('env-1');
  expect(result.incomplete).toBe(true);
  expect(result.truncated).toBe(false);
  expect(urls).toHaveLength(1);
  expect(urls[0].searchParams.has('limit')).toBe(false);
  expect(urls[0].searchParams.has('page')).toBe(false);
});

test('智能体选项继续读取第二页，不截断第 101 个智能体', async () => {
  const pages: (string | null)[] = [];
  globalThis.fetch = (async (input) => {
    const cursor = new URL(String(input), 'https://oma.duck.ai').searchParams.get('page');
    pages.push(cursor);
    return Response.json(
      cursor
        ? { data: [{ id: 'agent-101', name: '第 101 个智能体' }], next_page: null }
        : { data: Array.from({ length: 100 }, (_, index) => ({ id: `agent-${index + 1}` })), next_page: 'next-agents' },
    );
  }) as typeof fetch;
  const agents = await listAllWorkspaceAgents('workspace-test');
  expect(pages).toEqual([null, 'next-agents']);
  expect(agents).toHaveLength(101);
  expect(agents[100].id).toBe('agent-101');
});
