import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import type { TrajectoryPage, TrajectorySegment } from './api';

const { act, cleanup, renderHook } = await import('@testing-library/react');
const { useSessionTrajectoryLane } = await import('./useSessionTrajectoryLane');

afterEach(() => cleanup());

function page(...ids: string[]): TrajectoryPage {
  const segments: TrajectorySegment[] = ids.map((id, index) => ({ id, started_at: `2026-10-2${index}T00:00:00Z` }));
  return { header: null, events: [], next_seq: -1, has_more: false, segment: ids.at(-1) ?? '', segments };
}

function lane(threadId?: string) {
  return renderHook((props: { threadId?: string }) => useSessionTrajectoryLane('sesn_1', props.threadId), {
    initialProps: { threadId },
  });
}

test('switching lanes drops the previous lane segment and selection', () => {
  const { result, rerender } = lane();
  act(() => result.current.onPage(page('cse_old', 'cse_new')));
  act(() => result.current.select('cse_old'));
  expect(result.current.target).toEqual({ sessionId: 'sesn_1', threadId: undefined, segment: 'cse_old' });
  rerender({ threadId: 'sthr_child' });
  expect(result.current.target).toEqual({ sessionId: 'sesn_1', threadId: 'sthr_child', segment: undefined });
  expect(result.current.segments).toEqual([]);
  expect(result.current.viewKey).toBe('sesn_1:sthr_child:');
  rerender({ threadId: undefined });
  expect(result.current.target.segment).toBeUndefined();
  expect(result.current.viewKey).toBe('sesn_1::');
});

test('the main lane defaults to the latest run and remounts only on explicit selection', () => {
  const { result } = lane();
  expect(result.current.selectedIndex).toBe(-1);
  act(() => result.current.onPage(page('cse_old', 'cse_new')));
  const target = result.current.target;
  expect(result.current.segments.map((segment) => segment.id)).toEqual(['cse_old', 'cse_new']);
  expect(result.current.selectedIndex).toBe(1);
  expect(result.current.viewKey).toBe('sesn_1::');
  act(() => result.current.onPage(page('cse_old', 'cse_new')));
  expect(result.current.target).toBe(target);
  act(() => result.current.select('cse_old'));
  expect(result.current.selectedIndex).toBe(0);
  expect(result.current.viewKey).toBe('sesn_1::cse_old');
  expect(result.current.target.segment).toBe('cse_old');
});

test('a child lane keeps its thread target', () => {
  const { result } = lane('sthr_child');
  act(() => result.current.onPage(page('sthr_child')));
  expect(result.current.target).toEqual({ sessionId: 'sesn_1', threadId: 'sthr_child', segment: undefined });
  expect(result.current.segments).toHaveLength(1);
});
