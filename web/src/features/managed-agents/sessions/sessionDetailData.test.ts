import { afterEach, expect, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import '../../../test/setup';
import { sessionDetailDeltaFrames, sessionDetailScopeEvents, sessionLinkedAbortSignal } from '../api';
import { runSessionEventStreamLoop } from './sessionDetailData';

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

for (const threadId of ['', 'sthr_child']) {
  test(`${threadId || 'session'} reconnects after EOF and rescans history after subscribing`, async () => {
    const queryClient = new QueryClient();
    const controller = new AbortController();
    const calls: string[] = [];
    let listCount = 0;
    globalThis.fetch = async (input) => {
      const url = String(input);
      if (url.includes('/stream?')) {
        calls.push('stream');
        return new Response(
          new ReadableStream({
            start(stream) {
              stream.enqueue(new TextEncoder().encode(': connected\n\n'));
              stream.close();
            },
          }),
          { headers: { 'Content-Type': 'text/event-stream' } },
        );
      }
      listCount += 1;
      const page = new URL(url, 'https://oma.duck.ai').searchParams.get('page');
      calls.push(`list:${page ?? ''}`);
      if (listCount === 2) {
        setTimeout(() => controller.abort(), 0);
      }
      return new Response(
        JSON.stringify({
          data: [{ id: `sevt_${listCount}`, type: 'user.message', processed_at: '2026-08-26T13:13:00Z' }],
          next_page: null,
        }),
      );
    };

    await runSessionEventStreamLoop({
      queryClient,
      sessionId: 'sesn_123',
      workspaceId: 'workspace_123',
      threadId,
      signal: controller.signal,
      onCacheChange: () => undefined,
    });

    expect(calls).toEqual(['stream', 'list:', 'stream', 'list:']);
    expect(
      sessionDetailScopeEvents(queryClient, 'workspace_123', 'sesn_123', [threadId]).map((event) => event.id),
    ).toEqual(['sevt_1', 'sevt_2']);
  });
}

test('reconnects after a connection error and scans history once subscribed', async () => {
  const queryClient = new QueryClient();
  const controller = new AbortController();
  const calls: string[] = [];
  globalThis.fetch = async (input) => {
    const url = String(input);
    if (url.includes('/stream?')) {
      calls.push('stream');
      if (calls.length === 1) throw new Error('connection lost');
      return new Response(': connected\n\n', { headers: { 'Content-Type': 'text/event-stream' } });
    }
    calls.push('list');
    return new Response(JSON.stringify({ data: [], next_page: null }));
  };

  await runSessionEventStreamLoop({
    queryClient,
    sessionId: 'sesn_123',
    workspaceId: 'workspace_123',
    threadId: '',
    signal: controller.signal,
    onCacheChange: () => undefined,
    onHistorySynced: () => controller.abort(),
  });

  expect(calls).toEqual(['stream', 'stream', 'list']);
});

test('reconnects when the history scan fails after subscribing', async () => {
  const queryClient = new QueryClient();
  const controller = new AbortController();
  const calls: string[] = [];
  let listCount = 0;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.includes('/stream?')) {
      calls.push('stream');
      return new Response(
        new ReadableStream<Uint8Array>({
          start(stream) {
            stream.enqueue(new TextEncoder().encode(': connected\n\n'));
            if (init?.signal?.aborted) stream.close();
            else init?.signal?.addEventListener('abort', () => stream.close(), { once: true });
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      );
    }
    calls.push('list');
    listCount += 1;
    return listCount === 1
      ? new Response('temporary failure', { status: 500 })
      : new Response(JSON.stringify({ data: [{ id: 'sevt_recovered', type: 'user.message' }], next_page: null }));
  };

  await runSessionEventStreamLoop({
    queryClient,
    sessionId: 'sesn_123',
    workspaceId: 'workspace_123',
    threadId: '',
    signal: controller.signal,
    onCacheChange: () => undefined,
    onHistorySynced: () => controller.abort(),
  });

  expect(calls).toEqual(['stream', 'list', 'stream', 'list']);
  expect(sessionDetailScopeEvents(queryClient, 'workspace_123', 'sesn_123', ['']).map((event) => event.id)).toEqual([
    'sevt_recovered',
  ]);
});

test('ends an idle stream after its timeout', async () => {
  const controller = new AbortController();
  const linked = sessionLinkedAbortSignal(controller.signal, 5);
  linked.touch();
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(linked.signal.aborted).toBe(true);
  expect(linked.signal.reason).toEqual(new Error('Session event stream timed out'));
  linked.dispose();
});

test('times out while waiting for an SSE connection to open', async () => {
  const controller = new AbortController();
  const linked = sessionLinkedAbortSignal(controller.signal, 5);
  await new Promise((resolve) => setTimeout(resolve, 20));
  expect(linked.signal.aborted).toBe(true);
  expect(linked.signal.reason).toEqual(new Error('Session event stream timed out'));
  linked.dispose();
});

test('keeps a final SSE event while a later history page is pending and removes an unfinished preview on disconnect', async () => {
  const queryClient = new QueryClient();
  const controller = new AbortController();
  const calls: string[] = [];
  const final = {
    id: 'sevt_answer',
    type: 'agent.message',
    processed_at: '2026-08-26T13:13:01Z',
    content: [{ type: 'text', text: 'Complete answer' }],
  };
  let stream: ReadableStreamDefaultController<Uint8Array> | undefined;
  let finishSecondPage: ((response: Response) => void) | undefined;
  const frame = (event: object) => `data: ${JSON.stringify(event)}\n\n`;
  globalThis.fetch = async (input, init) => {
    const url = String(input);
    if (url.includes('/stream?')) {
      calls.push('stream');
      return new Response(
        new ReadableStream<Uint8Array>({
          start(current) {
            stream = current;
            init?.signal?.addEventListener('abort', () => current.close(), { once: true });
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      );
    }
    const page = new URL(url, 'https://oma.duck.ai').searchParams.get('page');
    calls.push(`list:${page ?? ''}`);
    if (!page) {
      return new Response(
        JSON.stringify({
          data: [{ id: 'sevt_first', type: 'user.message', processed_at: '2026-08-26T13:13:00Z' }],
          next_page: 'next',
        }),
      );
    }
    stream?.enqueue(
      new TextEncoder().encode(
        [
          frame({ type: 'event_start', event: { id: 'sevt_answer', type: 'agent.message' } }),
          frame({
            type: 'event_delta',
            event_id: 'sevt_answer',
            delta: { index: 0, content: { type: 'text', text: 'Part' } },
          }),
          frame({ type: 'event_start', event: { id: 'sevt_unfinished', type: 'agent.message' } }),
          frame(final),
        ].join(''),
      ),
    );
    return new Promise<Response>((resolve) => {
      finishSecondPage = resolve;
    });
  };

  await runSessionEventStreamLoop({
    queryClient,
    sessionId: 'sesn_123',
    workspaceId: 'workspace_123',
    threadId: '',
    signal: controller.signal,
    onCacheChange: () => {
      const events = sessionDetailScopeEvents(queryClient, 'workspace_123', 'sesn_123', ['']);
      if (events.some((event) => event.id === final.id && event.processed_at) && finishSecondPage) {
        finishSecondPage(
          new Response(
            JSON.stringify({
              data: [{ id: 'sevt_second', type: 'system.message', processed_at: '2026-08-26T13:13:02Z' }],
              next_page: null,
            }),
          ),
        );
        finishSecondPage = undefined;
      }
    },
    onHistorySynced: () => controller.abort(),
  });

  expect(calls).toEqual(['stream', 'list:', 'list:next']);
  const events = sessionDetailScopeEvents(queryClient, 'workspace_123', 'sesn_123', ['']);
  expect(events.map((event) => event.id)).toEqual(['sevt_first', 'sevt_answer', 'sevt_second']);
  expect(events[1]).toEqual(final);
  expect(sessionDetailDeltaFrames(queryClient, 'workspace_123', 'sesn_123', [''])).toEqual({});
});
