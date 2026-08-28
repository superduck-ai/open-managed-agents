import { afterEach, describe, expect, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { setAnthropicClientForTest } from '@/shared/api/anthropic';
import { listSessionFileOptions, mergeSessionStreamFrame, sessionBudgetUpdate, sessionDetailScopeEvents } from './api';
import { buildSessionEventEntries } from './sessions/sessionTraceModel';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
});

describe('managed agents API', () => {
  test('preserves preview time and removes an orphaned preview when the session idles', () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    const createdAt = '2026-08-26T13:13:00Z';

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      created_at: createdAt,
      processed_at: createdAt,
      event: { id: 'sevt_preview', type: 'agent.message' },
    });

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, [''])[0]).toMatchObject({
      id: 'sevt_preview',
      created_at: createdAt,
      processed_at: null,
      is_streaming: true,
    });

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_final',
      type: 'agent.message',
      created_at: createdAt,
      processed_at: createdAt,
      content: [{ type: 'text', text: 'Final answer' }],
    });
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_idle',
      type: 'session.status_idle',
      created_at: createdAt,
      processed_at: createdAt,
      result: 'Final answer',
    });

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']).map((event) => event.id)).toEqual([
      'sevt_final',
      'sevt_idle',
    ]);
  });

  test('omits an idle result that duplicates an agent message with the same timestamp', () => {
    const createdAt = '2026-08-26T13:13:00Z';
    const entries = buildSessionEventEntries(
      [
        {
          id: 'sevt_idle',
          type: 'session.status_idle',
          created_at: createdAt,
          processed_at: createdAt,
          result: 'Final answer',
        },
        {
          id: 'sevt_agent',
          type: 'agent.message',
          created_at: createdAt,
          processed_at: createdAt,
          content: [{ type: 'text', text: 'Final answer' }],
        },
      ],
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(entries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent',
    ]);
  });

  test('orders a non-duplicate idle result after an agent message with the same timestamp', () => {
    const createdAt = '2026-08-26T13:13:00Z';
    const entries = buildSessionEventEntries(
      [
        {
          id: 'sevt_idle',
          type: 'session.status_idle',
          created_at: createdAt,
          result: 'Run completed',
        },
        {
          id: 'sevt_agent',
          type: 'agent.message',
          created_at: createdAt,
          content: [{ type: 'text', text: 'Final answer' }],
        },
      ],
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(entries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent',
      'sevt_idle',
    ]);
  });

  test('keeps a generic result that matches the preceding agent message', () => {
    const createdAt = '2026-08-26T13:13:00Z';
    const entries = buildSessionEventEntries(
      [
        {
          id: 'sevt_agent',
          type: 'agent.message',
          created_at: createdAt,
          content: [{ type: 'text', text: 'Final answer' }],
        },
        { id: 'sevt_result', type: 'result', created_at: '2026-08-26T13:13:01Z', result: 'Final answer' },
      ],
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(entries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent',
      'sevt_result',
    ]);
  });

  test('uses canonical agent and user families as duplicate-result boundaries', () => {
    const createdAt = '2026-08-26T13:13:00Z';
    const agentEntries = buildSessionEventEntries(
      [
        { id: 'sevt_agent', type: 'agent', created_at: createdAt, content: [{ type: 'text', text: 'Final answer' }] },
        {
          id: 'sevt_idle',
          type: 'session.status_idle',
          created_at: '2026-08-26T13:13:01Z',
          result: 'Final answer',
        },
      ],
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );
    const userEntries = buildSessionEventEntries(
      [
        {
          id: 'sevt_agent_message',
          type: 'agent.message',
          created_at: createdAt,
          content: [{ type: 'text', text: 'Earlier answer' }],
        },
        { id: 'sevt_user', type: 'user', created_at: '2026-08-26T13:13:01Z', content: 'Follow-up' },
        {
          id: 'sevt_idle_after_user',
          type: 'session.status_idle',
          created_at: '2026-08-26T13:13:02Z',
          result: 'Earlier answer',
        },
      ],
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(agentEntries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent',
    ]);
    expect(userEntries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent_message',
      'sevt_user',
      'sevt_idle_after_user',
    ]);
  });

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

describe('sessionBudgetUpdate', () => {
  test('sends new budget when amount provided', () => {
    const values = { budgetAmount: '125', budgetInitiallySet: false } as never;
    expect(sessionBudgetUpdate(values)).toEqual({
      budget: { type: 'limit', max_list_cost: { amount: '125', currency: 'USD' } },
    });
  });

  test('removes budget when cleared and was set', () => {
    const values = { budgetAmount: '', budgetInitiallySet: true } as never;
    expect(sessionBudgetUpdate(values)).toEqual({ budget: null });
  });

  test('omits budget when empty and was never set', () => {
    const values = { budgetAmount: '', budgetInitiallySet: false } as never;
    expect(sessionBudgetUpdate(values)).toEqual({});
  });
});
