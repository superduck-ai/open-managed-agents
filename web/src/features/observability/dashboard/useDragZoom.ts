import { useState } from 'react';
import type { MouseHandlerDataParam } from 'recharts';
import type { Row } from './series';

interface Drag {
  anchor: number;
  current: number;
}

function activeIndex(state: MouseHandlerDataParam) {
  const index = Number(state.activeTooltipIndex);
  return state.activeTooltipIndex == null || !Number.isInteger(index) ? null : index;
}

export function useDragZoom(rows: Row[], intervalMs: number, onZoom?: (start: number, end: number) => void) {
  const [drag, setDrag] = useState<Drag | null>(null);
  const low = drag ? Math.min(drag.anchor, drag.current) : 0;
  const high = drag ? Math.max(drag.anchor, drag.current) : 0;
  const selecting = !!drag && low !== high && !!onZoom;
  return {
    area: selecting ? { x1: rows[low]?.t ?? undefined, x2: rows[high]?.t ?? undefined } : null,
    handlers: {
      onMouseDown: (state: MouseHandlerDataParam) => {
        const index = activeIndex(state);
        if (index != null) setDrag({ anchor: index, current: index });
      },
      onMouseMove: (state: MouseHandlerDataParam) => {
        const index = activeIndex(state);
        if (drag && index != null && index !== drag.current) setDrag({ ...drag, current: index });
      },
      onMouseUp: () => {
        const start = rows[low]?.t;
        const end = rows[high]?.t;
        if (selecting && start != null && end != null) onZoom(start, end + intervalMs);
        setDrag(null);
      },
      onMouseLeave: () => setDrag(null),
    },
  };
}
