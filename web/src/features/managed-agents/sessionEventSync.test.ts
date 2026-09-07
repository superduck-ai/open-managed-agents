import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createElement, type ReactNode } from 'react';
import { resetTestDom } from '../../test/setup';
import { runSessionEventStreamLoop, useSessionDetailEventData } from './sessions/sessionDetailData';
import {
  cleanupIncompleteSessionStreamEvents,
  mergeSessionEventCache,
  mergeSessionStreamFrame,
  sessionDetailDeltaFrames,
  sessionDetailScopeEvents,
  sessionDetailEventCacheKey,
  streamSessionEvents,
  syncSessionEventHistory,
} from './api';
import { setAnthropicClientForTest } from '@/shared/api/anthropic';

const originalFetch = globalThis.fetch;
const originalWindow = Object.getOwnPropertyDescriptor(globalThis, 'window');
const { cleanup, renderHook, waitFor } = await import('@testing-library/react');

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
  if (originalWindow) Object.defineProperty(globalThis, 'window', originalWindow);
  else Reflect.deleteProperty(globalThis, 'window');
});

test('mounting a non-live session refreshes the cached usage history', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/w/sessions/archived-usage');
  const client = new QueryClient();
  const old = {
    id: 'usage_old',
    type: 'session.usage',
    processed_at: '2026-09-07T00:00:00.000001Z',
    usage: { output_tokens: 3 },
  };
  const latest = { id: 'usage_latest', type: 'session.usage', processed_at: '2026-09-07T00:00:00.000002Z', usage: {} };
  client.setQueryData(sessionDetailEventCacheKey('w', 'archived-usage'), mergeSessionEventCache(undefined, [old]));
  const paths: string[] = [];
  globalThis.fetch = (async (input) => {
    paths.push(input instanceof Request ? input.url : String(input));
    return Response.json({ data: [latest], next_page: null });
  }) as typeof fetch;
  const threads: Parameters<typeof useSessionDetailEventData>[0]['threads'] = [];
  const view = renderHook(
    () =>
      useSessionDetailEventData({
        sessionId: 'archived-usage',
        workspaceId: 'w',
        threads,
        includeArchivedThreads: false,
        live: false,
        refreshKey: 0,
      }),
    {
      wrapper: ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children),
    },
  );
  await waitFor(() => expect(view.result.current.events).toEqual([latest]));
  expect(paths).toHaveLength(1);
  expect(paths[0]).not.toContain('/stream');
  view.unmount();
  client.clear();
});

