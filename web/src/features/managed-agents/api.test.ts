import { afterEach, describe, expect, test } from 'bun:test';
import { setAnthropicClientForTest } from '@/shared/api/anthropic';
import { listSessionFileOptions } from './api';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
});

describe('managed agents API', () => {
  test('loads every workspace file page for session resource options', async () => {
    const firstPage = Array.from({ length: 1000 }, (_, index) => fileMetadata(index));
    const finalFile = fileMetadata(1000);
    const requestedAfterIds: Array<string | null> = [];
    globalThis.fetch = (async (input) => {
      const url = new URL(requestURL(input), 'http://127.0.0.1');
      const afterId = url.searchParams.get('after_id');
      requestedAfterIds.push(afterId);
      const response = afterId
        ? { data: [finalFile], first_id: finalFile.id, has_more: false, last_id: finalFile.id }
        : {
            data: firstPage,
            first_id: firstPage[0]?.id,
            has_more: true,
            last_id: firstPage.at(-1)?.id,
          };
      return new Response(JSON.stringify(response), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      });
    }) as typeof fetch;
    setAnthropicClientForTest(null);

    const page = await listSessionFileOptions('workspace_123');

    expect(requestedAfterIds).toEqual([null, 'file_999']);
    expect(page.data).toHaveLength(1001);
    expect(page.data.at(-1)?.id).toBe('file_1000');
    expect(page).toMatchObject({ first_id: 'file_0', has_more: false, last_id: 'file_1000' });
  });
});

function requestURL(input: RequestInfo | URL) {
  return typeof input === 'string' || input instanceof URL ? String(input) : input.url;
}

function fileMetadata(index: number) {
  return {
    id: `file_${index}`,
    created_at: '2026-08-24T00:00:00Z',
    filename: `file-${index}.txt`,
    mime_type: 'text/plain',
    size_bytes: index,
    type: 'file' as const,
  };
}
