import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react';
import { TRACE_DEFAULT_LEFT_WIDTH, TRACE_MAX_LEFT_WIDTH, TRACE_MIN_LEFT_WIDTH } from './traceLayout';

export function useColumnResize(initial = TRACE_DEFAULT_LEFT_WIDTH) {
  const [width, setWidth] = useState(initial);
  // 拖拽中途组件卸载时也要摘掉 window 监听器，否则回调会继续 setState 并泄漏。
  const stopDragRef = useRef<(() => void) | null>(null);
  useEffect(() => () => stopDragRef.current?.(), []);

  const onResizeStart = useCallback(
    (event: ReactPointerEvent<HTMLElement>) => {
      event.preventDefault();
      const originX = event.clientX;
      const originWidth = width;
      const onMove = (next: PointerEvent) => {
        setWidth(Math.min(TRACE_MAX_LEFT_WIDTH, Math.max(TRACE_MIN_LEFT_WIDTH, originWidth + next.clientX - originX)));
      };
      const stopDrag = () => {
        window.removeEventListener('pointermove', onMove);
        window.removeEventListener('pointerup', stopDrag);
        stopDragRef.current = null;
      };
      stopDragRef.current?.();
      stopDragRef.current = stopDrag;
      window.addEventListener('pointermove', onMove);
      window.addEventListener('pointerup', stopDrag);
    },
    [width],
  );

  return { width, onResizeStart };
}