test('complete paginated history removes only unchanged baseline events while preserving concurrent live changes', async () => {
  const client = new QueryClient();
  const oldTime = '2026-09-07T00:00:00.000001Z';
  const newTime = '2026-09-07T00:00:00.000002Z';
  const obsolete = { id: 'orphan-tool', type: 'agent.tool_use', processed_at: oldTime };
  const kept = { id: 'kept', type: 'agent.message', processed_at: oldTime };
  const changed = { id: 'changed', type: 'agent.message', processed_at: oldTime, content: 'old' };
  for (const event of [obsolete, kept, changed, { id: 'pending', type: 'user.message', processed_at: null }])
    mergeSessionStreamFrame(client, 'w', 'snapshot', '', event);
  for (const [workspace, session, scope] of [
    ['w', 'snapshot', 'child'],
    ['other', 'snapshot', ''],
    ['w', 'other', ''],
  ])
    mergeSessionStreamFrame(client, workspace, session, scope, obsolete);
  mergeSessionStreamFrame(client, 'w', 'snapshot', '', {
    type: 'event_start',
    event: { id: 'preview', type: 'agent.message' },
  });
  const secondPage = Promise.withResolvers<Response>();
  const paths: string[] = [];
  globalThis.fetch = (async (input) => {
    paths.push(input instanceof Request ? input.url : String(input));
    return paths.length === 1 ? Response.json({ data: [kept], next_page: 'opaque-page-two' }) : secondPage.promise;
  }) as typeof fetch;
  const syncing = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'snapshot',
  });
  await waitFor(() => expect(paths).toHaveLength(2));
  expect(sessionDetailScopeEvents(client, 'w', 'snapshot', ['']).some((event) => event.id === obsolete.id)).toBe(true);
  const updated = { ...changed, processed_at: newTime, content: 'live update' };
  const applied = { id: 'pending', type: 'user.message', processed_at: newTime };
  const live = { id: 'live', type: 'agent.message', processed_at: newTime };
  for (const event of [updated, applied, live]) mergeSessionStreamFrame(client, 'w', 'snapshot', '', event);
  secondPage.resolve(Response.json({ data: [changed, { ...applied, processed_at: null }], next_page: null }));
  await syncing;
  const events = sessionDetailScopeEvents(client, 'w', 'snapshot', ['']);
  expect(events.map((event) => event.id).sort()).toEqual(['changed', 'kept', 'live', 'pending', 'preview']);
  expect(events.find((event) => event.id === 'changed')).toEqual(updated);
  expect(events.find((event) => event.id === 'pending')).toEqual(applied);
  expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 'snapshot', ['']))).toEqual(['preview']);
  for (const [workspace, session, scope] of [
    ['w', 'snapshot', 'child'],
    ['other', 'snapshot', ''],
    ['w', 'other', ''],
  ])
    expect(sessionDetailScopeEvents(client, workspace, session, [scope]).map((event) => event.id)).toEqual([
      obsolete.id,
    ]);
  expect(new URL(paths[1], 'http://local').searchParams.get('page')).toBe('opaque-page-two');
  client.clear();
});

test('a successful snapshot refreshes same-time public projections and upgrades previews without downgrading processed facts', async () => {
  const client = new QueryClient();
  const at = '2026-09-07T00:00:00Z';
  const thinking = { id: 'thinking', type: 'agent.thinking', processed_at: at };
  const processed = { id: 'processed', type: 'user.message', processed_at: at };
  mergeSessionStreamFrame(client, 'w', 'projection', '', { ...thinking, content: 'old private projection' });
  mergeSessionStreamFrame(client, 'w', 'projection', '', processed);
  mergeSessionStreamFrame(client, 'w', 'projection', '', { id: 'pending', type: 'user.message', processed_at: null });
  mergeSessionStreamFrame(client, 'w', 'projection', '', {
    type: 'event_start',
    event: { id: 'preview', type: 'agent.message' },
  });
  const final = { id: 'preview', type: 'agent.message', processed_at: at, content: 'complete' };
  const applied = { id: 'pending', type: 'user.message', processed_at: at };
  globalThis.fetch = (async () =>
    Response.json({
      data: [thinking, { ...processed, processed_at: null }, final, applied],
      next_page: null,
    })) as typeof fetch;
  await syncSessionEventHistory({ queryClient: client, workspaceId: 'w', sessionId: 'projection' });
  const events = sessionDetailScopeEvents(client, 'w', 'projection', ['']);
  for (const event of [thinking, processed, final, applied])
    expect(events.find((item) => item.id === event.id)).toEqual(event);
  expect(sessionDetailDeltaFrames(client, 'w', 'projection', [''])).toEqual({});
  client.clear();
});

test.each(['before', 'during'])(
  'a waiter cancelled %s another history request exits without cancelling its owner',
  async (when) => {
    const client = new QueryClient();
    const ownerController = new AbortController();
    const waiterController = new AbortController();
    const response = Promise.withResolvers<Response>();
    let calls = 0;
    globalThis.fetch = (async () => {
      calls += 1;
      return response.promise;
    }) as typeof fetch;
    const owner = syncSessionEventHistory({
      queryClient: client,
      workspaceId: 'w',
      sessionId: `cancel-waiter-${when}`,
      signal: ownerController.signal,
    });
    if (when === 'before') waiterController.abort(new Error('waiter cancelled'));
    const waiter = syncSessionEventHistory({
      queryClient: client,
      workspaceId: 'w',
      sessionId: `cancel-waiter-${when}`,
      signal: waiterController.signal,
    });
    if (when === 'during') waiterController.abort(new Error('waiter cancelled'));
    try {
      await expect(waiter).rejects.toThrow('waiter cancelled');
      expect(calls).toBe(1);
      expect(ownerController.signal.aborted).toBe(false);
    } finally {
      response.resolve(Response.json({ data: [], next_page: null }));
      await owner;
      client.clear();
    }
  },
);

