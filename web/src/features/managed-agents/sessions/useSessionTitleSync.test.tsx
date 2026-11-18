import { afterEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { resetTestDom } from '../../../test/setup';
import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import { useSessionTitleSync } from './useSessionTitleSync';

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function session(id = 'sesn_title', title: string | null = null): SessionApiResponse {
  return {
    id,
    title,
    type: 'session',
    agent: 'agent_title',
    environment_id: 'env_title',
    resources: [],
    archived_at: null,
    created_at: '2026-11-18T00:00:00Z',
    updated_at: '2026-11-18T00:00:00Z',
    status: 'running',
  };
}

function renderSync(initial: SessionApiResponse, events: QuickstartSessionEvent[] = []) {
  return renderHook(
    ({ current, events, workspaceId }) => {
      const [value, setValue] = useState<SessionApiResponse | null>(current);
      useSessionTitleSync(value, events, workspaceId, setValue);
      return { session: value, setSession: setValue };
    },
    { initialProps: { current: initial, events, workspaceId: 'workspace_title' } },
  );
}

test('keeps the visible title when metadata refresh fails and ignores updates without a title field', async () => {
  resetTestDom('https://oma.duck.ai/');
  let requests = 0;
  globalThis.fetch = async () => {
    requests += 1;
    return Response.json({ error: { message: 'Unavailable' } }, { status: 500 });
  };
  const initial = session('sesn_title', 'Existing');
  const hook = renderSync(initial, [{ type: 'session.updated', thread_id: 'thread_child' }]);
  await act(async () => undefined);
  expect(requests).toBe(0);
  hook.rerender({
    current: initial,
    events: [{ id: 'sevt_title', type: 'session.updated', title: 'Update' }],
    workspaceId: 'workspace_title',
  });
  await waitFor(() => expect(requests).toBe(1));
  expect(hook.result.current.session?.title).toBe('Existing');
});

test('ignores an old session response after navigation', async () => {
  resetTestDom('https://oma.duck.ai/');
  let finish: ((response: Response) => void) | undefined;
  globalThis.fetch = async () =>
    new Promise<Response>((resolve) => {
      finish = resolve;
    });
  const initial = session();
  const hook = renderSync(initial, [{ id: 'sevt_title', type: 'session.updated', title: 'Update' }]);
  await waitFor(() => expect(finish).toBeDefined());
  act(() => hook.result.current.setSession(session('sesn_second', 'Second')));
  hook.rerender({ current: session('sesn_second'), events: [], workspaceId: 'workspace_second' });
  await act(async () => finish?.(Response.json(session('sesn_title', 'Old'))));
  expect(hook.result.current.session?.title).toBe('Second');
});

test('reads the canonical title for historical and newly received updates without changing execution state', async () => {
  resetTestDom('https://oma.duck.ai/');
  let title: string | null = 'Canonical generated title';
  globalThis.fetch = async () => Response.json({ ...session('sesn_title', title), status: 'idle' });
  const initial = session();
  const history = [{ id: 'sevt_history', type: 'session.updated', title: 'Stale historical title' }];
  const hook = renderSync(initial, history);
  await waitFor(() => expect(hook.result.current.session?.title).toBe(title));
  expect(hook.result.current.session?.status).toBe('running');
  title = null;
  hook.rerender({
    current: initial,
    events: [...history, { id: 'sevt_live', type: 'session.updated', session_thread_id: 'sthr_primary', title: null }],
    workspaceId: 'workspace_title',
  });
  await waitFor(() => expect(hook.result.current.session?.title).toBeNull());
});

test('a late metadata response cannot overwrite the title from a newer update', async () => {
  resetTestDom('https://oma.duck.ai/');
  let finishOld: ((response: Response) => void) | undefined;
  let requests = 0;
  globalThis.fetch = async () => {
    requests += 1;
    if (requests === 1)
      return new Promise<Response>((resolve) => {
        finishOld = resolve;
      });
    return Response.json(session('sesn_title', 'Newer canonical title'));
  };
  const initial = session();
  const history = [{ id: 'sevt_old', type: 'session.updated', title: 'Old' }];
  const hook = renderSync(initial, history);
  await waitFor(() => expect(finishOld).toBeDefined());
  hook.rerender({
    current: initial,
    events: [...history, { id: 'sevt_new', type: 'session.updated', title: 'New' }],
    workspaceId: 'workspace_title',
  });
  await waitFor(() => expect(hook.result.current.session?.title).toBe('Newer canonical title'));
  await act(async () => finishOld?.(Response.json(session('sesn_title', 'Old title'))));
  expect(hook.result.current.session?.title).toBe('Newer canonical title');
});
