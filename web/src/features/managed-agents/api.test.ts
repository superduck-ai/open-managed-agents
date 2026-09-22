import { afterEach, describe, expect, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { setAnthropicClientForTest } from '@/shared/api/anthropic';
import {
  addSessionFileResource,
  listSessionFileOptions,
  mergeSessionEventCache,
  mergeSessionEventsById,
  mergeSessionStreamFrame,
  reconcileIncompleteSessionStreamEvents,
  sessionDetailDeltaFrames,
  sessionDetailScopeEvents,
  sessionIncompleteStreamEventIds,
  syncSessionEventHistory,
} from './api';
import { buildSessionEventEntries } from './sessions/sessionTraceModel';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
});

describe('managed agents API', () => {
  test('replaces a preview with the same ID as soon as the final agent message arrives', () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    const createdAt = '2026-08-26T13:13:00Z';

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      created_at: createdAt,
      processed_at: createdAt,
      event: { id: 'sevt_final', type: 'agent.message' },
    });

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, [''])[0]).toMatchObject({
      id: 'sevt_final',
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

    const visibleBeforeIdle = buildSessionEventEntries(
      sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']),
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );
    expect(visibleBeforeIdle.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_final',
    ]);
    expect(sessionDetailDeltaFrames(queryClient, workspaceId, sessionId, [''])).toEqual({});

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

  test('places the final message in persisted arrival order when idle has the same timestamp', () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    const startAt = '2026-08-26T13:13:00Z';
    const messageAt = '2026-08-26T13:13:01Z';
    const idleAt = '2026-08-26T13:13:05Z';

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_model_start',
      type: 'span.model_request_start',
      created_at: startAt,
      processed_at: startAt,
    });
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      created_at: messageAt,
      event: { id: 'sevt_final', type: 'agent.message' },
    });
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_model_end',
      type: 'span.model_request_end',
      model_request_start_id: 'sevt_model_start',
      event_ids: ['sevt_final'],
      created_at: idleAt,
      processed_at: idleAt,
    });
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_idle',
      type: 'session.status_idle',
      created_at: idleAt,
      processed_at: idleAt,
    });

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']).map((event) => event.id)).toEqual([
      'sevt_model_start',
      'sevt_model_end',
      'sevt_idle',
    ]);

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_final',
      type: 'agent.message',
      created_at: messageAt,
      processed_at: idleAt,
      content: [{ type: 'text', text: 'Final answer' }],
    });

    const events = sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']);
    expect(events.map((event) => event.id)).toEqual(['sevt_model_start', 'sevt_model_end', 'sevt_idle', 'sevt_final']);

    const entries = buildSessionEventEntries(events, 'transcript', Date.parse(startAt), undefined, {
      platformTranscriptFiltering: true,
    });
    const message = entries.find((entry) => entry.kind === 'message');
    expect(message?.bracketStartMs).toBe(Date.parse(startAt));
    expect(message?.bracketEndMs).toBe(Date.parse(idleAt));
  });

  test('does not reconcile a thinking preview with a final agent message', () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    const createdAt = '2026-08-26T13:13:00Z';

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      created_at: createdAt,
      processed_at: createdAt,
      event: { id: 'sevt_thinking_preview', type: 'agent.thinking' },
    });
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_final',
      type: 'agent.message',
      created_at: createdAt,
      processed_at: createdAt,
      content: [{ type: 'text', text: 'Final answer' }],
    });

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']).map((event) => event.id)).toEqual([
      'sevt_thinking_preview',
      'sevt_final',
    ]);
  });

  test('reconciles an incomplete preview after idle history sync', async () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    const createdAt = '2026-08-26T13:13:00Z';

    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      created_at: createdAt,
      event: { id: 'sevt_preview', type: 'agent.message' },
    });
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      id: 'sevt_idle',
      type: 'session.status_idle',
      created_at: createdAt,
      processed_at: createdAt,
    });
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          data: [
            {
              id: 'sevt_final',
              type: 'agent.message',
              created_at: createdAt,
              processed_at: createdAt,
              content: [{ type: 'text', text: 'Final answer' }],
            },
            {
              id: 'sevt_idle',
              type: 'session.status_idle',
              created_at: createdAt,
              processed_at: createdAt,
            },
          ],
          next_page: null,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )) as typeof fetch;

    await reconcileIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId);

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']).map((event) => event.id)).toEqual([
      'sevt_final',
      'sevt_idle',
    ]);
    expect(sessionDetailDeltaFrames(queryClient, workspaceId, sessionId, [''])).toEqual({});
  });

  test('keeps incomplete previews when idle history sync fails', async () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      event: { id: 'sevt_preview', type: 'agent.message' },
    });
    globalThis.fetch = (async () => new Response('Unavailable', { status: 503 })) as typeof fetch;

    await expect(reconcileIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId)).rejects.toBeDefined();

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']).map((event) => event.id)).toEqual([
      'sevt_preview',
    ]);
  });

  test('only reconciles previews that existed when idle arrived', async () => {
    const queryClient = new QueryClient();
    const workspaceId = 'workspace_123';
    const sessionId = 'sesn_123';
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      event: { id: 'sevt_stale', type: 'agent.message' },
    });
    const stalePreviewIds = sessionIncompleteStreamEventIds(queryClient, workspaceId, sessionId);
    mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', {
      type: 'event_start',
      event: { id: 'sevt_new', type: 'agent.message' },
    });
    globalThis.fetch = (async () =>
      new Response(JSON.stringify({ data: [], next_page: null }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })) as typeof fetch;

    await reconcileIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, '', undefined, stalePreviewIds);

    expect(sessionDetailScopeEvents(queryClient, workspaceId, sessionId, ['']).map((event) => event.id)).toEqual([
      'sevt_new',
    ]);
    expect(Object.keys(sessionDetailDeltaFrames(queryClient, workspaceId, sessionId, ['']))).toEqual(['sevt_new']);
  });

  test('preserves a same-timestamp user turn before its agent error', () => {
    const createdAt = '2026-08-28T01:01:29Z';
    const entries = buildSessionEventEntries(
      [
        {
          id: 'sevt_3qzvoDj1lEhf4xsGJoTHSxjA',
          type: 'user.message',
          created_at: createdAt,
          processed_at: createdAt,
          content: [{ type: 'text', text: '帮我把这个信息写成一个txt文件。' }],
        },
        {
          id: 'sevt_7ab0bf46aa79a2d26bfdce33c3e2e400',
          type: 'agent.message',
          created_at: createdAt,
          processed_at: createdAt,
          content: [{ type: 'text', text: 'An error occurred while executing Claude Code.' }],
        },
      ],
      'transcript',
      Date.parse(createdAt),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(entries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_3qzvoDj1lEhf4xsGJoTHSxjA',
      'sevt_7ab0bf46aa79a2d26bfdce33c3e2e400',
    ]);
  });

  test('sorts cached events by processed time even when delivery runs backwards', () => {
    const events = [
      {
        id: 'sevt_user_inserted_first',
        type: 'user.message',
        created_at: '2026-08-28T01:01:30Z',
        content: [{ type: 'text', text: 'First by database id' }],
      },
      {
        id: 'sevt_agent_inserted_second',
        type: 'agent.message',
        created_at: '2026-08-28T01:01:29Z',
        content: [{ type: 'text', text: 'Second by database id' }],
      },
    ];

    expect(mergeSessionEventsById(events).map((event) => event.id)).toEqual([
      'sevt_agent_inserted_second',
      'sevt_user_inserted_first',
    ]);
    expect(
      buildSessionEventEntries(events, 'transcript', Date.parse('2026-08-28T01:01:29Z'), undefined, {
        platformTranscriptFiltering: true,
      }).map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id)),
    ).toEqual(['sevt_user_inserted_first', 'sevt_agent_inserted_second']);
  });

  test('keeps content-block entries at their source event position', () => {
    const entries = buildSessionEventEntries(
      [
        {
          id: 'sevt_user_first',
          type: 'user.message',
          created_at: '2026-08-28T01:01:30Z',
          content: [{ type: 'text', text: 'First' }],
        },
        {
          id: 'sevt_agent_blocks',
          type: 'agent.message',
          created_at: '2026-08-28T01:01:31Z',
          content: [
            { type: 'thinking', thinking: 'Working' },
            { type: 'tool_use', id: 'toolu_read', name: 'Read', input: { path: '/workspace/file.txt' } },
          ],
        },
        {
          id: 'sevt_user_second',
          type: 'user.message',
          created_at: '2026-08-28T01:01:32Z',
          content: [{ type: 'text', text: 'Second' }],
        },
      ],
      'transcript',
      Date.parse('2026-08-28T01:01:30Z'),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(entries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_user_first',
      'sevt_agent_blocks-thinking-0',
      'toolu_read',
      'sevt_user_second',
    ]);
  });

  test('sorts replacements and appended events by processed time', () => {
    const first = mergeSessionEventCache(undefined, [
      {
        id: 'sevt_first',
        type: 'user.message',
        created_at: '2026-08-28T01:01:30Z',
        processed_at: null,
        is_streaming: true,
        content: 'draft',
      },
      { id: 'sevt_second', type: 'agent.message', created_at: '2026-08-28T01:01:29Z', content: 'second' },
    ]);
    const updated = mergeSessionEventCache(first, [
      {
        id: 'sevt_first',
        type: 'user.message',
        created_at: '2026-08-28T01:01:30Z',
        processed_at: '2026-08-28T01:01:30Z',
        content: 'final',
      },
      { id: 'sevt_third', type: 'session.status_idle', created_at: '2026-08-28T01:01:28Z' },
    ]);

    expect(updated.events.map((event) => event.id)).toEqual(['sevt_third', 'sevt_second', 'sevt_first']);
    expect(updated.events[2]?.content).toBe('final');
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

  test('does not reorder a non-duplicate idle result around an agent message', () => {
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
      'sevt_idle',
      'sevt_agent',
    ]);
  });

  test('keeps a tool batch at the position of its first tool call', () => {
    const entries = buildSessionEventEntries(
      [
        { id: 'sevt_model_start', type: 'span.model_request_start', created_at: '2026-08-28T01:01:29Z' },
        {
          id: 'sevt_tool_first',
          type: 'agent.tool_use',
          created_at: '2026-08-28T01:01:30Z',
          name: 'Read',
          input: { path: '/workspace/first.txt' },
        },
        {
          id: 'sevt_agent_between_tools',
          type: 'agent.message',
          created_at: '2026-08-28T01:01:31Z',
          content: [{ type: 'text', text: 'Between tool calls' }],
        },
        {
          id: 'sevt_tool_second',
          type: 'agent.tool_use',
          created_at: '2026-08-28T01:01:32Z',
          name: 'Read',
          input: { path: '/workspace/second.txt' },
        },
        {
          id: 'sevt_model_end',
          type: 'span.model_request_end',
          created_at: '2026-08-28T01:01:33Z',
          model_request_start_id: 'sevt_model_start',
          event_ids: ['sevt_agent_between_tools'],
          tool_use_ids: ['sevt_tool_first', 'sevt_tool_second'],
        },
      ],
      'transcript',
      Date.parse('2026-08-28T01:01:29Z'),
      undefined,
      { platformTranscriptFiltering: true },
    );

    expect(entries.map((entry) => entry.kind)).toEqual(['tool_batch', 'message']);
    expect(entries.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_tool_first',
      'sevt_agent_between_tools',
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

  test('deduplicates idle results only within the current turn across either arrival order', () => {
    const createdAt = '2026-08-26T13:13:00Z';
    const agentThenIdle = buildSessionEventEntries(
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
    const idleThenAgent = buildSessionEventEntries(
      [
        {
          id: 'sevt_idle_first',
          type: 'session.status_idle',
          created_at: createdAt,
          result: 'Final answer',
        },
        {
          id: 'sevt_agent_after_idle',
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
    const separateTurns = buildSessionEventEntries(
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

    expect(agentThenIdle.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent',
    ]);
    expect(idleThenAgent.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
      'sevt_agent_after_idle',
    ]);
    expect(separateTurns.map((entry) => ('traceEntry' in entry ? entry.traceEntry.rawEventId : entry.id))).toEqual([
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

  test('adds a file through the session resource endpoint', async () => {
    let capturedInput: RequestInfo | URL = '';
    let capturedInit: RequestInit | undefined;
    globalThis.fetch = (async (input, init) => {
      capturedInput = input;
      capturedInit = init;
      return new Response(
        JSON.stringify({
          id: 'sesrsc_123',
          created_at: '2026-08-30T00:00:00Z',
          file_id: 'file_123',
          mount_path: '/reports/input.csv',
          type: 'file',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      );
    }) as typeof fetch;
    setAnthropicClientForTest(null);

    await addSessionFileResource(
      'sesn_123',
      { fileId: ' file_123 ', mountPath: ' reports/input.csv ' },
      'workspace_123',
    );

    expect(requestURL(capturedInput)).toBe('/v1/sessions/sesn_123/resources?beta=true');
    expect(JSON.parse(String(capturedInit?.body))).toEqual({
      file_id: 'file_123',
      mount_path: '/reports/input.csv',
      type: 'file',
    });
    expect(new Headers(capturedInit?.headers).get('x-workspace-id')).toBe('workspace_123');
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

describe('model request lifecycle', () => {
  test('history establishes equal-timestamp order when live events overlap paginated loading', async () => {
    const client = new QueryClient();
    const events = ['z', 'b', 'a'].map((id) => ({ id, type: 'agent.message', processed_at: '2026-09-21T00:00:00Z' }));
    mergeSessionStreamFrame(client, 'w', 's', '', events[2]);
    let page = 0;
    globalThis.fetch = (async () => {
      page++;
      return Response.json(
        page === 1 ? { data: events.slice(0, 2), next_page: 'next' } : { data: events.slice(2), next_page: null },
      );
    }) as typeof fetch;
    await syncSessionEventHistory({ queryClient: client, workspaceId: 'w', sessionId: 's', force: true });
    expect(sessionDetailScopeEvents(client, 'w', 's', ['']).map((event) => event.id)).toEqual(['z', 'b', 'a']);
  });

  test('preserves server order for equal timestamps instead of sorting random IDs', () => {
    const events = ['z', 'b', 'a'].map((id) => ({ id, type: 'agent.message', processed_at: '2026-09-21T00:00:00Z' }));
    let cache: ReturnType<typeof mergeSessionEventCache> | undefined;
    for (const event of events) cache = mergeSessionEventCache(cache, [event]);
    expect(cache?.events).toEqual(events);
    expect(mergeSessionEventCache(undefined, events).events).toEqual(events);
  });

  test('closes only the previews listed by an end and rejects their late deltas', () => {
    const client = new QueryClient();
    const send = (event: Parameters<typeof mergeSessionStreamFrame>[4]) =>
      mergeSessionStreamFrame(client, 'w', 's', '', event);
    for (const id of ['answer-a', 'answer-b']) send({ type: 'event_start', event: { id, type: 'agent.message' } });
    send({ id: 'end-a', type: 'span.model_request_end', model_request_start_id: 'start-a', event_ids: ['answer-a'] });
    send({ type: 'event_start', event: { id: 'answer-a', type: 'agent.message' } });
    send({ type: 'event_delta', event_id: 'answer-a', delta: { content: { text: 'late' } } });
    expect([...sessionIncompleteStreamEventIds(client, 'w', 's')]).toEqual(['answer-b']);
    expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 's', ['']))).toEqual(['answer-b']);
  });

  test('live arrival and history produce identical intervals for concurrent requests and late messages', () => {
    const start = (id: string, thread: string, at: string) => ({
      id,
      request_id: id,
      type: 'span.model_request_start',
      session_thread_id: thread,
      processed_at: at,
    });
    const a = start('a', 'main', '2026-09-21T00:00:01.000001Z');
    const b = start('b', 'main', '2026-09-21T00:00:01.000002Z');
    const child = start('c', 'child', '2026-09-21T00:00:01.000003Z');
    const answer = {
      id: 'answer',
      type: 'agent.message',
      session_thread_id: 'main',
      model_request_start_id: 'a',
      processed_at: '2026-09-21T00:00:05Z',
      content: [{ type: 'text', text: 'Done' }],
    };
    const end = {
      id: 'end-a',
      type: 'span.model_request_end',
      session_thread_id: 'main',
      model_request_start_id: 'a',
      processed_at: '2026-09-21T00:00:04.000001Z',
      usage: { input_tokens: 12, output_tokens: 4 },
    };
    const arrival = [answer, child, b, end, a];
    let cache: ReturnType<typeof mergeSessionEventCache> | undefined;
    for (const event of arrival) cache = mergeSessionEventCache(cache, [event]);
    const history = mergeSessionEventCache(undefined, [a, b, child, end, answer]);
    expect(cache?.events).toEqual(history.events);
    const entries = buildSessionEventEntries(cache!.events, 'transcript');
    expect(entries.find((entry) => entry.kind === 'message')).toMatchObject({
      bracketId: 'a',
      inferenceMs: 3000,
      bracketOpen: false,
    });
    expect(buildSessionEventEntries(history.events, 'transcript')).toEqual(entries);
  });
});