test.each(['http', 'cancel', 'schema', 'missing-type', 'numeric-processed-at', 'object-processed-at'])(
  'an incomplete %s history snapshot never removes baseline events',
  async (failure) => {
    const client = new QueryClient();
    const controller = new AbortController();
    const old = { id: 'baseline', type: 'agent.message', processed_at: '2026-09-07T00:00:00Z' };
    mergeSessionStreamFrame(client, 'w', `failed-${failure}`, '', old);
    let calls = 0;
    globalThis.fetch = (async () => {
      if (++calls === 1) return Response.json({ data: [], next_page: 'page-two' });
      if (failure === 'http') return new Response('failed', { status: 503 });
      if (failure === 'schema') return Response.json({ data: null });
      if (failure === 'missing-type') return Response.json({ data: [{ id: 'invalid' }], next_page: null });
      if (failure === 'numeric-processed-at' || failure === 'object-processed-at') {
        return Response.json({
          data: [{ id: 'invalid', type: 'agent.message', processed_at: failure === 'numeric-processed-at' ? 1 : {} }],
          next_page: null,
        });
      }
      controller.abort(new Error('cancelled snapshot'));
      return Response.json({ data: [], next_page: null });
    }) as typeof fetch;
    await expect(
      syncSessionEventHistory({
        queryClient: client,
        workspaceId: 'w',
        sessionId: `failed-${failure}`,
        signal: controller.signal,
      }),
    ).rejects.toBeDefined();
    expect(sessionDetailScopeEvents(client, 'w', `failed-${failure}`, [''])).toEqual([old]);
    client.clear();
  },
);

test('concurrent callers share a fresh successor to the in-flight snapshot instead of reviving removed rows', async () => {
  const client = new QueryClient();
  const old = { id: 'old', type: 'agent.message', processed_at: '2026-09-07T00:00:00.000001Z' };
  const latest = { id: 'latest', type: 'agent.message', processed_at: '2026-09-07T00:00:00.000002Z' };
  mergeSessionStreamFrame(client, 'w', 'serialized', '', old);
  const firstPage = Promise.withResolvers<Response>();
  let calls = 0;
  globalThis.fetch = (async () =>
    ++calls === 1 ? firstPage.promise : Response.json({ data: [latest], next_page: null })) as typeof fetch;
  const first = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'serialized',
  });
  const second = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'serialized',
  });
  const third = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'serialized',
  });
  expect(calls).toBe(1);
  firstPage.resolve(Response.json({ data: [old], next_page: null }));
  await Promise.all([first, second, third]);
  expect(calls).toBe(2);
  expect(sessionDetailScopeEvents(client, 'w', 'serialized', [''])).toEqual([latest]);
  client.clear();
});

test('a cancelled follower of a shared successor exits without cancelling the successor owner', async () => {
  const client = new QueryClient();
  const firstPage = Promise.withResolvers<Response>();
  const nextPage = Promise.withResolvers<Response>();
  const ownerController = new AbortController();
  const followerController = new AbortController();
  let calls = 0;
  globalThis.fetch = (async () => (++calls === 1 ? firstPage.promise : nextPage.promise)) as typeof fetch;
  const first = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'cancel-successor',
  });
  const owner = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'cancel-successor',
    signal: ownerController.signal,
  });
  const follower = syncSessionEventHistory({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'cancel-successor',
    signal: followerController.signal,
  });
  firstPage.resolve(Response.json({ data: [], next_page: null }));
  try {
    await waitFor(() => expect(calls).toBe(2));
    followerController.abort(new Error('follower cancelled'));
    await expect(follower).rejects.toThrow('follower cancelled');
    expect(ownerController.signal.aborted).toBe(false);
    expect(calls).toBe(2);
  } finally {
    nextPage.resolve(Response.json({ data: [], next_page: null }));
    await Promise.all([first, owner]);
    client.clear();
  }
});

