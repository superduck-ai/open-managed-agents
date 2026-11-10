import { useQuery } from '@tanstack/react-query';
import { useMemo, useRef, useState } from 'react';

import { useI18n } from '../../shared/i18n';
import { listAllSessionThreads } from '../managed-agents/api';
import { useSessionDetailEventData } from '../managed-agents/sessions/sessionDetailData';
import {
  buildSessionDetailLaneState,
  buildSessionEventsByLane,
  buildSessionTimeline,
  buildSessionTimelineVisibleIds,
  resolveSelectedSessionEventEntry,
  sessionEventEntryRowId,
} from '../managed-agents/sessions/sessionDetailModel';
import { buildSessionEntriesByLane } from '../managed-agents/sessions/sessionLaneEntries';
import { SESSION_MAIN_LANE_ID, scrollSessionEntryToOffset } from '../managed-agents/sessions/sessionTimeline';
import { latestOpenModelRequest, sessionEventTimestamp } from '../managed-agents/sessions/sessionTraceModel';
import type { SessionThreadApiResponse } from '../managed-agents/types';
import { useEvaluationScope } from './useEvaluationScope';

const NO_THREADS: SessionThreadApiResponse[] = [];

function useTimelineSelection() {
  const [laneId, setLaneId] = useState(SESSION_MAIN_LANE_ID);
  const [query, setQuery] = useState('');
  const [selectedEntryId, setSelectedEntryId] = useState<string | null>(null);
  const [hoveredEventId, setHoveredEventId] = useState<string | null>(null);
  const [showArchivedLanes, setShowArchivedLanes] = useState(false);
  return {
    laneId,
    setLaneId,
    query,
    setQuery,
    selectedEntryId,
    setSelectedEntryId,
    hoveredEventId,
    setHoveredEventId,
    showArchivedLanes,
    setShowArchivedLanes,
  };
}

export function useItemSessionTimeline(sessionId: string, live: boolean) {
  const { msg } = useI18n();
  const scope = useEvaluationScope();
  const selection = useTimelineSelection();
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const suppressScrollSeekUntilRef = useRef(0);
  const threadsQuery = useQuery({
    queryKey: ['evaluation-trace-threads', scope.workspaceId, sessionId],
    queryFn: async () => (await listAllSessionThreads(sessionId, scope.workspaceId)).data ?? [],
    enabled: scope.ready,
    refetchInterval: live ? 5000 : false,
  });
  const threads = threadsQuery.data ?? NO_THREADS;
  const eventData = useSessionDetailEventData({
    sessionId: scope.ready ? sessionId : null,
    workspaceId: scope.workspaceId,
    threads,
    includeArchivedThreads: selection.showArchivedLanes,
    live,
    refreshKey: 0,
  });
  const events = eventData.events;
  const laneState = useMemo(
    () => buildSessionDetailLaneState(threads, msg, selection.showArchivedLanes),
    [msg, selection.showArchivedLanes, threads],
  );
  const { lanes, laneIdByThreadId } = laneState;
  const activeLane = lanes.some((lane) => lane.id === selection.laneId) ? selection.laneId : SESSION_MAIN_LANE_ID;
  const traceStartMs = useMemo(() => events.map(sessionEventTimestamp).find(Boolean) ?? 0, [events]);
  const eventsByLaneId = useMemo(
    () => buildSessionEventsByLane(lanes, events, laneIdByThreadId),
    [events, laneIdByThreadId, lanes],
  );
  const entriesByLaneId = useMemo(
    () => buildSessionEntriesByLane(lanes, events, eventsByLaneId, traceStartMs, msg),
    [events, eventsByLaneId, lanes, msg, traceStartMs],
  );
  const entries = entriesByLaneId.get(activeLane) ?? [];
  const needle = selection.query.trim().toLowerCase();
  const filteredEntries = needle ? entries.filter((entry) => entry.searchText.includes(needle)) : entries;
  const timeline = useMemo(
    () => buildSessionTimeline(lanes, entriesByLaneId, laneIdByThreadId),
    [entriesByLaneId, laneIdByThreadId, lanes],
  );
  const selectLane = (nextLaneId: string, targetEntryId?: string | null) => {
    suppressScrollSeekUntilRef.current = Date.now() + 200;
    selection.setLaneId(nextLaneId);
    selection.setSelectedEntryId(targetEntryId ?? null);
    const target = resolveSelectedSessionEventEntry(entriesByLaneId.get(nextLaneId) ?? [], targetEntryId ?? null);
    if (target) {
      window.setTimeout(() => scrollSessionEntryToOffset(scrollerRef.current, sessionEventEntryRowId(target)), 0);
    }
  };
  return {
    ...laneState,
    activeLane,
    eventData,
    entries,
    filteredEntries,
    timeline,
    timelineVisibleIds: buildSessionTimelineVisibleIds(filteredEntries, timeline, activeLane, selection.query),
    openModelRequest: needle ? null : latestOpenModelRequest(eventsByLaneId.get(activeLane) ?? []),
    selectedEntry: resolveSelectedSessionEventEntry(filteredEntries, selection.selectedEntryId),
    traceStartMs,
    scrollerRef,
    suppressScrollSeekUntilRef,
    selection,
    selectLane,
  };
}
