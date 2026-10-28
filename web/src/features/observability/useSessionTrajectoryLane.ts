import { useCallback, useMemo, useState } from 'react';
import type { TrajectoryPage, TrajectorySegment, TrajectoryTarget } from './api';

type LaneState = { lane: string; segment?: string; pinned?: string; segments: TrajectorySegment[] };

function sameSegments(left: TrajectorySegment[], right: TrajectorySegment[]) {
  return left.length === right.length && left.every((segment, index) => segment.id === right[index].id);
}

export function useSessionTrajectoryLane(sessionId: string, threadId?: string) {
  const lane = `${sessionId}:${threadId ?? ''}`;
  const [state, setState] = useState<LaneState>({ lane, segments: [] });
  if (state.lane !== lane) setState({ lane, segments: [] });
  const active: LaneState = state.lane === lane ? state : { lane, segments: [] };
  const target = useMemo<TrajectoryTarget>(
    () => ({ sessionId, threadId: threadId || undefined, segment: active.segment }),
    [sessionId, threadId, active.segment],
  );
  const onPage = useCallback(
    (page: TrajectoryPage) =>
      setState((current) => {
        const base: LaneState = current.lane === lane ? current : { lane, segments: [] };
        const pinned = base.pinned ?? (page.segment || undefined);
        return sameSegments(base.segments, page.segments) && pinned === base.pinned
          ? base
          : { ...base, pinned, segments: page.segments };
      }),
    [lane],
  );
  const select = useCallback(
    (segment: string) =>
      setState((current) => ({ ...(current.lane === lane ? current : { lane, segments: [] }), segment })),
    [lane],
  );
  const shown = active.segment ?? active.pinned;
  const selectedIndex = shown
    ? active.segments.findIndex((segment) => segment.id === shown)
    : active.segments.length - 1;
  return {
    viewKey: `${lane}:${active.segment ?? ''}`,
    target,
    onPage,
    select,
    segments: active.segments,
    selectedIndex,
  };
}