test('a final from history seals a delayed preview from the buffered stream', () => {
  const client = new QueryClient();
  const final = { id: 'same', type: 'agent.message', processed_at: '2026-09-07T00:00:00.000002Z', content: 'final' };
  mergeSessionStreamFrame(client, 'w', 's', '', final);
  mergeSessionStreamFrame(client, 'w', 's', '', { type: 'event_start', event: { id: 'same', type: 'agent.message' } });
  mergeSessionStreamFrame(client, 'w', 's', '', {
    type: 'event_delta',
    event_id: 'same',
    delta: { content: { type: 'text', text: 'late' } },
  });
  expect(sessionDetailScopeEvents(client, 'w', 's', [''])).toEqual([final]);
  expect(sessionDetailDeltaFrames(client, 'w', 's', [''])).toEqual({});
});

test.each([true, false, null, undefined])(
  'live request end closes only unfinished previews regardless of is_error=%p',
  (isError) => {
    const client = new QueryClient();
    const start = { type: 'event_start', event: { id: 'orphan', type: 'agent.message' } };
    mergeSessionStreamFrame(client, 'w', 's', '', start);
    mergeSessionStreamFrame(client, 'w', 's', 'child', start);
    const final = {
      id: 'complete',
      type: 'agent.message',
      processed_at: '2026-09-07T00:00:00.000001Z',
      content: 'final',
    };
    mergeSessionStreamFrame(client, 'w', 's', '', { type: 'event_start', event: { id: final.id, type: final.type } });
    mergeSessionStreamFrame(client, 'w', 's', '', final);
    const end = {
      id: 'end',
      type: 'span.model_request_end',
      model_request_start_id: 'request',
      is_error: isError,
      processed_at: '2026-09-07T00:00:00.000002Z',
    };
    mergeSessionStreamFrame(client, 'w', 's', '', end);
    // Discarding a disconnected accumulator must not erase an explicit close.
    cleanupIncompleteSessionStreamEvents(client, 'w', 's');
    // Retried starts cannot revive a preview closed without a durable final.
    mergeSessionStreamFrame(client, 'w', 's', '', start);
    mergeSessionStreamFrame(client, 'w', 's', '', {
      type: 'event_delta',
      event_id: 'orphan',
      delta: { content: { type: 'text', text: 'late' } },
    });
    expect(sessionDetailScopeEvents(client, 'w', 's', [''])).toEqual([final, end]);
    expect(sessionDetailDeltaFrames(client, 'w', 's', [''])).toEqual({});
    expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 's', ['child']))).toEqual(['orphan']);
    mergeSessionStreamFrame(client, 'w', 's', '', {
      type: 'event_start',
      event: { id: 'next-request', type: 'agent.message' },
    });
    expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 's', ['']))).toEqual(['next-request']);
  },
);

test.each(['child', 'primary'])('thread termination closes only its explicit subject %s', (subject) => {
  const client = new QueryClient();
  for (const scope of ['', 'child', 'sibling'])
    mergeSessionStreamFrame(client, 'w', 's', scope, {
      type: 'event_start',
      event: { id: scope || 'main', type: 'agent.message' },
    });
  mergeSessionStreamFrame(
    client,
    'w',
    's',
    '',
    {
      id: 'terminated',
      type: 'session.thread_status_terminated',
      session_thread_id: subject,
      processed_at: '2026-09-07T00:00:00Z',
    },
    'primary',
  );
  const target = subject === 'primary' ? '' : subject;
  expect(sessionDetailDeltaFrames(client, 'w', 's', [target])).toEqual({});
  const other = subject === 'primary' ? 'child' : '';
  expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 's', [other]))).toHaveLength(1);
  expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 's', ['sibling']))).toHaveLength(1);
  mergeSessionStreamFrame(
    client,
    'w',
    's',
    target,
    { type: 'event_start', event: { id: 'late-new-id', type: 'agent.message' } },
    'primary',
  );
  expect(sessionDetailDeltaFrames(client, 'w', 's', [target])).toEqual({});
});

