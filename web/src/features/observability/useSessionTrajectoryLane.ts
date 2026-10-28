import { useCallback, useMemo, useState } from 'react';
import type { TrajectoryPage, TrajectorySegment, TrajectoryTarget } from './api';

type LaneState = { lane: string; segment?: string; segments: TrajectorySegment[] };

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
        return sameSegments(base.segments, page.segments) ? base : { ...base, segments: page.segments };
      }),
    [lane],
  );
  const select = useCallback(
    (segment: string) =>
      setState((current) => ({ ...(current.lane === lane ? current : { lane, segments: [] }), segment })),
    [lane],
  );
  const selectedIndex = active.segment
    ? active.segments.findIndex((segment) => segment.id === active.segment)
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
