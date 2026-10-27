import { afterEach, expect, mock, test } from 'bun:test';
import { getObservabilityTrace } from './api';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test('trace detail keeps the list window and workspace scope', async () => {
  let capturedPath = '';
  let capturedHeaders = new Headers();
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    capturedPath = String(input);
    capturedHeaders = new Headers(init?.headers);
    return new Response(
      JSON.stringify({ trace_id: 'trace/1', data_as_of: '2026-08-13T00:00:00Z', spans: [], truncated: false }),
      {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      },
    );
  }) as unknown as typeof fetch;

  await getObservabilityTrace('org/1', 'ws/1', 'trace/1', {
    start_time: '2026-08-12T00:00:00Z',
    end_time: '2026-08-13T00:00:00Z',
    agent_id: 'agent/1',
    session_id: 'session/1',
    agent_version: [3, 4],
  });

  const requestURL = new URL(capturedPath, 'https://oma.local');
  expect(requestURL.pathname).toBe('/api/organizations/org%2F1/observability/traces/trace%2F1');
  expect(requestURL.searchParams.get('start_time')).toBe('2026-08-12T00:00:00Z');
  expect(requestURL.searchParams.get('end_time')).toBe('2026-08-13T00:00:00Z');
  expect(requestURL.searchParams.get('agent_id')).toBe('agent/1');
  expect(requestURL.searchParams.get('session_id')).toBe('session/1');
  expect(requestURL.searchParams.getAll('agent_version')).toEqual(['3', '4']);
  expect(capturedHeaders.get('x-workspace-id')).toBe('ws/1');
});