test.each(['session.status_terminated', 'session.deleted'])(
  '%s closes every Session scope without affecting another workspace or Session',
  (type) => {
    const client = new QueryClient();
    for (const scope of ['', 'child'])
      mergeSessionStreamFrame(client, 'w', 's', scope, {
        type: 'event_start',
        event: { id: scope || 'main', type: 'agent.message' },
      });
    for (const [workspace, session] of [
      ['other', 's'],
      ['w', 'other'],
    ])
      mergeSessionStreamFrame(client, workspace, session, '', {
        type: 'event_start',
        event: { id: 'unrelated', type: 'agent.message' },
      });
    mergeSessionStreamFrame(client, 'w', 's', '', { id: 'terminal', type, processed_at: '2026-09-07T00:00:00Z' });
    expect(sessionDetailDeltaFrames(client, 'w', 's', ['', 'child'])).toEqual({});
    mergeSessionStreamFrame(client, 'w', 's', 'new-child', {
      type: 'event_start',
      event: { id: 'late-new-id', type: 'agent.message' },
    });
    expect(sessionDetailDeltaFrames(client, 'w', 's', ['new-child'])).toEqual({});
    for (const [workspace, session] of [
      ['other', 's'],
      ['w', 'other'],
    ])
      expect(Object.keys(sessionDetailDeltaFrames(client, workspace, session, ['']))).toEqual(['unrelated']);
  },
);

test('old request ends from history do not clear an active live preview', async () => {
  const client = new QueryClient();
  mergeSessionStreamFrame(client, 'w', 'historical-end', '', {
    type: 'event_start',
    event: { id: 'live', type: 'agent.message' },
  });
  globalThis.fetch = (async () =>
    Response.json({
      data: [{ id: 'old-end', type: 'span.model_request_end', processed_at: '2020-01-01T00:00:00Z' }],
      next_page: null,
    })) as typeof fetch;
  await syncSessionEventHistory({ queryClient: client, workspaceId: 'w', sessionId: 'historical-end' });
  expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 'historical-end', ['']))).toEqual(['live']);
});

test('normal EOF discards old drafts while allowing a same-ID start on the next connection', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/w/sessions/eof');
  const client = new QueryClient();
  const controller = new AbortController();
  const final = { id: 'complete', type: 'agent.message', processed_at: '2026-09-07T00:00:00Z', content: 'final' };
  let streams = 0;
  let historyReads = 0;
  let rebuilt = false;
  globalThis.fetch = (async (input) => {
    const path = input instanceof Request ? input.url : String(input);
    if (!path.includes('/stream?'))
      return Response.json({ data: ++historyReads === 1 ? [] : [final], next_page: null });
    if (++streams === 1) {
      const frames = ['complete', 'orphan'].map((id) => ({
        type: 'event_start',
        event: { id, type: 'agent.message' },
      }));
      return new Response(frames.map((event) => `event: event_start\ndata: ${JSON.stringify(event)}\n\n`).join(''), {
        headers: { 'Content-Type': 'text/event-stream' },
      });
    }
    expect(sessionDetailScopeEvents(client, 'w', 'eof', [''])).toEqual([final]);
    expect(sessionDetailDeltaFrames(client, 'w', 'eof', [''])).toEqual({});
    const start = { type: 'event_start', event: { id: 'orphan', type: 'agent.message' } };
    return new Response(`event: event_start\ndata: ${JSON.stringify(start)}\n\n`, {
      headers: { 'Content-Type': 'text/event-stream' },
    });
  }) as typeof fetch;
  await runSessionEventStreamLoop({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'eof',
    threadId: '',
    signal: controller.signal,
    onCacheChange: () => {},
    onPrimaryEvent: (event) => {
      if (streams === 2 && event.type === 'event_start') {
        rebuilt = Object.keys(sessionDetailDeltaFrames(client, 'w', 'eof', [''])).includes('orphan');
        controller.abort();
      }
    },
  });
  expect(streams).toBe(2);
  expect(historyReads).toBe(3);
  expect(rebuilt).toBe(true);
  client.clear();
});

