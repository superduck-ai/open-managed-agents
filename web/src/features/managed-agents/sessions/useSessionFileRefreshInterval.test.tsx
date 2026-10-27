import { afterEach, expect, spyOn, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { SessionApiResponse } from '../types';
import { useSessionFileRefreshInterval } from './useSessionFileRefreshInterval';

const { act, cleanup, renderHook } = await import('@testing-library/react');
let timerSpy: ReturnType<typeof spyOn> | undefined;
let clearSpy: ReturnType<typeof spyOn> | undefined;

afterEach(() => {
  cleanup();
  timerSpy?.mockRestore();
  clearSpy?.mockRestore();
});

function session(status: string, extra: Partial<SessionApiResponse> = {}) {
  return { id: 'session', status, resources: [], ...extra } as SessionApiResponse;
}

test('stops the post-run refresh after 30 seconds and does not extend it on resource updates', () => {
  resetTestDom();
  let expire: (() => void) | undefined;
  const original = window.setTimeout;
  timerSpy = spyOn(window, 'setTimeout').mockImplementation((handler, timeout, ...args) => {
    if (timeout === 30_000) {
      expire = handler as () => void;
      return 987654;
    }
    return original(handler, timeout, ...args);
  });
  const hook = renderHook(({ current }) => useSessionFileRefreshInterval(current, 'workspace'), {
    initialProps: { current: session('running') },
  });
  expect(hook.result.current).toBe(3000);
  hook.rerender({ current: session('idle') });
  expect(hook.result.current).toBe(1000);
  hook.rerender({ current: session('idle', { resources: [{ type: 'file', file_id: 'late' }] }) });
  expect(timerSpy.mock.calls.filter((call) => call[1] === 30_000)).toHaveLength(1);
  act(() => expire?.());
  expect(hook.result.current).toBe(false);
});

test('does not poll old idle sessions but catches recently completed sessions on mount', () => {
  resetTestDom();
  const old = renderHook(() =>
    useSessionFileRefreshInterval(session('idle', { updated_at: '2025-01-01T00:00:00Z' }), 'workspace'),
  );
  expect(old.result.current).toBe(false);
  const completed = session('idle', { updated_at: new Date().toISOString() });
  const recent = renderHook(() => useSessionFileRefreshInterval(completed, 'workspace'));
  expect(recent.result.current).toBe(1000);
});

test('cleans up the refresh window on scope changes, archival and unmount', () => {
  resetTestDom();
  clearSpy = spyOn(window, 'clearTimeout');
  const hook = renderHook(({ current, workspaceId }) => useSessionFileRefreshInterval(current, workspaceId), {
    initialProps: { current: session('running'), workspaceId: 'first' },
  });
  hook.rerender({ current: session('idle'), workspaceId: 'first' });
  expect(hook.result.current).toBe(1000);
  hook.rerender({ current: session('idle'), workspaceId: 'second' });
  expect(hook.result.current).toBe(false);
  expect(clearSpy).toHaveBeenCalled();
  hook.rerender({ current: session('running'), workspaceId: 'second' });
  hook.rerender({ current: session('idle'), workspaceId: 'second' });
  expect(hook.result.current).toBe(1000);
  hook.rerender({ current: session('idle', { archived_at: new Date().toISOString() }), workspaceId: 'second' });
  expect(hook.result.current).toBe(false);
  hook.unmount();
});
