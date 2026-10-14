import { afterEach, expect, test } from 'bun:test';
import {
  mockAgentsApi,
  resetManagedAgentsTestState,
  resetTestDom,
  jsonResponse,
} from '../ManagedAgentsPage.test-utils';
import { findQuickstartSession } from './api';

afterEach(resetManagedAgentsTestState);

test('Session recovery filters by Agent across every page and matches the exact operation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const urls: URL[] = [];
  globalThis.fetch = (async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    urls.push(url);
    return jsonResponse(
      url.searchParams.has('page')
        ? {
            data: [{ type: 'session', id: 'sesn_match', metadata: { quickstart_operation_id: 'operation' } }],
            next_page: null,
          }
        : {
            data: [{ type: 'session', id: 'sesn_other', metadata: { quickstart_operation_id: 'other' } }],
            next_page: 'next',
          },
    );
  }) as typeof fetch;
  const result = await findQuickstartSession('operation', 'default', 'agent_selected');
  expect(result.map((item) => item.id)).toEqual(['sesn_match']);
  expect(urls).toHaveLength(2);
  for (const url of urls) expect(url.searchParams.get('agent_id')).toBe('agent_selected');
  expect(urls[1]?.searchParams.get('page')).toBe('next');
});