test('a child idle does not schedule reconciliation of the main connection preview', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/w/sessions/child-idle');
  const client = new QueryClient();
  const controller = new AbortController();
  let historyReads = 0;
  globalThis.fetch = (async (input) => {
    const path = input instanceof Request ? input.url : String(input);
    if (!path.includes('/stream?')) {
      historyReads += 1;
      return Response.json({ data: [], next_page: null });
    }
    return new Response(
      new ReadableStream({
        start(stream) {
          const frames = [
            { type: 'event_start', event: { id: 'main-preview', type: 'agent.message' } },
            {
              id: 'child-idle',
              type: 'session.thread_status_idle',
              session_thread_id: 'child',
              processed_at: '2026-09-07T00:00:00Z',
            },
          ];
          stream.enqueue(
            new TextEncoder().encode(
              frames.map((event) => `event: ${event.type}\ndata: ${JSON.stringify(event)}\n\n`).join(''),
            ),
          );
          controller.signal.addEventListener('abort', () => stream.error(controller.signal.reason), { once: true });
        },
      }),
      { headers: { 'Content-Type': 'text/event-stream' } },
    );
  }) as typeof fetch;
  const running = runSessionEventStreamLoop({
    queryClient: client,
    workspaceId: 'w',
    sessionId: 'child-idle',
    threadId: '',
    primaryThreadId: 'primary',
    signal: controller.signal,
    onCacheChange: () => {},
  });
  try {
    await waitFor(() =>
      expect(sessionDetailScopeEvents(client, 'w', 'child-idle', ['']).some((event) => event.id === 'child-idle')).toBe(
        true,
      ),
    );
    await new Promise((resolve) => setTimeout(resolve, 1650));
    expect(Object.keys(sessionDetailDeltaFrames(client, 'w', 'child-idle', ['']))).toEqual(['main-preview']);
    expect(historyReads).toBe(1);
  } finally {
    controller.abort();
    await running;
    client.clear();
  }
});

test('arrival order, equal milliseconds and stale pending cannot change the completed history', () => {
  const a = { id: 'z', processed_at: '2026-09-07T00:00:00.000001Z', content: 'first' };
  const b = { id: 'a', processed_at: '2026-09-07T00:00:00.000002Z', content: 'second' };
  const pending = { id: 'pending', created_at: '2026-09-06T00:00:00Z', processed_at: null };
  const live = mergeSessionEventCache(undefined, [b, pending, a]);
  const refreshed = mergeSessionEventCache(undefined, [a, b, pending]);
  expect(live.events).toEqual(refreshed.events);
  expect(mergeSessionEventCache(live, [{ ...a, processed_at: null, content: 'stale' }]).events).toEqual(
    refreshed.events,
  );
});

test('a timestamp-free preview follows the history and is replaced by its final', () => {
  const client = new QueryClient();
  mergeSessionStreamFrame(client, 'w', 's', '', {
    id: 'old',
    type: 'user.message',
    processed_at: '2020-01-01T00:00:00.000001Z',
  });
  mergeSessionStreamFrame(client, 'w', 's', '', { type: 'event_start', event: { id: 'new', type: 'agent.message' } });
  expect(sessionDetailScopeEvents(client, 'w', 's', ['']).map((event) => event.id)).toEqual(['old', 'new']);
  const final = { id: 'new', type: 'agent.message', processed_at: '2020-01-01T00:00:00.000002Z', content: 'done' };
  mergeSessionStreamFrame(client, 'w', 's', '', final);
  expect(sessionDetailScopeEvents(client, 'w', 's', [''])[1]).toEqual(final);
  expect(sessionDetailDeltaFrames(client, 'w', 's', [''])).toEqual({});
});

