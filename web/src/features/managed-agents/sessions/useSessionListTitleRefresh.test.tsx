import { afterEach, expect, test } from 'bun:test';
import { useCallback, useState } from 'react';
import { resetTestDom } from '../../../test/setup';
import type { ManagedEntityApiResponse, ManagedEntityListFilters, PageResponse, SessionApiResponse } from '../types';
import { sessionTitleRefreshInterval } from './sessionTitleRefresh';
import { useSessionListTitleRefresh } from './useSessionListTitleRefresh';

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
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    status: 'running',
  };
}

function renderRefresh(entities: ManagedEntityApiResponse[]) {
  const filters: ManagedEntityListFilters = { agentId: 'agent_title', statuses: ['running'] };
  return renderHook(
    ({ workspaceId }) => {
      const [rows, setRows] = useState(entities);
      const onPage = useCallback((page: PageResponse<ManagedEntityApiResponse>) => setRows(page.data), []);
      useSessionListTitleRefresh({
        section: 'sessions',
        loading: false,
        searching: false,
        entities: rows,
        workspaceId,
        cursor: 'page_two',
        filters,
        onPage,
      });
      return rows;
    },
    { initialProps: { workspaceId: 'workspace_title' } },
  );
}

async function showPage() {
  await act(async () => {
    document.dispatchEvent(new window.Event('visibilitychange'));
  });
}

test('keeps rows after background failure and discards an old workspace response', async () => {
  resetTestDom('https://oma.duck.ai/');
  let finish: ((response: Response) => void) | undefined;
  globalThis.fetch = async () => Response.json({ error: { message: 'Unavailable' } }, { status: 500 });
  const initial = session();
  const hook = renderRefresh([initial]);
  await showPage();
  expect(hook.result.current).toEqual([initial]);
  globalThis.fetch = async () =>
    new Promise<Response>((resolve) => {
      finish = resolve;
    });
  await showPage();
  await waitFor(() => expect(finish).toBeDefined());
  hook.rerender({ workspaceId: 'workspace_second' });
  await act(async () => finish?.(Response.json({ data: [session('sesn_title', 'Old workspace')] })));
  expect(hook.result.current).toEqual([initial]);
});

test('updates the title and removes rows that no longer match the current filters', async () => {
  resetTestDom('https://oma.duck.ai/');
  let requestUrl = '';
  globalThis.fetch = async (input) => {
    requestUrl = String(input);
    return Response.json({
      data: [session('sesn_title', 'Generated')],
      next_page: 'changed',
    });
  };
  const named = session('sesn_named', 'User title');
  const initial = session();
  const hook = renderRefresh([named, initial]);
  await showPage();
  await waitFor(() => expect(hook.result.current[0]?.title).toBe('Generated'));
  expect(hook.result.current.map((row) => row.id)).toEqual([initial.id]);
  expect(requestUrl).toContain('page=page_two');
  expect(requestUrl).toContain('agent_id=agent_title');
  expect(new URL(requestUrl, 'https://oma.duck.ai').searchParams.getAll('statuses[]')).toEqual(['running']);
});

test('refreshes older unnamed sessions less often and skips named and archived sessions', () => {
  const initial = session();
  const now = Date.parse(initial.updated_at);
  expect(sessionTitleRefreshInterval(session('sesn_named', 'Named'), now)).toBe(false);
  expect(sessionTitleRefreshInterval({ ...initial, archived_at: initial.created_at }, now)).toBe(false);
  expect(sessionTitleRefreshInterval(initial, now + 10 * 60 * 1000)).toBe(30_000);
  expect(sessionTitleRefreshInterval(initial, now)).toBe(5_000);
});

test('pauses background requests while the page is hidden', async () => {
  resetTestDom('https://oma.duck.ai/');
  let requests = 0;
  globalThis.fetch = async () => {
    requests += 1;
    return Response.json({ data: [session('sesn_title', 'Generated')] });
  };
  const descriptor = Object.getOwnPropertyDescriptor(document, 'visibilityState');
  try {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' });
    const hook = renderRefresh([session()]);
    await showPage();
    expect(requests).toBe(0);
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
    await showPage();
    await waitFor(() => expect(hook.result.current[0]?.title).toBe('Generated'));
    expect(requests).toBe(1);
  } finally {
    if (descriptor) Object.defineProperty(document, 'visibilityState', descriptor);
    else Reflect.deleteProperty(document, 'visibilityState');
  }
});
