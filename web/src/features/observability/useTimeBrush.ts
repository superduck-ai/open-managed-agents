import { useCallback, useEffect, useRef, useState } from 'react';
import { clampedCustomRange, timeAtPlotX } from './model';

type PlotPointerEvent = {
  clientX: number;
  currentTarget: EventTarget & Element;
  pointerId?: number;
};

function isoAtClientX(clientX: number, target: Element, startMs: number, endMs: number) {
  const grid = target.querySelector('.recharts-cartesian-grid') ?? target;
  const rect = grid.getBoundingClientRect();
  return new Date(timeAtPlotX(clientX - rect.left, rect.width, startMs, endMs)).toISOString();
}

export function useTimeBrush(
  domain: { startMs: number; endMs: number } | null,
  onTimeRangeChange?: (start: string, end: string) => void,
) {
  const dragRef = useRef<{ start: string | null; current: string | null }>({ start: null, current: null });
  const [selection, setSelection] = useState<{ left: number; right: number } | null>(null);

  const commit = useCallback(() => {
    const { start, current } = dragRef.current;
    dragRef.current = { start: null, current: null };
    setSelection(null);
    const next = start && current ? clampedCustomRange(start, current) : null;
    if (next) {
      onTimeRangeChange?.(next.start, next.end);
    }
  }, [onTimeRangeChange]);

  useEffect(() => {
    const onWindowPointerUp = () => {
      if (dragRef.current.start) {
        commit();
      }
    };
    window.addEventListener('pointerup', onWindowPointerUp);
    return () => window.removeEventListener('pointerup', onWindowPointerUp);
  }, [commit]);

  const onPointerDown = useCallback(
    (event: PlotPointerEvent) => {
      if (!onTimeRangeChange || !domain) {
        return;
      }
      if (typeof event.currentTarget.setPointerCapture === 'function' && event.pointerId != null) {
        event.currentTarget.setPointerCapture(event.pointerId);
      }
      const label = isoAtClientX(event.clientX, event.currentTarget, domain.startMs, domain.endMs);
      dragRef.current = { start: label, current: label };
      const ms = Date.parse(label);
      setSelection({ left: ms, right: ms });
    },
    [domain, onTimeRangeChange],
  );

  const onPointerMove = useCallback(
    (event: PlotPointerEvent) => {
      if (!dragRef.current.start || !domain) {
        return;
      }
      const label = isoAtClientX(event.clientX, event.currentTarget, domain.startMs, domain.endMs);
      dragRef.current.current = label;
      setSelection({ left: Date.parse(dragRef.current.start), right: Date.parse(label) });
    },
    [domain],
  );

  return {
    selection,
    onPointerDown,
    onPointerMove,
    onPointerUp: commit,
  };
}