test('opens the stream before all history pages, and starts a fresh snapshot on the next sync', async () => {
  Object.defineProperty(globalThis, 'window', { configurable: true, value: globalThis });
  const client = new QueryClient();
  const paths: string[] = [];
  const a = { id: 'A', type: 'agent.message', processed_at: '2026-09-07T00:00:00.000001Z' };
  const b = { id: 'B', type: 'agent.message', processed_at: '2026-09-07T00:00:00.000002Z' };
  const c = { id: 'C', type: 'agent.message', processed_at: '2026-09-07T00:00:00.000003Z' };
  globalThis.fetch = (async (input) => {
    const path = input instanceof Request ? input.url : String(input);
    paths.push(path);
    if (path.includes('/stream?')) {
      return new Response(`event: agent.message\ndata: ${JSON.stringify(c)}\n\n`, {
        headers: { 'Content-Type': 'text/event-stream' },
      });
    }
    const page = new URL(path, 'http://local').searchParams.get('page');
    return Response.json(page ? { data: [b], next_page: null } : { data: [a], next_page: 'snapshot-page-2' });
  }) as typeof fetch;
  const signal = new AbortController().signal;
  await streamSessionEvents({
    sessionId: 's',
    workspaceId: 'w',
    signal,
    onOpen: async () => {
      await syncSessionEventHistory({ queryClient: client, sessionId: 's', workspaceId: 'w', signal });
      expect(sessionDetailScopeEvents(client, 'w', 's', ['']).map((event) => event.id)).toEqual(['A', 'B']);
    },
    onEvent: (event) => mergeSessionStreamFrame(client, 'w', 's', '', event),
  });
  expect(paths[0]).toContain('/stream?');
  expect(new URL(paths[1], 'http://local').searchParams.has('page')).toBe(false);
  expect(new URL(paths[2], 'http://local').searchParams.get('page')).toBe('snapshot-page-2');
  expect(sessionDetailScopeEvents(client, 'w', 's', ['']).map((event) => event.id)).toEqual(['A', 'B', 'C']);
  await syncSessionEventHistory({ queryClient: client, sessionId: 's', workspaceId: 'w', signal });
  expect(new URL(paths[3], 'http://local').searchParams.has('page')).toBe(false);
});

test('a late request end cannot clear previews from the next request', () => {
  const client = new QueryClient();
  const merge = (event: Parameters<typeof mergeSessionStreamFrame>[4]) =>
    mergeSessionStreamFrame(client, 'workspace', 'session', '', event);
  merge({ id: 'request-a', type: 'span.model_request_start' });
  merge({ type: 'event_start', event: { id: 'preview-a', type: 'agent.message' } });
  merge({ id: 'request-b', type: 'span.model_request_start' });
  merge({ type: 'event_start', event: { id: 'preview-b', type: 'agent.message' } });
  merge({ id: 'end-a', type: 'span.model_request_end', model_request_start_id: 'request-a' });
  expect(sessionDetailScopeEvents(client, 'workspace', 'session', ['']).some((event) => event.id === 'preview-b')).toBe(
    true,
  );
  expect(sessionDetailScopeEvents(client, 'workspace', 'session', ['']).some((event) => event.id === 'preview-a')).toBe(
    false,
  );
  merge({ id: 'end-b', type: 'span.model_request_end', model_request_start_id: 'request-b' });
  expect(sessionDetailScopeEvents(client, 'workspace', 'session', ['']).some((event) => event.id === 'preview-b')).toBe(
    false,
  );
});
