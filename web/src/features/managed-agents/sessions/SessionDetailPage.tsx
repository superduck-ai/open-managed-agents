import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '../../../shared/ui/dropdown-menu';
import { toast } from '../../../shared/ui/sonner';
import { TooltipProvider } from '../../../shared/ui/tooltip';
import { useWorkspace } from '../../../shared/workspaces/context';
import { archiveManagedEntity, deleteManagedEntity, listAllSessionThreads, listSessionResourcesForDetail, retrieveSessionDetailSession, SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS, sessionThreadListSignature } from '../api';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import { ConfirmEntityDialog, ManagedErrorAlert, ManagedWarningAlert } from '../components/common';
import { type EventsTabProps, type QuickstartSessionEvent, type ResourceConfig, type SessionApiResponse, type SessionDebugDetailTab, type SessionEventListEntry, type SessionResourceApiResponse, type SessionThreadApiResponse, type SessionTraceFilterOption, type SessionTraceView } from '../types';
import { compactEntityId, copyText, errorMessage, managedEntityListHref } from '../utils';
import clsx from 'clsx';
import { Archive, ChevronDown, Copy, RotateCcw, X } from 'lucide-react';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { SessionDetailDeltaFramesContext, useSessionDetailEventData } from './sessionDetailData';
import { buildSessionDetailFilterOptions, buildSessionDetailLaneState, buildSessionDetailSummary, buildSessionEventsByLane, buildSessionTimeline, buildSessionTimelineVisibleIds, flattenSessionEntriesByLane, nearestSessionEventEntry, readSessionArchivedLanePreference, readSessionDetailInitialEventId, readSessionDetailInitialLaneId, readSessionDetailInitialView, resolveSelectedSessionEventEntry, scrollSessionEntryIntoView, sessionDetailEventCopyPayload, sessionEventEntryMatchesSelectedId, sessionEventEntryRowId, sessionEventEntrySelectionId, sessionEventListFilterValue, sessionEventUpdateTimestamp, sessionShouldStreamEvents, sessionStatusFromEventType, writeSessionArchivedLanePreference, writeSessionDetailUrlState } from './sessionDetailModel';
import { EventsMinimap, LaneTabStrip, scrollSessionEntryToOffset, SESSION_MAIN_LANE_ID, SessionStatusPill, SessionSummaryChip, sessionTimelineNow } from './sessionTimeline';
import { buildSessionEventEntries, compareSessionEvents, sessionEventTimestamp, sessionEventType } from './sessionTraceModel';
import { EventDetailPanel, SessionEventTypeFilter, SessionTraceEmpty, SessionTraceSearch, SessionTraceSkeleton, SessionTraceViewMode } from './SessionTracePanel';
import { DebugRow, TranscriptRow } from './sessionTraceRows';

