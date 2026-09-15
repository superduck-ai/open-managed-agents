import { act, renderHook } from '@testing-library/react';
import { describe, expect, mock, test } from 'bun:test';
import '../../test/setup';
import { useTimeBrush } from './useTimeBrush';

const START = Date.parse('2026-08-06T04:50:00.000Z');
const END = Date.parse('2026-08-13T04:50:00.000Z');

function plotEvent(clientX: number) {
  const grid = document.createElement('div');
  grid.className = 'recharts-cartesian-grid';
  grid.getBoundingClientRect = () =>
    ({
      x: 100,
      y: 0,
      left: 100,
      top: 0,
      right: 800,
      bottom: 120,
      width: 700,
      height: 120,
      toJSON() {
        return {};
      },
    }) as DOMRect;
  const currentTarget = document.createElement('div');
  currentTarget.appendChild(grid);
  currentTarget.setPointerCapture = () => undefined;
  currentTarget.releasePointerCapture = () => undefined;
  return { clientX, currentTarget, pointerId: 1 };
}

describe('useTimeBrush', () => {
  test('does not commit when pointerup happens before any drag', () => {
    const onChange = mock();
    const { result } = renderHook(() => useTimeBrush({ startMs: START, endMs: END }, onChange));
    act(() => {
      result.current.onPointerDown(plotEvent(100));
      result.current.onPointerUp();
    });
    expect(onChange).not.toHaveBeenCalled();
  });

  test('maps a drag across empty plot space onto the dashboard clock', () => {
    const onChange = mock();
    const { result } = renderHook(() => useTimeBrush({ startMs: START, endMs: END }, onChange));
    act(() => {
      result.current.onPointerDown(plotEvent(100));
      result.current.onPointerMove(plotEvent(450));
      result.current.onPointerUp();
    });
    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith('2026-08-06T04:50:00.000Z', '2026-08-09T16:50:00.000Z');
  });
});
