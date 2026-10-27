import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../../test/setup';
import { mergeSessionStreamFrame, sessionDetailScopeEvents } from '../api';
import { useSessionDetailEventData } from './sessionDetailData';

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('keeps a visible session history cache alive while the detail page is mounted', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_123');
  globalThis.fetch = async () => new Promise<Response>(() => undefined);
  const queryClient = new QueryClient({ defaultOptions: { queries: { gcTime: 10 } } });
  mergeSessionStreamFrame(queryClient, 'workspace_123', 'sesn_123', '', {
    id: 'sevt_old',
    type: 'user.message',
    processed_at: '2026-08-26T13:13:00Z',
  });

  const { result, unmount } = renderHook(
    () =>
      useSessionDetailEventData({
        sessionId: 'sesn_123',
        workspaceId: 'workspace_123',
        threads: [],
        includeArchivedThreads: false,
        live: true,
        refreshKey: 0,
      }),
    { wrapper: ({ children }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider> },
  );

  expect(result.current.events.map((event) => event.id)).toEqual(['sevt_old']);
  await new Promise((resolve) => setTimeout(resolve, 30));
  expect(sessionDetailScopeEvents(queryClient, 'workspace_123', 'sesn_123', ['']).map((event) => event.id)).toEqual([
    'sevt_old',
  ]);
  act(() =>
    result.current.appendPrimaryEvents([
      { id: 'sevt_new', type: 'user.message', processed_at: '2026-08-26T13:13:01Z' },
    ]),
  );
  expect(result.current.events.map((event) => event.id)).toEqual(['sevt_old', 'sevt_new']);
  unmount();
  await new Promise((resolve) => setTimeout(resolve, 30));
  expect(sessionDetailScopeEvents(queryClient, 'workspace_123', 'sesn_123', [''])).toEqual([]);
});

test('a permanently unavailable child stream stops showing as loading', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_123');
  let finishChild: ((response: Response) => void) | undefined;
  globalThis.fetch = async (input) =>
    String(input).includes('/threads/')
      ? new Promise<Response>((resolve) => {
          finishChild = resolve;
        })
      : new Response('missing', { status: 404 });
  const queryClient = new QueryClient();
  const threads = [
    { id: 'sthr_child', type: 'session_thread', parent_thread_id: 'sthr_primary', created_at: '2026-08-26T13:13:00Z' },
  ];
  const { result } = renderHook(
    () =>
      useSessionDetailEventData({
        sessionId: 'sesn_123',
        workspaceId: 'workspace_123',
        threads,
        includeArchivedThreads: false,
        live: true,
        refreshKey: 0,
      }),
    { wrapper: ({ children }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider> },
  );

  await waitFor(() => expect(finishChild).toBeDefined());
  expect(result.current.childLoading).toBe(true);
  await act(async () => finishChild?.(new Response('missing', { status: 404 })));
  await waitFor(() => expect(result.current.childLoading).toBe(false));
});