export function SessionDetailPage({ config, sessionId }: { config: ResourceConfig; sessionId: string }) {
  const { activeWorkspaceId } = useWorkspace();
  const { msg } = useI18n();
  const formatters = useFormatters();
  const listHref = managedEntityListHref(activeWorkspaceId, 'sessions');
  const [session, setSession] = useState<SessionApiResponse | null>(null);
  const [resources, setResources] = useState<SessionResourceApiResponse[]>([]);
  const [threads, setThreads] = useState<SessionThreadApiResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [metadataError, setMetadataError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [view, setView] = useState<SessionTraceView>(readSessionDetailInitialView);
  const [query, setQuery] = useState('');
  const [selectedTypes, setSelectedTypes] = useState<string[]>([]);
  const [selectedLaneId, setSelectedLaneId] = useState(readSessionDetailInitialLaneId);
  const [showArchivedLanes, setShowArchivedLanesState] = useState(readSessionArchivedLanePreference);
  const [selectedEntryId, setSelectedEntryId] = useState<string | null>(readSessionDetailInitialEventId);
  const [selectedDetailTab, setSelectedDetailTab] = useState<SessionDebugDetailTab>('content');
  const [confirmAction, setConfirmAction] = useState<'archive' | 'delete' | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [metadataLoaded, setMetadataLoaded] = useState(false);
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const detailPanelRef = useRef<HTMLDivElement | null>(null);
  const lastEntryCountRef = useRef(0);
  const skipNextAutoFollowRef = useRef(false);
  const suppressScrollSeekUntilRef = useRef(0);
  const threadRefreshTimerRef = useRef<number | null>(null);
  const setShowArchivedLanes = (value: boolean) => {
    setShowArchivedLanesState(value);
    writeSessionArchivedLanePreference(value);
  };
  const refreshSessionThreads = useCallback(() => {
    if (!session?.id) {
      return;
    }
    const activeSessionId = session.id;
    if (threadRefreshTimerRef.current !== null) {
      return;
    }
    threadRefreshTimerRef.current = window.setTimeout(() => {
      threadRefreshTimerRef.current = null;
      void (async () => {
        const threadsPage = await listAllSessionThreads(activeSessionId, activeWorkspaceId);
        const nextThreads = threadsPage.data ?? [];
        setThreads((currentThreads) =>
          sessionThreadListSignature(currentThreads) === sessionThreadListSignature(nextThreads) ? currentThreads : nextThreads
        );
      })().catch(() => undefined);
    }, 600);
  }, [activeWorkspaceId, session?.id]);
  const refreshSessionMetadata = useCallback(() => {
    if (!session?.id) {
      return;
    }
    const activeSessionId = session.id;
    void retrieveSessionDetailSession(activeSessionId, activeWorkspaceId)
      .then((updatedSession) => setSession(updatedSession))
      .catch(() => undefined);
  }, [activeWorkspaceId, session?.id]);
  const activeSessionId = session?.id ?? null;
  const handlePrimaryStreamEvent = useCallback((event: QuickstartSessionEvent) => {
    const type = sessionEventType(event);
    const nextStatus = sessionStatusFromEventType(type);
    if (nextStatus) {
      setSession((currentSession) => currentSession
        && currentSession.id === activeSessionId
        ? {
            ...currentSession,
            status: nextStatus,
            updated_at: sessionEventUpdateTimestamp(event, currentSession.updated_at),
            archived_at: type === 'session.deleted'
              ? currentSession.archived_at ?? sessionEventUpdateTimestamp(event, currentSession.updated_at)
              : currentSession.archived_at
          }
        : currentSession);
    }
    if (type === 'session.thread_created') {
      refreshSessionThreads();
    } else if (type === 'session.updated') {
      refreshSessionMetadata();
    }
  }, [activeSessionId, refreshSessionMetadata, refreshSessionThreads]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setMetadataError(null);
    setResources([]);
    setThreads([]);
    setMetadataLoaded(false);
    void (async () => {
      try {
        const loadedSession = await retrieveSessionDetailSession(sessionId, activeWorkspaceId);
        if (!active) {
          return;
        }
        setSession(loadedSession);
        setLoading(false);

        const [resourcesResult, threadsResult] = await Promise.allSettled([
          listSessionResourcesForDetail(loadedSession.id, activeWorkspaceId),
          listAllSessionThreads(loadedSession.id, activeWorkspaceId)
        ]);
        if (!active) {
          return;
        }
        const loadedThreads = threadsResult.status === 'fulfilled' ? threadsResult.value.data ?? [] : [];
        if (resourcesResult.status === 'fulfilled') {
          setResources(resourcesResult.value.data ?? []);
        }
        if (threadsResult.status === 'fulfilled') {
          setThreads(loadedThreads);
        }
        setMetadataLoaded(true);
        const settledResults = [resourcesResult, threadsResult] as PromiseSettledResult<unknown>[];
        const firstRejected = settledResults.find(
          (result): result is PromiseRejectedResult => result.status === 'rejected'
        );
        setMetadataError(firstRejected ? errorMessage(firstRejected.reason) : null);
      } catch (error) {
        if (active) {
          setSession(null);
          setLoadError(errorMessage(error));
          setLoading(false);
          setMetadataLoaded(true);
        }
      }
    })();
    return () => {
      active = false;
      if (threadRefreshTimerRef.current !== null) {
        window.clearTimeout(threadRefreshTimerRef.current);
        threadRefreshTimerRef.current = null;
      }
    };
  }, [activeWorkspaceId, refreshKey, sessionId]);

  useEffect(() => {
    if (!session?.id || !sessionShouldStreamEvents(session)) {
      return;
    }
    let active = true;
    const syncThreads = () => {
      void listAllSessionThreads(session.id, activeWorkspaceId)
        .then((threadsPage) => {
          if (!active) {
            return;
          }
          const nextThreads = threadsPage.data ?? [];
          setThreads((currentThreads) =>
            sessionThreadListSignature(currentThreads) === sessionThreadListSignature(nextThreads) ? currentThreads : nextThreads
          );
        })
        .catch(() => undefined);
    };
    const interval = window.setInterval(syncThreads, SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS);
    return () => {
      active = false;
      window.clearInterval(interval);
    };
  }, [activeWorkspaceId, session?.archived_at, session?.id, session?.status]);

  const laneState = useMemo(() => buildSessionDetailLaneState(threads, msg, showArchivedLanes), [msg, showArchivedLanes, threads]);
  const { lanes, threadNameById, laneIdByThreadId, archivedLaneCount, isMultiAgent } = laneState;
  const activeLane = lanes.some((lane) => lane.id === selectedLaneId) ? selectedLaneId : SESSION_MAIN_LANE_ID;
  const eventData = useSessionDetailEventData({
    sessionId: session?.id ?? null,
    workspaceId: activeWorkspaceId,
    threads,
    includeArchivedThreads: showArchivedLanes,
    live: sessionShouldStreamEvents(session),
    onPrimaryEvent: handlePrimaryStreamEvent,
    refreshKey
  });
  const events = eventData.events;
  const eventsLoading = eventData.loading || eventData.childLoading;
  const eventError = eventData.error;
  const sortedEvents = useMemo(() => [...events].sort(compareSessionEvents), [events]);
  const traceStartMs = useMemo(() => {
    const sessionStart = session?.created_at ? Date.parse(session.created_at) : NaN;
    if (Number.isFinite(sessionStart)) {
      return sessionStart;
    }
    return sortedEvents.map(sessionEventTimestamp).find(Boolean) ?? 0;
  }, [session?.created_at, sortedEvents]);

  const eventsByLaneId = useMemo(
    () => buildSessionEventsByLane(lanes, sortedEvents, laneIdByThreadId),
    [laneIdByThreadId, lanes, sortedEvents]
  );
  const entriesByLaneId = useMemo(() => {
    const nextEntriesByLaneId = new Map<string, SessionEventListEntry[]>();
    lanes.forEach((lane) => {
      nextEntriesByLaneId.set(
        lane.id,
        buildSessionEventEntries(eventsByLaneId.get(lane.id) ?? [], view, traceStartMs, msg, { platformTranscriptFiltering: true })
      );
    });
    return nextEntriesByLaneId;
  }, [eventsByLaneId, lanes, msg, traceStartMs, view]);
  const entries = useMemo(() => entriesByLaneId.get(activeLane) ?? [], [activeLane, entriesByLaneId]);
  const allEntries = useMemo(() => flattenSessionEntriesByLane(lanes, entriesByLaneId), [entriesByLaneId, lanes]);
  const filterOptions = useMemo<SessionTraceFilterOption[]>(() => buildSessionDetailFilterOptions(allEntries, view, msg), [allEntries, msg, view]);
  const filteredEntries = useMemo(() => {
    const selected = new Set(selectedTypes);
    const needle = query.trim().toLowerCase();
    return entries.filter((entry) => {
      const matchesType = selected.size === 0 || selected.has(sessionEventListFilterValue(entry, view));
      const matchesQuery = !needle || entry.searchText.includes(needle);
      return matchesType && matchesQuery;
    });
  }, [entries, query, selectedTypes, view]);
  const selectedEntry = useMemo(
    () => resolveSelectedSessionEventEntry(filteredEntries, selectedEntryId),
    [filteredEntries, selectedEntryId]
  );
  const selectedEntryInAnyLane = useMemo(
    () => resolveSelectedSessionEventEntry(allEntries, selectedEntryId),
    [allEntries, selectedEntryId]
  );
  const hasFilter = query.trim().length > 0 || selectedTypes.length > 0 || activeLane !== SESSION_MAIN_LANE_ID;
  const timeline = useMemo(() => buildSessionTimeline(lanes, entriesByLaneId), [entriesByLaneId, lanes]);
  const timelineVisibleIds = useMemo(
    () => buildSessionTimelineVisibleIds(entriesByLaneId, filteredEntries, timeline, activeLane, selectedTypes, query, view),
    [activeLane, entriesByLaneId, filteredEntries, query, selectedTypes, timeline, view]
  );
  const summary = useMemo(() => (session ? buildSessionDetailSummary(session, resources, sortedEvents, formatters, msg) : null), [formatters, msg, resources, session, sortedEvents]);
  const copyPayload = useMemo(() => sessionDetailEventCopyPayload(filteredEntries, view), [filteredEntries, view]);

  useEffect(() => {
    setSelectedTypes([]);
    setQuery('');
    setSelectedDetailTab('content');
  }, [view]);

  useEffect(() => {
    writeSessionDetailUrlState(view, selectedEntryId, selectedLaneId, showArchivedLanes);
  }, [selectedEntryId, selectedLaneId, showArchivedLanes, view]);

  useEffect(() => {
    if (!metadataLoaded) {
      return;
    }
    if (!lanes.some((lane) => lane.id === selectedLaneId)) {
      setSelectedLaneId(SESSION_MAIN_LANE_ID);
      setSelectedEntryId(null);
      setSelectedDetailTab('content');
    }
  }, [lanes, metadataLoaded, selectedLaneId]);

  useEffect(() => {
    if (!metadataLoaded || eventsLoading) {
      return;
    }
    if (selectedEntryId && selectedEntryInAnyLane && !selectedEntry) {
      setSelectedEntryId(null);
    }
  }, [eventsLoading, metadataLoaded, selectedEntry, selectedEntryId, selectedEntryInAnyLane]);

  useEffect(() => {
    if (!selectedEntry) {
      return;
    }
    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (target instanceof Node && detailPanelRef.current?.contains(target)) {
        return;
      }
      setSelectedEntryId(null);
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setSelectedEntryId(null);
      }
    };
    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [selectedEntry]);

  useEffect(() => {
    const scroller = scrollerRef.current;
    if (!scroller) {
      return;
    }
    const previousCount = lastEntryCountRef.current;
    lastEntryCountRef.current = filteredEntries.length;
    if (skipNextAutoFollowRef.current) {
      skipNextAutoFollowRef.current = false;
      scroller.scrollTop = 0;
      return;
    }
    if (!filteredEntries.length) {
      return;
    }
    const distanceFromBottom = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight;
    if (filteredEntries.length > previousCount || distanceFromBottom < 96) {
      scroller.scrollTop = scroller.scrollHeight;
    }
  }, [filteredEntries.length]);

  const handleCopy = async (value: string, message: string) => {
    try {
      await copyText(value);
      toast.success(message);
    } catch (error) {
      setMutationError(errorMessage(error));
    }
  };
  const handleThreadClick = (threadId: string, processedAtMs: number, eventType: string) => {
    const laneId = laneIdByThreadId.get(threadId) ?? (lanes.some((lane) => lane.id === threadId) ? threadId : '');
    if (!laneId) {
      return;
    }
    suppressScrollSeekUntilRef.current = sessionTimelineNow() + 200;
    setSelectedLaneId(laneId);
    const laneEntries = (entriesByLaneId.get(laneId) ?? []).filter(
      (entry): entry is Extract<SessionEventListEntry, { event: QuickstartSessionEvent }> => 'event' in entry
    );
    const timedEntries = laneEntries.filter((entry) => Number.isFinite(entry.processedAtMs));
    const matchingEntries = timedEntries.filter(
      (entry) => sessionEventType(entry.event) === eventType && Math.abs(entry.processedAtMs - processedAtMs) <= 2000
    );
    const targetEntry = nearestSessionEventEntry(matchingEntries.length ? matchingEntries : timedEntries, processedAtMs);
    setSelectedEntryId(targetEntry?.id ?? null);
    setSelectedDetailTab('content');
    if (targetEntry) {
      window.setTimeout(() => scrollSessionEntryIntoView(scrollerRef.current, sessionEventEntryRowId(targetEntry)), 0);
    }
  };
  const handleSelectLane = useCallback((laneId: string, targetEntryId?: string | null) => {
    suppressScrollSeekUntilRef.current = sessionTimelineNow() + 200;
    skipNextAutoFollowRef.current = true;
    setSelectedLaneId(laneId);
    setSelectedEntryId(targetEntryId ?? null);
    setSelectedDetailTab('content');
    if (targetEntryId) {
      const targetEntry = resolveSelectedSessionEventEntry(entriesByLaneId.get(laneId) ?? [], targetEntryId);
      window.setTimeout(() => scrollSessionEntryToOffset(scrollerRef.current, targetEntry ? sessionEventEntryRowId(targetEntry) : targetEntryId), 0);
    } else if (scrollerRef.current) {
      scrollerRef.current.scrollTop = 0;
    }
  }, [entriesByLaneId]);
  const handleTimelineSeek = useCallback((entryId: string | null) => {
    setSelectedEntryId(entryId);
    setSelectedDetailTab('content');
  }, []);
  const handleArchive = async () => {
    if (!session) {
      return;
    }
    setBusyAction('archive');
    setMutationError(null);
    try {
      const updated = await archiveManagedEntity('sessions', session.id, activeWorkspaceId);
      setSession(updated as SessionApiResponse);
      toast.success(msg('managedAgents.sessions.detail.archivedToast', 'Session archived'));
      setConfirmAction(null);
    } catch (error) {
      setMutationError(errorMessage(error));
      setConfirmAction(null);
    } finally {
      setBusyAction(null);
    }
  };
  const handleDelete = async () => {
    if (!session) {
      return;
    }
    setBusyAction('delete');
    setMutationError(null);
    try {
      await deleteManagedEntity('sessions', session.id, activeWorkspaceId);
      setConfirmAction(null);
      setBusyAction(null);
      window.location.assign(listHref);
    } catch (error) {
      setMutationError(errorMessage(error));
      setConfirmAction(null);
      setBusyAction(null);
    }
  };

  if (loading) {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <ManagedDetailBreadcrumb listHref={listHref} listLabel={config.title} />
        <div className="mt-14 text-sm text-muted-foreground">{msg('managedAgents.sessions.detail.loading', 'Loading session...')}</div>
      </section>
    );
  }

  if (!session || loadError || !summary) {
    return (
      <section className="min-h-[calc(100vh-48px)] text-foreground">
        <ManagedDetailBreadcrumb listHref={listHref} listLabel={config.title} />
        <ManagedErrorAlert className="mt-6 max-w-xl">
          {loadError || msg('managedAgents.sessions.detail.notFound', 'Session not found')}
        </ManagedErrorAlert>
      </section>
    );
  }

  const archived = Boolean(session.archived_at);

  return (
    <TooltipProvider>
      <section className="relative min-h-[calc(100vh-48px)] text-foreground" data-testid="session-detail-page">
        {confirmAction ? (
          <ConfirmEntityDialog
            action={confirmAction}
            section="sessions"
            entity={session}
            busy={busyAction === confirmAction}
            onCancel={() => {
              if (!busyAction) {
                setConfirmAction(null);
              }
            }}
            onConfirm={() => {
              if (confirmAction === 'archive') {
                void handleArchive();
                return;
              }
              void handleDelete();
            }}
          />
        ) : null}
        <ManagedDetailBreadcrumb
          listHref={listHref}
          listLabel={config.title}
          currentLabel={compactEntityId(session.id)}
          className="mb-5 min-w-0"
        />

        <header className="mb-7 flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <h1 className="min-w-0 truncate text-[28px] font-semibold leading-tight text-foreground">{summary.title}</h1>
              <SessionStatusPill status={session.archived_at ? msg('common.archived', 'Archived') : summary.statusLabel} />
            </div>
            <div className="mt-4 flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
              {summary.chips.map((chip) => (
                <SessionSummaryChip key={chip.key} icon={chip.icon}>
                  {chip.value}
                </SessionSummaryChip>
              ))}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="lg"
              className="bg-secondary text-sm font-semibold text-foreground hover:bg-accent"
              onClick={() => setRefreshKey((value) => value + 1)}
            >
              <RotateCcw className="size-4" aria-hidden />
              {msg('managedAgents.sessions.detail.refresh', 'Refresh')}
            </Button>
            <DropdownMenu>
              <DropdownMenuTrigger
                render={(
                  <Button
                    type="button"
                    variant="outline"
                    size="lg"
                    className="bg-secondary text-sm font-semibold text-foreground hover:bg-accent disabled:cursor-wait disabled:opacity-60"
                    disabled={Boolean(busyAction)}
                  />
                )}
              >
                {msg('common.actions', 'Actions')}
                <ChevronDown className="size-4 text-muted-foreground" aria-hidden />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56 bg-popover">
                <DropdownMenuItem className="h-9" onClick={() => setRefreshKey((value) => value + 1)}>
                  <RotateCcw className="size-4" aria-hidden />
                  {msg('managedAgents.sessions.detail.refresh', 'Refresh')}
                </DropdownMenuItem>
                <DropdownMenuItem className="h-9" onClick={() => void handleCopy(session.id, msg('managedAgents.sessions.detail.copiedSessionId', 'Session ID copied'))}>
                  <Copy className="size-4" aria-hidden />
                  {msg('managedAgents.sessions.detail.copySessionId', 'Copy session ID')}
                </DropdownMenuItem>
                <DropdownMenuItem className="h-9" onClick={() => void handleCopy(copyPayload, msg('managedAgents.sessions.detail.copiedCurrentView', 'Current view copied'))}>
                  <Copy className="size-4" aria-hidden />
                  {msg('managedAgents.sessions.detail.copyCurrentView', 'Copy current view')}
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem className="h-9" disabled={archived || busyAction === 'archive'} onClick={() => setConfirmAction('archive')}>
                  <Archive className="size-4" aria-hidden />
                  {msg('common.archive', 'Archive')}
                </DropdownMenuItem>
                <DropdownMenuItem className="h-9" variant="destructive" disabled={busyAction === 'delete'} onClick={() => setConfirmAction('delete')}>
                  <X className="size-4" aria-hidden />
                  {msg('common.delete', 'Delete')}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        {mutationError ? <ManagedErrorAlert className="mb-4 max-w-xl">{mutationError}</ManagedErrorAlert> : null}
        {metadataError || eventError ? (
          <ManagedWarningAlert className="mb-4 max-w-xl">
            {metadataError || eventError}
          </ManagedWarningAlert>
        ) : null}

        <SessionDetailDeltaFramesContext.Provider value={eventData.deltaFrames}>
          <EventsTab
            activeLane={activeLane}
            childLoading={eventsLoading}
            copyPayload={copyPayload}
            detailPanelRef={detailPanelRef}
            entries={entries}
            events={events}
            filteredEntries={filteredEntries}
            filterOptions={filterOptions}
            hasFilter={hasFilter}
            lanes={lanes}
            onClearFilters={() => {
              setSelectedTypes([]);
              setQuery('');
              handleSelectLane(SESSION_MAIN_LANE_ID, null);
            }}
            onCopyAll={() => void handleCopy(copyPayload, msg('managedAgents.sessions.detail.copiedCurrentView', 'Current view copied'))}
            onQueryChange={setQuery}
            onOpenDeltas={(entryId) => {
              setSelectedEntryId(entryId);
              setSelectedDetailTab('deltas');
            }}
            onSelectEntry={(entryId) => {
              setSelectedEntryId(entryId);
              setSelectedDetailTab('content');
            }}
            onSelectLane={handleSelectLane}
            onThreadClick={handleThreadClick}
            onSelectedTypesChange={setSelectedTypes}
            onTimelineSeek={handleTimelineSeek}
            onViewChange={setView}
            query={query}
            scrollerRef={scrollerRef}
            selectedEntry={selectedEntry}
            selectedDetailTab={selectedDetailTab}
            selectedEntryId={selectedEntryId}
            selectedTypes={selectedTypes}
            suppressScrollSeekUntilRef={suppressScrollSeekUntilRef}
            archivedLaneCount={archivedLaneCount}
            isMultiAgent={isMultiAgent}
            showArchivedLanes={showArchivedLanes}
            timeline={timeline}
            timelineVisibleIds={timelineVisibleIds}
            threadNameById={threadNameById}
            onDetailTabChange={setSelectedDetailTab}
            onToggleArchivedLanes={(nextPressed) => setShowArchivedLanes(nextPressed)}
            view={view}
          />
        </SessionDetailDeltaFramesContext.Provider>
      </section>
    </TooltipProvider>
  );
}

