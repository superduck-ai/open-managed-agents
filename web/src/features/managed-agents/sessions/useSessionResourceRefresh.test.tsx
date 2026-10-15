import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { Dispatch, SetStateAction } from 'react';
import { resetTestDom } from '../../../test/setup';
import type { SessionApiResponse } from '../types';
import { useSessionResourceRefresh } from './useSessionResourceRefresh';

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function makeSession(id = 'sesn_refresh', status = 'running') {
  return { id, status, resources: [], type: 'session' } as unknown as SessionApiResponse;
}

function renderRefresh(initial: SessionApiResponse, resourcesVisible = true) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
  let latest: SessionApiResponse | null = initial;
  const setSession: Dispatch<SetStateAction<SessionApiResponse | null>> = (update) => {
    latest = typeof update === 'function' ? update(latest) : update;
  };
  const hook = renderHook(
    ({ session, workspaceId }) => useSessionResourceRefresh(session, workspaceId, resourcesVisible, setSession),
    {
      initialProps: { session: initial, workspaceId: 'workspace_first' },
      wrapper: ({ children }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>,
    },
  );
  return { ...hook, latest: () => latest };
}

test('keeps current resources after refresh failure and clears the error on retry', async () => {
  resetTestDom('https://oma.duck.ai/');
  globalThis.fetch = async () =>
    Response.json({ error: { type: 'api_error', message: 'Refresh failed' } }, { status: 500 });
  const initial = makeSession();
  initial.resources = [{ type: 'file', file_id: 'file_existing' }];
  const hook = renderRefresh(initial);
  await waitFor(() => expect(hook.result.current.error).toBe('Refresh failed'));
  expect(hook.latest()?.resources).toEqual(initial.resources);
  globalThis.fetch = async () => Response.json({ ...initial, resources: [{ type: 'file', file_id: 'file_output' }] });
  await act(async () => {
    await hook.result.current.refresh();
  });
  await waitFor(() => expect(hook.latest()?.resources[0]?.file_id).toBe('file_output'));
  expect(hook.result.current.error).toBeNull();
});

test('ignores an old workspace response after switching scopes', async () => {
  resetTestDom('https://oma.duck.ai/');
  let resolveFirst: ((response: Response) => void) | undefined;
  globalThis.fetch = async (_input, init) =>
    new Headers(init?.headers).get('x-workspace-id') === 'workspace_first'
      ? new Promise<Response>((resolve) => {
          resolveFirst = resolve;
        })
      : Response.json({ ...makeSession(), resources: [{ type: 'file', file_id: 'file_second' }] });
  const hook = renderRefresh(makeSession());
  await waitFor(() => expect(resolveFirst).toBeDefined());
  hook.rerender({ session: makeSession(), workspaceId: 'workspace_second' });
  await waitFor(() => expect(hook.latest()?.resources[0]?.file_id).toBe('file_second'));
  await act(async () => {
    resolveFirst?.(Response.json({ ...makeSession(), resources: [{ type: 'file', file_id: 'file_first' }] }));
  });
  expect(hook.latest()?.resources[0]?.file_id).toBe('file_second');
});

test('discovers generated files while running and refreshes immediately when the session becomes idle', async () => {
  resetTestDom('https://oma.duck.ai/');
  let fileId = '';
  const fetchMock = mock(async () =>
    Response.json({ ...makeSession(), resources: fileId ? [{ type: 'file', file_id: fileId }] : [] }),
  );
  globalThis.fetch = fetchMock as typeof fetch;
  const hook = renderRefresh(makeSession());
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  fileId = 'file_during_run';
  await waitFor(() => expect(hook.latest()?.resources[0]?.file_id).toBe(fileId), { timeout: 4500 });
  fileId = 'file_final';
  hook.rerender({ session: makeSession('sesn_refresh', 'idle'), workspaceId: 'workspace_first' });
  await waitFor(() => expect(hook.latest()?.resources[0]?.file_id).toBe('file_final'));
});

test('preserves Session identity when only non-resource response fields change', async () => {
  resetTestDom('https://oma.duck.ai/');
  const initial = makeSession();
  initial.resources = [{ id: 'resource_existing', type: 'file', file_id: 'file_existing', metadata: { version: 1 } }];
  let response = initial;
  const fetchMock = mock(async () => Response.json(response));
  globalThis.fetch = fetchMock as typeof fetch;
  const hook = renderRefresh(initial);
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  await act(async () => {
    await hook.result.current.refresh();
  });
  expect(hook.latest()).toBe(initial);
  response = { ...initial, updated_at: 'changed', usage: { input_tokens: 42 }, status: 'idle' };
  await act(async () => {
    await hook.result.current.refresh();
  });
  expect(hook.latest()).toBe(initial);
  expect(hook.latest()?.resources).toBe(initial.resources);
  response = { ...response, resources: [{ ...initial.resources[0], metadata: { version: 2 } }] };
  await act(async () => {
    await hook.result.current.refresh();
  });
  await waitFor(() => expect(hook.latest()?.resources[0].metadata).toEqual({ version: 2 }));
  expect(hook.latest()).not.toBe(initial);
  expect(hook.latest()?.status).toBe('running');
  expect(hook.latest()?.updated_at).toBe(initial.updated_at);
});

test('discovers late resources after idle with the resources inspector closed', async () => {
  resetTestDom('https://oma.duck.ai/');
  let resources: SessionApiResponse['resources'] = [];
  const fetchMock = mock(async () => Response.json({ ...makeSession(), resources }));
  globalThis.fetch = fetchMock as typeof fetch;
  const hook = renderRefresh(makeSession(), false);
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  hook.rerender({ session: makeSession('sesn_refresh', 'idle'), workspaceId: 'workspace_first' });
  await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  resources = [{ type: 'file', file_id: 'file_late', mount_path: '/outputs/late.txt' }];
  await waitFor(() => expect(hook.latest()?.resources[0]?.file_id).toBe('file_late'), { timeout: 2500 });
});
