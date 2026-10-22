import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import type { ReactNode } from 'react';
import type { ObservabilityScope, Trajectory } from './api';

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WorkspaceContext } = await import('../../shared/workspaces/context');
const { defaultWorkspace } = await import('../../shared/workspaces/api');
const { useTrajectoryBrowser } = await import('./useTrajectoryBrowser');
const originalFetch = globalThis.fetch;
const clients: InstanceType<typeof QueryClient>[] = [];

afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  globalThis.fetch = originalFetch;
});

function browser(scope: ObservabilityScope) {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  clients.push(client);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <WorkspaceContext
        value={{
          orgUuid: 'org',
          activeWorkspaceId: 'ws',
          activeWorkspace: defaultWorkspace,
          workspaces: [defaultWorkspace],
          isLoading: false,
          error: null,
          selectWorkspace: () => {},
          createWorkspace: async () => defaultWorkspace,
          refreshWorkspaces: async () => {},
        }}
      >
        {children}
      </WorkspaceContext>
    </QueryClientProvider>
  );
  return {
    client,
    ...renderHook((value: ObservabilityScope) => useTrajectoryBrowser(value), { initialProps: scope, wrapper }),
  };
}

function rows(...ids: string[]): Trajectory[] {
  return ids.map((id) => ({ id, session_id: 'session', agent_id: '', started_at: 1, last_event_at: 2 }));
}

test('session selection survives polling reorder and explicit back stays on the list', async () => {
  globalThis.fetch = (async () => Response.json({ items: rows('root', 'child'), next_offset: null })) as typeof fetch;
  const { result, client } = browser({ kind: 'session', sessionId: 'session' });
  await waitFor(() => expect(result.current.current).toBe('root'));
  const key = ['trajectories', result.current.queryScope, 0];
  act(() => {
    client.setQueryData(key, { items: rows('child', 'root'), next_offset: null });
  });
  await waitFor(() => expect(result.current.query.data?.items[0]?.id).toBe('child'));
  expect(result.current.current).toBe('root');
  act(() => result.current.select(null));
  act(() => {
    client.setQueryData(key, { items: rows('another', 'child', 'root'), next_offset: null });
  });
  await waitFor(() => expect(result.current.query.data?.items[0]?.id).toBe('another'));
  expect(result.current.current).toBeUndefined();
  act(() => result.current.select('child'));
  act(() => {
    client.setQueryData(key, { items: rows('root'), next_offset: null });
  });
  await waitFor(() => expect(result.current.query.data?.items[0]?.id).toBe('root'));
  expect(result.current.current).toBe('child');
});

test('initially empty session commits its first arrival and resets when scope changes', async () => {
  globalThis.fetch = (async () => Response.json({ items: [], next_offset: null })) as typeof fetch;
  const { result, client, rerender } = browser({ kind: 'session', sessionId: 'empty' });
  await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
  expect(result.current.current).toBeUndefined();
  act(() => {
    client.setQueryData(['trajectories', result.current.queryScope, 0], { items: rows('arrived'), next_offset: null });
  });
  await waitFor(() => expect(result.current.current).toBe('arrived'));
  rerender({ kind: 'session', sessionId: 'other' });
  await waitFor(() => expect(result.current.queryScope.sessionId).toBe('other'));
  expect(result.current.current).toBeUndefined();
  act(() => {
    client.setQueryData(['trajectories', result.current.queryScope, 0], {
      items: rows('other-root'),
      next_offset: null,
    });
  });
  await waitFor(() => expect(result.current.current).toBe('other-root'));
});

test('pagination follows server offsets and keeps previous offsets for back navigation', async () => {
  const offsets: number[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const offset = Number(new URL(String(input), 'http://localhost').searchParams.get('offset'));
    offsets.push(offset);
    return Response.json({ items: rows(`row-${offset}`), next_offset: offset === 0 ? 7 : offset === 7 ? 19 : null });
  }) as typeof fetch;
  const { result, rerender } = browser({ kind: 'workspace' });
  await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
  act(() => result.current.next());
  await waitFor(() => expect(result.current.query.data?.items[0]?.id).toBe('row-7'));
  act(() => result.current.next());
  await waitFor(() => expect(result.current.query.data?.items[0]?.id).toBe('row-19'));
  expect(result.current.hasPrevious).toBe(true);
  act(() => result.current.previous());
  await waitFor(() => expect(result.current.query.data?.items[0]?.id).toBe('row-7'));
  act(() => result.current.previous());
  await waitFor(() => expect(result.current.hasPrevious).toBe(false));
  expect(offsets).toEqual([0, 7, 19]);
  act(() => result.current.next());
  await waitFor(() => expect(result.current.hasPrevious).toBe(true));
  rerender({ kind: 'agent', agentId: 'agent' });
  await waitFor(() => expect(result.current.queryScope.agentId).toBe('agent'));
  expect(result.current.hasPrevious).toBe(false);
  await waitFor(() => expect(result.current.query.isSuccess).toBe(true));
});

test('opening a trajectory pauses list queries and returning reactivates them', async () => {
  let calls = 0;
  globalThis.fetch = (async () => {
    calls++;
    return Response.json({ items: rows('root'), next_offset: null });
  }) as typeof fetch;
  const { result, client } = browser({ kind: 'session', sessionId: 'session' });
  await waitFor(() => expect(result.current.current).toBe('root'));
  const key = ['trajectories', result.current.queryScope, 0];
  expect(client.getQueryCache().find({ queryKey: key })?.isActive()).toBe(false);
  await act(async () => {
    await client.invalidateQueries({ queryKey: key });
  });
  expect(calls).toBe(1);
  act(() => result.current.select(null));
  await waitFor(() => expect(calls).toBe(2));
  expect(client.getQueryCache().find({ queryKey: key })?.isActive()).toBe(true);
});
