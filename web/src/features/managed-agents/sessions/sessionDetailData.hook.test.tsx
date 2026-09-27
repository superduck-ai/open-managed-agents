import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../../test/setup';
import { useSessionDetailEventData } from './sessionDetailData';

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
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