export function EventsTab(props: EventsTabProps) {
  return <EventsTabInner {...props} />;
}

export function EventsTabInner({
  activeLane,
  archivedLaneCount,
  childLoading,
  entries,
  events,
  filteredEntries,
  filterOptions,
  hasFilter,
  isMultiAgent,
  lanes,
  onClearFilters,
  onCopyAll,
  onDetailTabChange,
  onOpenDeltas,
  onQueryChange,
  onSelectEntry,
  onSelectLane,
  onThreadClick,
  onSelectedTypesChange,
  onTimelineSeek,
  onToggleArchivedLanes,
  onViewChange,
  query,
  scrollerRef,
  selectedEntry,
  selectedDetailTab,
  selectedEntryId,
  selectedTypes,
  showArchivedLanes,
  suppressScrollSeekUntilRef,
  threadNameById,
  timeline,
  timelineVisibleIds,
  view,
  detailPanelRef
}: EventsTabProps) {
  const { msg } = useI18n();
  return (
    <div data-testid="events-tab">
      <KeyboardShortcutsModal />
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-0 py-3">
        <div className="flex min-w-0 flex-wrap items-center gap-3">
          <ViewModeSegment value={view} onChange={onViewChange} />
          <div className="h-5 w-px bg-accent" aria-hidden />
          <SessionEventTypeFilter options={filterOptions} selectedTypes={selectedTypes} view={view} onChange={onSelectedTypesChange} />
          <ExpandingSearch value={query} onChange={onQueryChange} />
        </div>
        <Button
          type="button"
          variant="ghost"
          className="h-8 px-2 text-sm font-medium text-foreground hover:bg-accent"
          onClick={onCopyAll}
        >
          <Copy className="size-4 text-muted-foreground" aria-hidden />
          {msg('managedAgents.sessions.detail.copyAll', 'Copy all')}
        </Button>
      </div>

      <EventsMinimap
        lanes={timeline}
        activeLane={activeLane}
        selectedEntryId={selectedEntry?.id ?? selectedEntryId}
        visibleIds={timelineVisibleIds}
        scrollerRef={scrollerRef}
        suppressScrollSeekUntilRef={suppressScrollSeekUntilRef}
        onLaneChange={onSelectLane}
        onSeek={onTimelineSeek}
      />

      <div className="flex min-h-0 flex-col border-t border-border" data-testid="session-trace-shell">
        <LaneTabStrip
          lanes={lanes}
          activeLane={activeLane}
          archivedLaneCount={archivedLaneCount}
          isMultiAgent={isMultiAgent}
          selectedEntryId={selectedEntry?.id ?? selectedEntryId}
          showArchivedLanes={showArchivedLanes}
          timeline={timeline}
          timelineVisibleIds={timelineVisibleIds}
          onChange={onSelectLane}
          onToggleArchivedLanes={onToggleArchivedLanes}
        />

        <div
          className={clsx(
            'grid min-h-[420px]',
            selectedEntry ? 'lg:grid-cols-[minmax(0,1fr)_minmax(360px,44%)]' : 'grid-cols-1'
          )}
        >
          <div
            ref={scrollerRef}
            data-testid="session-trace-list-pane"
            className={clsx(
              'subtle-scrollbar max-h-[calc(100vh-330px)] min-h-[420px] min-w-0 overflow-x-hidden overflow-y-auto px-0 py-3',
              selectedEntry && 'lg:border-r lg:border-border'
            )}
          >
            {childLoading && !events.length ? (
              <SessionTraceSkeleton />
            ) : filteredEntries.length ? (
              <div className="flex flex-col pb-8">
                {filteredEntries.map((entry) => (
                  view === 'debug' && entry.kind === 'debug' ? (
                    <DebugRow
                      key={entry.id}
                      entry={entry}
                      selected={sessionEventEntryMatchesSelectedId(entry, selectedEntryId)}
                      onSelect={() => onSelectEntry(entry.displayEvent.id)}
                      onOpenDeltas={() => onOpenDeltas(entry.displayEvent.id)}
                    />
                  ) : (
                    <TranscriptRow
                      key={entry.id}
                      entry={entry}
                      selected={sessionEventEntryMatchesSelectedId(entry, selectedEntryId)}
                      onSelect={() => onSelectEntry(sessionEventEntrySelectionId(entry))}
                      threadNameById={threadNameById}
                      onThreadClick={onThreadClick}
                    />
                  )
                ))}
              </div>
            ) : (
              <SessionTraceEmpty
                message={
                  entries.length === 0
                    ? msg('managedAgents.sessions.trace.noEvents', 'No events yet. Events will appear here as they occur.')
                    : msg('managedAgents.sessions.trace.noMatchingEvents', 'No events match the current filters.')
                }
                onClear={hasFilter ? onClearFilters : undefined}
              />
            )}
          </div>
          {selectedEntry ? (
            <div ref={detailPanelRef} data-testid="session-event-detail-panel" className="min-h-0">
              <EventDetailPanel
                entry={selectedEntry}
                view={view}
                detailTab={selectedDetailTab}
                placement="side"
                onClose={() => onSelectEntry(null)}
                onDetailTabChange={onDetailTabChange}
              />
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}

export function ViewModeSegment(props: { value: SessionTraceView; onChange: (value: SessionTraceView) => void }) {
  return <SessionTraceViewMode {...props} />;
}

export function ExpandingSearch(props: { value: string; onChange: (value: string) => void }) {
  return <SessionTraceSearch {...props} />;
}

export function KeyboardShortcutsModal() {
  return null;
}

export * from './SessionTracePanel';
export * from './sessionTraceModel';
export * from './sessionDetailData';
export * from './sessionTimeline';
export * from './sessionTraceRows';
export * from './sessionDetailModel';
