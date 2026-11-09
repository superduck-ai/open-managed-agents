import { useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useMemo, useState } from 'react';
import type { DashboardState, StateUpdate } from './dashboardState';
import {
  absoluteRange,
  formatAbsolute,
  isRelative,
  rangeProblem,
  refreshMs,
  resolveRange,
  shiftRange,
  zoomOutRange,
  type RawTimeRange,
} from './time/range';

export function useDashboardTime(
  state: DashboardState,
  update: (patch: StateUpdate, mode?: 'push' | 'replace') => void,
) {
  const queryClient = useQueryClient();
  const [anchor, setAnchor] = useState(Date.now);
  const resolved = useMemo(
    () => resolveRange(state.range, anchor, state.timeZone),
    [state.range, anchor, state.timeZone],
  );
  const problem = resolved ? null : rangeProblem(state.range, anchor, state.timeZone);
  const timeWindow = useMemo(
    () =>
      resolved
        ? { startTime: new Date(resolved.start).toISOString(), endTime: new Date(resolved.end).toISOString() }
        : null,
    [resolved],
  );
  const relative = isRelative(state.range);
  const refresh = useCallback(() => {
    if (relative) {
      setAnchor(Date.now());
      return;
    }
    void queryClient.invalidateQueries({ queryKey: ['observability-dashboard'] });
    void queryClient.invalidateQueries({ queryKey: ['observability-session-options'] });
  }, [queryClient, relative]);
  const setRange = useCallback(
    (range: RawTimeRange) => {
      setAnchor(Date.now());
      update({ range }, 'push');
    },
    [update],
  );
  const shift = useCallback(
    (direction: -1 | 1) => resolved && setRange(shiftRange(resolved, direction, Date.now())),
    [resolved, setRange],
  );
  const zoomOut = useCallback(
    () => resolved && setRange(zoomOutRange(state.range, resolved)),
    [resolved, state.range, setRange],
  );
  const zoomTo = useCallback((start: number, end: number) => setRange(absoluteRange(start, end)), [setRange]);
  useEffect(() => {
    const reset = () => setAnchor(Date.now());
    window.addEventListener('popstate', reset);
    return () => window.removeEventListener('popstate', reset);
  }, []);
  useEffect(() => {
    const interval = refreshMs(state.refresh);
    if (!interval) return;
    const timer = setInterval(() => {
      if (document.visibilityState === 'visible') refresh();
    }, interval);
    return () => clearInterval(timer);
  }, [state.refresh, refresh]);
  const title = resolved
    ? `${formatAbsolute(resolved.start, state.timeZone)} — ${formatAbsolute(resolved.end, state.timeZone)}`
    : '';
  return {
    anchor,
    timeWindow,
    problem,
    title,
    refresh,
    setRange,
    shift,
    zoomOut,
    zoomTo,
  };
}
