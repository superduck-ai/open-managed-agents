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
});

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  });
}
