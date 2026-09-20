import { afterEach, expect, mock, test } from 'bun:test';

import { setConsoleRequestContext } from '../../shared/api/client';
import {
  archiveDream,
  cancelDream,
  createDream,
  dreamModelId,
  dreamSessionCount,
  dreamSessionIds,
  listDreams,
  retrieveDream,
} from './api';
import type { Dream } from './api';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});

test('uses the Dream beta contract for list, retrieve, create, archive, and cancel requests', async () => {
  const requests: Array<{ url: string; method: string; headers: Headers; body?: Record<string, unknown> }> = [];
  setConsoleRequestContext({ workspaceId: 'default' });
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push({
      url: String(input),
      method: init?.method ?? 'GET',
      headers: new Headers(init?.headers),
      body: init?.body ? (JSON.parse(String(init.body)) as Record<string, unknown>) : undefined,
    });
    return new Response(JSON.stringify({ data: [] }), { headers: { 'Content-Type': 'application/json' } });
  });

  await listDreams('cursor_test');
  await retrieveDream('drm_test');
  await createDream(
    {
      memoryStoreId: 'memstore_taste',
      model: 'claude-sonnet-4-6',
      sessionIds: ['sess_one'],
      instructions: 'Keep it concise.',
    },
    'csrf_test',
  );
  await archiveDream('drm_test', 'csrf_test');
  await cancelDream('drm_test', 'csrf_test');

  expect(requests).toHaveLength(5);
  for (const request of requests) {
    expect(request.headers.get('anthropic-version')).toBe('2023-06-01');
    expect(request.headers.get('anthropic-beta')).toBe('managed-agents-2026-04-01,dreaming-2026-04-21');
    expect(request.headers.get('x-workspace-id')).toBe('default');
  }
  expect(requests[1]).toMatchObject({
    method: 'GET',
    url: expect.stringContaining('/v1/dreams/drm_test'),
  });
  expect(requests[0].url).toContain('limit=20&page=cursor_test');
  expect(requests[2]).toMatchObject({
    method: 'POST',
    body: {
      model: 'claude-sonnet-4-6',
      instructions: 'Keep it concise.',
      inputs: [
        { type: 'memory_store', memory_store_id: 'memstore_taste' },
        { type: 'sessions', session_ids: ['sess_one'] },
      ],
    },
  });
  expect(requests[2].headers.get('x-csrf-token')).toBe('csrf_test');
  expect(requests[3]).toMatchObject({
    method: 'POST',
    url: expect.stringContaining('/v1/dreams/drm_test/archive'),
  });
  expect(requests[3].headers.get('x-csrf-token')).toBe('csrf_test');
  expect(requests[4]).toMatchObject({
    method: 'POST',
    url: expect.stringContaining('/v1/dreams/drm_test/cancel'),
  });
  expect(requests[4].headers.get('x-csrf-token')).toBe('csrf_test');
});

test('reads official split inputs and nested session_ids for display helpers', () => {
  const official: Dream = {
    id: 'drm_official',
    type: 'dream',
    status: 'running',
    model: { id: 'claude-opus-4-8' },
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    session_id: 'sesn_internal',
    inputs: [
      { type: 'memory_store', memory_store_id: 'memstore_taste' },
      { type: 'sessions', session_ids: ['sesn_one', 'sesn_two'] },
    ],
    outputs: [{ type: 'memory_store', memory_store_id: 'memstore_out' }],
  };
  const nested: Dream = {
    ...official,
    id: 'drm_nested',
    inputs: [{ type: 'memory_store', memory_store_id: 'memstore_taste', session_ids: ['sesn_legacy'] }],
  };
  expect(dreamModelId(official)).toBe('claude-opus-4-8');
  expect(dreamSessionIds(official)).toEqual(['sesn_one', 'sesn_two']);
  expect(dreamSessionCount(official)).toBe(2);
  expect(dreamSessionIds(nested)).toEqual(['sesn_legacy']);
});
