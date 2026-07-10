import { afterEach, describe, expect, mock, test } from 'bun:test';
import { setConsoleRequestContext } from '../../../../shared/api/client';
import { loadMcpDirectoryServers, resetMcpDirectoryCacheForTests } from './api';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  resetMcpDirectoryCacheForTests();
});

describe('MCP directory API', () => {
  test('deduplicates concurrent requests and caches a successful response', async () => {
    let requestCount = 0;
    globalThis.fetch = mock(async () => {
      requestCount += 1;
      return jsonResponse({
        servers: [{
          type: 'remote',
          slug: 'notion',
          display_name: 'Notion',
          tool_names: ['search'],
          remote: { url: 'https://mcp.notion.com/mcp' }
        }]
      });
    });

    const [first, second] = await Promise.all([
      loadMcpDirectoryServers(),
      loadMcpDirectoryServers()
    ]);
    const cached = await loadMcpDirectoryServers();

    expect(requestCount).toBe(1);
    expect(second).toBe(first);
    expect(cached).toBe(first);
    expect(first[0].toolNames).toEqual(['search']);
  });

  test('does not cache a failed request and allows a later retry', async () => {
    let requestCount = 0;
    globalThis.fetch = mock(async () => {
      requestCount += 1;
      if (requestCount === 1) {
        return jsonResponse({ error: { message: 'directory unavailable' } }, 503);
      }
      return jsonResponse({
        servers: [{
          type: 'remote',
          slug: 'slack',
          display_name: 'Slack',
          tool_names: ['slack_send_message'],
          remote: { url: 'https://mcp.slack.com/mcp' }
        }]
      });
    });

    await expect(loadMcpDirectoryServers()).rejects.toMatchObject({ status: 503 });
    const servers = await loadMcpDirectoryServers();

    expect(requestCount).toBe(2);
    expect(servers[0].slug).toBe('slack');
  });

  test('keeps a reset request isolated from an older in-flight request', async () => {
    let requestCount = 0;
    let resolveFirst!: (response: Response) => void;
    let resolveSecond!: (response: Response) => void;
    const firstResponse = new Promise<Response>((resolve) => {
      resolveFirst = resolve;
    });
    const secondResponse = new Promise<Response>((resolve) => {
      resolveSecond = resolve;
    });
    globalThis.fetch = mock(() => {
      requestCount += 1;
      return requestCount === 1 ? firstResponse : secondResponse;
    });

    const firstRequest = loadMcpDirectoryServers();
    resetMcpDirectoryCacheForTests();
    const secondRequest = loadMcpDirectoryServers();

    resolveFirst(jsonResponse({
      servers: [{ type: 'remote', slug: 'old', display_name: 'Old', tool_names: ['old_tool'] }]
    }));
    await firstRequest;
    expect(loadMcpDirectoryServers()).toBe(secondRequest);

    resolveSecond(jsonResponse({
      servers: [{ type: 'remote', slug: 'new', display_name: 'New', tool_names: ['new_tool'] }]
    }));
    const servers = await secondRequest;
    const cached = await loadMcpDirectoryServers();

    expect(requestCount).toBe(2);
    expect(servers[0].slug).toBe('new');
    expect(cached).toBe(servers);
  });
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  });
}
