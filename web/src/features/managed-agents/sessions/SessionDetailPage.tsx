import { useFormatters, useI18n } from '../../../shared/i18n';
import { Button } from '../../../shared/ui/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '../../../shared/ui/dropdown-menu';
import { toast } from '../../../shared/ui/sonner';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../../shared/ui/tooltip';
import {
  MessageScroller,
  MessageScrollerButton,
  MessageScrollerContent,
  MessageScrollerProvider,
  MessageScrollerViewport,
} from '../../../shared/ui/message-scroller';
import { useWorkspace } from '../../../shared/workspaces/context';
import {
  addSessionFileResource,
  archiveManagedEntity,
  deleteManagedEntity,
  listAllSessionThreads,
  postSessionToolConfirmation,
  retrieveSessionDetailSession,
  SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS,
  sessionThreadListSignature,
} from '../api';
import { ManagedDetailBreadcrumb } from '../components/breadcrumbs';
import { ConfirmEntityDialog, ManagedErrorAlert, ManagedWarningAlert } from '../components/common';
import { resourceTitle } from '../labels';
import {
  type EventsTabProps,
  type QuickstartSessionEvent,
  type ResourceConfig,
  type SessionApiResponse,
  type SessionEventListEntry,
  type SessionFileResourceFormValue,
  type SessionThreadApiResponse,
  type SessionToolConfirmationInput,
  type ToolCallEntry,
} from '../types';
import { compactEntityId, copyText, errorMessage, managedEntityListHref } from '../utils';
import { Archive, ArrowDown, ChevronDown, Copy, PanelRightOpen, RotateCcw, X } from 'lucide-react';
import {
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react';
import { SessionDetailDeltaFramesContext, useSessionDetailEventData } from './sessionDetailData';
import {
  buildSessionDetailLaneState,
  buildSessionDetailSummary,
  buildSessionEventsByLane,
  buildSessionTimeline,
  buildSessionTimelineVisibleIds,
  findActiveAwaitingToolCall,
  flattenSessionEntriesByLane,
  nearestSessionEventEntry,
  readSessionArchivedLanePreference,
  readSessionDetailInitialEventId,
  readSessionDetailInitialLaneId,
  resolveSelectedSessionEventEntry,
  scrollSessionEntryIntoView,
  sessionDetailEventCopyPayload,
  sessionEventEntrySelectionId,
  sessionEventEntryRowId,
  sessionEventUpdateTimestamp,
  sessionShouldStreamEvents,
  sessionStatusIsLive,
  sessionStatusFromEventType,
  writeSessionArchivedLanePreference,
  writeSessionDetailUrlState,
} from './sessionDetailModel';
import {
  EventsMinimap,
  EventsMinimapSkeleton,
  LaneTabStrip,
  scrollSessionEntryToOffset,
  SESSION_MAIN_LANE_ID,
  SessionStatusPill,
  SessionSummaryChip,
  sessionTimelineNow,
} from './sessionTimeline';
import {
  buildSessionEventEntries,
  sessionEventTimestamp,
  sessionEventType,
  latestRequiresActionEventIDs,
  latestOpenModelRequest,
  sessionStatusFromEvents,
} from './sessionTraceModel';
import { SessionTraceEmpty, SessionTraceSearch, SessionTraceSkeleton } from './SessionTracePanel';
import { SessionInspector } from './SessionInspector';
import {
  readSessionInspectorTab,
  type SessionInspectorTab,
  writeSessionInspectorUrlState,
} from './sessionInspectorModel';
import { SessionMessageComposer } from './SessionMessageComposer';
import { SessionRequiresActionCard } from './SessionRequiresActionCard';
import { SessionTranscriptView } from './SessionTranscriptView';
import { SessionTraceWorkspaceLayout } from './SessionTraceWorkspaceLayout';
import { sessionTraceKeyboardTarget } from './sessionTraceInteractions';

const SESSION_CHROME_GUTTER_CLASS_NAME = 'px-4 @min-[640px]:px-6 @min-[1024px]:px-8';

function sessionPendingAction(
  toolCall: ToolCallEntry | null,
  onConfirm: (input: SessionToolConfirmationInput) => Promise<void>,
  disabled: boolean,
) {
  if (!toolCall) return undefined;
  return <SessionRequiresActionCard toolCall={toolCall} onConfirm={onConfirm} disabled={disabled} />;
}

export function SessionDetailPage({ config, sessionId }: { config: ResourceConfig; sessionId: string }) {
  const { activeWorkspaceId } = useWorkspace();
  const { msg } = useI18n();
  const formatters = useFormatters();
  const listHref = managedEntityListHref(activeWorkspaceId, 'sessions');
  const listLabel = resourceTitle(config, msg);
  const [session, setSession] = useState<SessionApiResponse | null>(null);
  const [threads, setThreads] = useState<SessionThreadApiResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [metadataError, setMetadataError] = useState<string | null>(null);
  const [resourceRefreshError, setResourceRefreshError] = useState<string | null>(null);
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);
  const [eventRefreshKey, setEventRefreshKey] = useState(0);
  const [summaryClock, setSummaryClock] = useState(Date.now);
  const [query, setQuery] = useState('');
  const [selectedLaneId, setSelectedLaneId] = useState(readSessionDetailInitialLaneId);
  const [showArchivedLanes, setShowArchivedLanesState] = useState(readSessionArchivedLanePreference);
  const [selectedEntryId, setSelectedEntryId] = useState<string | null>(readSessionDetailInitialEventId);
  const [hoveredEventId, setHoveredEventId] = useState<string | null>(null);
  const [inspectorOpen, setInspectorOpen] = useState(true);
  const [inspectorTab, setInspectorTab] = useState<SessionInspectorTab>(readSessionInspectorTab);
  const [confirmAction, setConfirmAction] = useState<'archive' | 'delete' | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [metadataLoaded, setMetadataLoaded] = useState(false);
  const scrollerRef = useRef<HTMLDivElement | null>(null);
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
          sessionThreadListSignature(currentThreads) === sessionThreadListSignature(nextThreads)
            ? currentThreads
            : nextThreads,
        );
      })().catch(() => undefined);
    }, 600);
  }, [activeWorkspaceId, session?.id]);
  const refreshSessionResources = useCallback(() => {
    if (!session?.id) {
      return;
    }
    const activeSessionId = session.id;
    setResourceRefreshError(null);
    void retrieveSessionDetailSession(activeSessionId, activeWorkspaceId)
      .then((updatedSession) => setSession((currentSession) => mergeSessionResources(currentSession, updatedSession)))
      .catch((error) => setResourceRefreshError(errorMessage(error)));
  }, [activeWorkspaceId, session?.id]);
  const handleAddFileResource = useCallback(
    (resource: SessionFileResourceFormValue) =>
      addSessionFileResource(session!.id, resource, activeWorkspaceId).then(refreshSessionResources),
    [activeWorkspaceId, refreshSessionResources, session],
  );
  const activeSessionId = session?.id ?? null;
  const handlePrimaryStreamEvent = useCallback(
    (event: QuickstartSessionEvent) => {
      const type = sessionEventType(event);
      const nextStatus = sessionStatusFromEventType(type);
      if (nextStatus) {
        setSession((currentSession) =>
          currentSession && currentSession.id === activeSessionId
            ? {
                ...currentSession,
                status: nextStatus,
                updated_at: sessionEventUpdateTimestamp(event, currentSession.updated_at),
                archived_at:
                  type === 'session.deleted'
                    ? (currentSession.archived_at ?? sessionEventUpdateTimestamp(event, currentSession.updated_at))
                    : currentSession.archived_at,
              }
            : currentSession,
        );
      }
      if (type === 'session.thread_created') {
        refreshSessionThreads();
      }
    },
    [activeSessionId, refreshSessionThreads],
  );

  useEffect(() => {
    let active = true;
    setLoading(true);
    setLoadError(null);
    setMetadataError(null);
    setResourceRefreshError(null);
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

        try {
          const threadsPage = await listAllSessionThreads(loadedSession.id, activeWorkspaceId);
          if (active) {
            setThreads(threadsPage.data ?? []);
          }
        } catch (error) {
          if (active) {
            setMetadataError(errorMessage(error));
          }
        } finally {
          if (active) {
            setMetadataLoaded(true);
          }
        }
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
            sessionThreadListSignature(currentThreads) === sessionThreadListSignature(nextThreads)
              ? currentThreads
              : nextThreads,
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

  useEffect(() => {
    setSummaryClock(Date.now());
    const interval = window.setInterval(() => setSummaryClock(Date.now()), sessionSummaryRefreshInterval(session));
    return () => window.clearInterval(interval);
  }, [session?.archived_at, session?.id, session?.status]);

  const laneState = useMemo(
    () => buildSessionDetailLaneState(threads, msg, showArchivedLanes),
    [msg, showArchivedLanes, threads],
  );
  const { lanes, threadNameById, laneIdByThreadId, archivedLaneCount, isMultiAgent } = laneState;
  const activeLane = lanes.some((lane) => lane.id === selectedLaneId) ? selectedLaneId : SESSION_MAIN_LANE_ID;
  const eventData = useSessionDetailEventData({
    sessionId: session?.id ?? null,
    workspaceId: activeWorkspaceId,
    threads,
    includeArchivedThreads: showArchivedLanes,
    live: sessionShouldStreamEvents(session),
    onPrimaryEvent: handlePrimaryStreamEvent,
    refreshKey: refreshKey + eventRefreshKey,
  });
  const events = eventData.events;
  const eventsLoading = eventData.loading || eventData.childLoading;
  const eventError = eventData.error;

  // Reconcile the header status from the event cache, not just live stream frames,
  // so a missed frame (or a reply that fully landed before SSE subscribed) still
  // corrects an optimistic "running". No-op while the statuses agree.
  useEffect(() => {
    const next = sessionStatusFromEvents(events);
    if (!next || !session) {
      return;
    }
    setSession((currentSession) => {
      const currentStatus = currentSession?.status.toLowerCase();
      if (
        !currentSession ||
        currentSession.id !== session.id ||
        ((currentStatus === 'terminated' || currentStatus === 'deleted') && next.status !== 'deleted')
      ) {
        return currentSession;
      }
      // Mirror the live-frame path: a cached session.deleted must also archive,
      // otherwise the header keeps the Archive action enabled.
      const archivedAt =
        next.status === 'deleted'
          ? (currentSession.archived_at ?? sessionEventUpdateTimestamp(next.event, currentSession.updated_at))
          : currentSession.archived_at;
      if (next.status === currentSession.status.toLowerCase() && archivedAt === currentSession.archived_at) {
        return currentSession;
      }
      return { ...currentSession, status: next.status, archived_at: archivedAt };
    });
  }, [events, session]);
  const traceStartMs = useMemo(() => {
    const sessionStart = session?.created_at ? Date.parse(session.created_at) : NaN;
    if (Number.isFinite(sessionStart)) {
      return sessionStart;
    }
    return events.map(sessionEventTimestamp).find(Boolean) ?? 0;
  }, [events, session?.created_at]);

  const eventsByLaneId = useMemo(
    () => buildSessionEventsByLane(lanes, events, laneIdByThreadId),
    [events, laneIdByThreadId, lanes],
  );
  const entriesByLaneId = useMemo(() => {
    const nextEntriesByLaneId = new Map<string, SessionEventListEntry[]>();
    lanes.forEach((lane) => {
      nextEntriesByLaneId.set(
        lane.id,
        buildSessionEventEntries(eventsByLaneId.get(lane.id) ?? [], 'transcript', traceStartMs, msg, {
          platformTranscriptFiltering: true,
        }),
      );
    });
    return nextEntriesByLaneId;
  }, [eventsByLaneId, lanes, msg, traceStartMs]);
  const entries = useMemo(() => entriesByLaneId.get(activeLane) ?? [], [activeLane, entriesByLaneId]);
  const openModelRequest = useMemo(
    () => latestOpenModelRequest(eventsByLaneId.get(activeLane) ?? []),
    [activeLane, eventsByLaneId],
  );
  const allEntries = useMemo(() => flattenSessionEntriesByLane(lanes, entriesByLaneId), [entriesByLaneId, lanes]);
  const inspectorEventEntries = useMemo(
    () => buildSessionEventEntries(events, 'debug', traceStartMs, msg),
    [events, msg, traceStartMs],
  );
  const filteredEntries = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return needle ? entries.filter((entry) => entry.searchText.includes(needle)) : entries;
  }, [entries, query]);
  const selectedEntry = useMemo(
    () => resolveSelectedSessionEventEntry(filteredEntries, selectedEntryId),
    [filteredEntries, selectedEntryId],
  );
  const selectedEntryInAnyLane = useMemo(
    () => resolveSelectedSessionEventEntry(allEntries, selectedEntryId),
    [allEntries, selectedEntryId],
  );
  const selectedInspectorEntry = useMemo(
    () => resolveSelectedSessionEventEntry(inspectorEventEntries, selectedEntryId) ?? selectedEntryInAnyLane,
    [inspectorEventEntries, selectedEntryId, selectedEntryInAnyLane],
  );
  const hoveredEntryInAnyLane = useMemo(
    () => resolveSelectedSessionEventEntry(allEntries, hoveredEventId),
    [allEntries, hoveredEventId],
  );
  const hoveredInspectorEntry = useMemo(
    () => resolveSelectedSessionEventEntry(inspectorEventEntries, hoveredEventId) ?? hoveredEntryInAnyLane,
    [hoveredEntryInAnyLane, hoveredEventId, inspectorEventEntries],
  );
  const hoveredInspectorEventId = hoveredInspectorEntry
    ? sessionEventEntrySelectionId(hoveredInspectorEntry)
    : hoveredEventId;
  const hasFilter = query.trim().length > 0 || activeLane !== SESSION_MAIN_LANE_ID;
  const timeline = useMemo(() => buildSessionTimeline(lanes, entriesByLaneId), [entriesByLaneId, lanes]);
  const timelineVisibleIds = useMemo(
    () => buildSessionTimelineVisibleIds(filteredEntries, timeline, activeLane, query),
    [activeLane, filteredEntries, query, timeline],
  );
  const summary = useMemo(
    () => (session ? buildSessionDetailSummary(session, events, formatters, msg, summaryClock) : null),
    [events, formatters, msg, session, summaryClock],
  );
  const currentViewCopyPayload = useMemo(() => sessionDetailEventCopyPayload(filteredEntries), [filteredEntries]);
  const fullTranscriptCopyPayload = useMemo(() => sessionDetailEventCopyPayload(entries), [entries]);
  const actionEntries = allEntries;
  const requiresActionEventIDs = useMemo(() => latestRequiresActionEventIDs(events), [events]);
  const activeAwaitingToolCall = useMemo(
    () => findActiveAwaitingToolCall(actionEntries, requiresActionEventIDs),
    [actionEntries, requiresActionEventIDs],
  );

  const handleToolConfirmation = useCallback(
    async (input: SessionToolConfirmationInput) => {
      if (!session?.id) {
        return;
      }
      setMutationError(null);
      try {
        const response = await postSessionToolConfirmation(session.id, input, activeWorkspaceId);
        if (response.data?.length) {
          eventData.appendPrimaryEvents(response.data);
        }
        setSession((currentSession) =>
          currentSession && currentSession.id === session.id
            ? { ...currentSession, status: 'running' }
            : currentSession,
        );
        setEventRefreshKey((value) => value + 1);
      } catch (error) {
        setMutationError(errorMessage(error));
        throw error;
      }
    },
    [activeWorkspaceId, eventData, session?.id],
  );

  useEffect(() => {
    writeSessionDetailUrlState(selectedEntryId, selectedLaneId, showArchivedLanes);
  }, [selectedEntryId, selectedLaneId, showArchivedLanes]);

  useEffect(() => {
    writeSessionInspectorUrlState(inspectorTab);
  }, [inspectorTab]);

  useEffect(() => {
    if (!metadataLoaded) {
      return;
    }
    if (!lanes.some((lane) => lane.id === selectedLaneId)) {
      setSelectedLaneId(SESSION_MAIN_LANE_ID);
      setSelectedEntryId(null);
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
    if (!selectedEntryId) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setSelectedEntryId(null);
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [selectedEntryId]);

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
      (entry): entry is Extract<SessionEventListEntry, { event: QuickstartSessionEvent }> => 'event' in entry,
    );
    const timedEntries = laneEntries.filter((entry) => Number.isFinite(entry.processedAtMs));
    const matchingEntries = timedEntries.filter(
      (entry) => sessionEventType(entry.event) === eventType && Math.abs(entry.processedAtMs - processedAtMs) <= 2000,
    );
    const targetEntry = nearestSessionEventEntry(
      matchingEntries.length ? matchingEntries : timedEntries,
      processedAtMs,
    );
    setSelectedEntryId(targetEntry?.id ?? null);
    if (targetEntry) {
      window.setTimeout(() => scrollSessionEntryIntoView(scrollerRef.current, sessionEventEntryRowId(targetEntry)), 0);
    }
  };
  const handleSelectLane = useCallback(
    (laneId: string, targetEntryId?: string | null) => {
      suppressScrollSeekUntilRef.current = sessionTimelineNow() + 200;
      setSelectedLaneId(laneId);
      setSelectedEntryId(targetEntryId ?? null);
      if (targetEntryId) {
        const targetEntry = resolveSelectedSessionEventEntry(entriesByLaneId.get(laneId) ?? [], targetEntryId);
        window.setTimeout(
          () =>
            scrollSessionEntryToOffset(
              scrollerRef.current,
              targetEntry ? sessionEventEntryRowId(targetEntry) : targetEntryId,
            ),
          0,
        );
      } else if (scrollerRef.current) {
        scrollerRef.current.scrollTop = 0;
      }
    },
    [entriesByLaneId],
  );
  const handleTimelineSeek = useCallback((entryId: string | null) => {
    setSelectedEntryId(entryId);
  }, []);
  const handleArchive = async () => {
    if (!session) return;
    setBusyAction('archive');
    setMutationError(null);
    try {
      setSession((await archiveManagedEntity('sessions', session.id, activeWorkspaceId)) as SessionApiResponse);
      toast.success(msg('managedAgents.sessions.detail.archivedToast', 'Session archived'));
    } catch (error) {
      setMutationError(errorMessage(error));
    } finally {
      setConfirmAction(null);
      setBusyAction(null);
    }
  };
  const handleDelete = async () => {
    if (!session) return;
    setBusyAction('delete');
    setMutationError(null);
    try {
      await deleteManagedEntity('sessions', session.id, activeWorkspaceId);
      window.location.assign(listHref);
    } catch (error) {
      setMutationError(errorMessage(error));
    } finally {
      setConfirmAction(null);
      setBusyAction(null);
    }
  };
  if (loading) {
    return (
      <section className="@container min-h-[calc(100vh-48px)] text-foreground">
        <div className={SESSION_CHROME_GUTTER_CLASS_NAME}>
          <ManagedDetailBreadcrumb listHref={listHref} listLabel={listLabel} />
          <div className="mt-14 text-sm text-muted-foreground">
            {msg('managedAgents.sessions.detail.loading', 'Loading session...')}
          </div>
        </div>
      </section>
    );
  }

  if (!session || loadError || !summary) {
    return (
      <section className="@container min-h-[calc(100vh-48px)] text-foreground">
        <div className={SESSION_CHROME_GUTTER_CLASS_NAME}>
          <ManagedDetailBreadcrumb listHref={listHref} listLabel={listLabel} />
          <ManagedErrorAlert className="mt-6 max-w-xl">
            {loadError || msg('managedAgents.sessions.detail.notFound', 'Session not found')}
          </ManagedErrorAlert>
        </div>
      </section>
    );
  }

  const archived = Boolean(session.archived_at);
  const conversationState = sessionConversationState(session);
  const warningError = [resourceRefreshError, metadataError, eventError].find(Boolean);

  return (
    <TooltipProvider>
      <section
        className="@container relative flex min-h-0 w-full flex-1 flex-col overflow-hidden text-foreground"
        data-testid="session-detail-page"
      >
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
        <header className={`mb-3 min-w-0 ${SESSION_CHROME_GUTTER_CLASS_NAME}`} data-testid="session-detail-header">
          <div
            className="grid grid-cols-1 items-center gap-2 @min-[768px]:grid-cols-[minmax(0,1fr)_auto]"
            data-testid="session-detail-header-utility-row"
          >
            <ManagedDetailBreadcrumb
              listHref={listHref}
              listLabel={listLabel}
              currentLabel={compactEntityId(session.id)}
              className="min-w-0"
            />
            <div className="flex items-center gap-2 @min-[768px]:justify-self-end">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      type="button"
                      variant="outline"
                      size="default"
                      className="bg-background text-sm font-medium text-foreground disabled:cursor-wait disabled:opacity-60"
                      disabled={Boolean(busyAction)}
                    />
                  }
                >
                  {msg('common.actions', 'Actions')}
                  <ChevronDown className="size-4 text-muted-foreground" aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-56 bg-popover">
                  <DropdownMenuItem className="h-9" onClick={() => setRefreshKey((value) => value + 1)}>
                    <RotateCcw className="size-4" aria-hidden />
                    {msg('managedAgents.sessions.detail.refresh', 'Refresh')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="h-9"
                    onClick={() =>
                      void handleCopy(
                        session.id,
                        msg('managedAgents.sessions.detail.copiedSessionId', 'Session ID copied'),
                      )
                    }
                  >
                    <Copy className="size-4" aria-hidden />
                    {msg('managedAgents.sessions.detail.copySessionId', 'Copy session ID')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="h-9"
                    onClick={() =>
                      void handleCopy(
                        currentViewCopyPayload,
                        msg('managedAgents.sessions.detail.copiedCurrentView', 'Current view copied'),
                      )
                    }
                  >
                    <Copy className="size-4" aria-hidden />
                    {msg('managedAgents.sessions.detail.copyCurrentView', 'Copy current view')}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="h-9"
                    disabled={archived || busyAction === 'archive'}
                    onClick={() => setConfirmAction('archive')}
                  >
                    <Archive className="size-4" aria-hidden />
                    {msg('common.archive', 'Archive')}
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="h-9"
                    variant="destructive"
                    disabled={busyAction === 'delete'}
                    onClick={() => setConfirmAction('delete')}
                  >
                    <X className="size-4" aria-hidden />
                    {msg('common.delete', 'Delete')}
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </div>
          </div>

          <div className="mt-3 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1.5">
            <div className="flex min-w-0 max-w-full items-center gap-2">
              <h1 className="min-w-0 truncate font-serif text-[28px] font-medium leading-[1.3] tracking-[-0.015em] text-foreground">
                {summary.title}
              </h1>
              <SessionStatusPill
                status={session.archived_at ? msg('common.archived', 'Archived') : summary.statusLabel}
              />
            </div>
            {summary.chips.map((chip) => (
              <SessionSummaryChip key={chip.key} icon={chip.icon} tooltip={chip.tooltip}>
                {chip.value}
              </SessionSummaryChip>
            ))}
          </div>
        </header>

        <SessionDetailAlerts mutationError={mutationError} warningError={warningError} />

        <div className="min-h-0 flex-1 overflow-hidden pt-1" data-testid="session-viewer">
          <SessionDetailDeltaFramesContext.Provider value={eventData.deltaFrames}>
            <EventsTab
              activeLane={activeLane}
              archivedLaneCount={archivedLaneCount}
              childLoading={eventsLoading}
              composer={
                <SessionMessageComposer
                  awaitingAction={Boolean(activeAwaitingToolCall)}
                  disabled={conversationState.disabled}
                  live={conversationState.live}
                  onError={setMutationError}
                  onEventsChanged={() => setEventRefreshKey((value) => value + 1)}
                  onMessageSent={(sentEvents) => {
                    eventData.appendPrimaryEvents(sentEvents);
                    setSession((currentSession) =>
                      currentSession && currentSession.id === session.id
                        ? { ...currentSession, status: 'running' }
                        : currentSession,
                    );
                  }}
                  sessionId={session.id}
                  workspaceId={activeWorkspaceId}
                />
              }
              entries={entries}
              events={events}
              filteredEntries={filteredEntries}
              hasFilter={hasFilter}
              hoveredEventId={hoveredEventId}
              inspector={
                <SessionInspector
                  activeTab={inspectorTab}
                  activeLane={activeLane}
                  events={events}
                  eventsByLaneId={eventsByLaneId}
                  lanes={lanes}
                  onActiveTabChange={(tab) => {
                    setInspectorTab(tab);
                    if (tab === 'resources') refreshSessionResources();
                  }}
                  onAddFileResource={handleAddFileResource}
                  onClose={() => setInspectorOpen(false)}
                  onHoverEvent={setHoveredEventId}
                  onSelectEntry={(entryId) => {
                    setSelectedEntryId(entryId);
                  }}
                  onSelectLane={(laneId) => handleSelectLane(laneId, null)}
                  refreshKey={refreshKey}
                  selectedEntry={selectedInspectorEntry}
                  hoveredEventId={hoveredInspectorEventId}
                  session={session}
                  workspaceId={activeWorkspaceId}
                />
              }
              inspectorOpen={inspectorOpen}
              isMultiAgent={isMultiAgent}
              lanes={lanes}
              openModelRequest={openModelRequest}
              pendingAction={sessionPendingAction(
                activeAwaitingToolCall,
                handleToolConfirmation,
                conversationState.disabled,
              )}
              onClearFilters={() => {
                setQuery('');
                handleSelectLane(SESSION_MAIN_LANE_ID, null);
              }}
              onCopyAll={() =>
                void handleCopy(
                  fullTranscriptCopyPayload,
                  msg('managedAgents.sessions.detail.copiedFullTranscript', 'Full transcript copied'),
                )
              }
              onCloseInspector={() => setInspectorOpen(false)}
              onHoverEvent={setHoveredEventId}
              onOpenInspector={() => setInspectorOpen(true)}
              onQueryChange={setQuery}
              onSelectEntry={(entryId) => {
                setSelectedEntryId(entryId);
                if (entryId) {
                  setInspectorOpen(true);
                  setInspectorTab('events');
                }
              }}
              onSelectLane={handleSelectLane}
              onThreadClick={handleThreadClick}
              onTimelineSeek={handleTimelineSeek}
              onToggleArchivedLanes={(nextPressed) => setShowArchivedLanes(nextPressed)}
              query={query}
              scrollerRef={scrollerRef}
              selectedEntry={selectedEntry}
              selectedEntryId={selectedEntryId}
              showArchivedLanes={showArchivedLanes}
              suppressScrollSeekUntilRef={suppressScrollSeekUntilRef}
              threadNameById={threadNameById}
              timeline={timeline}
              timelineVisibleIds={timelineVisibleIds}
              traceStartMs={traceStartMs}
            />
          </SessionDetailDeltaFramesContext.Provider>
        </div>
      </section>
    </TooltipProvider>
  );
}

function SessionDetailAlerts({
  mutationError,
  warningError,
}: {
  mutationError: string | null;
  warningError?: string | null;
}) {
  return (
    <>
      {mutationError ? (
        <ManagedErrorAlert className="mx-4 mb-4 max-w-xl @min-[640px]:mx-6 @min-[1024px]:mx-8">
          {mutationError}
        </ManagedErrorAlert>
      ) : null}
      {warningError ? (
        <ManagedWarningAlert className="mx-4 mb-4 max-w-xl @min-[640px]:mx-6 @min-[1024px]:mx-8">
          {warningError}
        </ManagedWarningAlert>
      ) : null}
    </>
  );
}

function mergeSessionResources(currentSession: SessionApiResponse | null, updatedSession: SessionApiResponse) {
  return currentSession?.id === updatedSession.id
    ? { ...currentSession, resources: updatedSession.resources }
    : currentSession;
}

function sessionSummaryRefreshInterval(session: SessionApiResponse | null) {
  return session && sessionStatusIsLive(session.status) && !session.archived_at ? 1_000 : 60_000;
}

function sessionConversationState(session: SessionApiResponse) {
  const archived = Boolean(session.archived_at);
  const status = session.status.toLowerCase();
  return {
    disabled: archived || status === 'deleted' || status === 'terminated',
    live: !archived && sessionStatusIsLive(status),
  };
}

export function EventsTab({
  activeLane,
  archivedLaneCount,
  childLoading,
  composer,
  entries,
  events,
  filteredEntries,
  hasFilter,
  hoveredEventId,
  inspector,
  inspectorOpen,
  isMultiAgent,
  lanes,
  openModelRequest,
  pendingAction,
  onClearFilters,
  onCopyAll,
  onCloseInspector,
  onHoverEvent,
  onOpenInspector,
  onQueryChange,
  onSelectEntry,
  onSelectLane,
  onThreadClick,
  onTimelineSeek,
  onToggleArchivedLanes,
  query,
  scrollerRef,
  selectedEntry,
  selectedEntryId,
  showArchivedLanes,
  suppressScrollSeekUntilRef,
  threadNameById,
  timeline,
  timelineVisibleIds,
  traceStartMs,
}: EventsTabProps) {
  const { msg } = useI18n();
  const [minimapControlsSlot, setMinimapControlsSlot] = useState<HTMLSpanElement | null>(null);
  const inspectorOpenTriggerRef = useRef<HTMLButtonElement | null>(null);
  const previousInspectorOpenRef = useRef(inspectorOpen);
  const minimapLoading = childLoading && events.length === 0;
  const hasMinimapEvents = timeline.some((lane) => lane.items.length > 0);
  useLayoutEffect(() => {
    const wasOpen = previousInspectorOpenRef.current;
    previousInspectorOpenRef.current = inspectorOpen;
    if (wasOpen && !inspectorOpen) {
      const activeElement = document.activeElement;
      if (activeElement instanceof HTMLElement && activeElement.closest('[data-testid="session-inspector"]')) {
        inspectorOpenTriggerRef.current?.focus();
      }
    } else if (!wasOpen && inspectorOpen && document.activeElement === document.body) {
      document.querySelector<HTMLElement>('[data-inspector-close]')?.focus();
    }
  }, [inspectorOpen]);
  const handleTraceNavigation = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const eventTarget = event.target;
    if (
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      (eventTarget instanceof HTMLElement &&
        Boolean(eventTarget.closest('input, textarea, select, [contenteditable="true"]')))
    ) {
      return;
    }
    const target = sessionTraceKeyboardTarget(filteredEntries, selectedEntryId, event.key);
    if (!target) {
      return;
    }
    event.preventDefault();
    onSelectEntry(target.selectionId);
    window.requestAnimationFrame(() =>
      scrollSessionEntryIntoView(scrollerRef.current, sessionEventEntryRowId(target.entry)),
    );
  };
  return (
    <div
      className="flex h-full min-h-0 flex-col overflow-hidden bg-background [container-type:inline-size]"
      data-testid="events-tab"
    >
      <div
        className={`grid min-h-10 grid-cols-[minmax(0,1fr)_auto] items-center gap-2 py-1.5 ${SESSION_CHROME_GUTTER_CLASS_NAME}`}
        data-testid="session-trace-toolbar"
      >
        <SessionTraceSearch className="w-44 min-w-0 @min-[640px]:w-56" value={query} onChange={onQueryChange} />
        <div className="flex shrink-0 items-center gap-2">
          <span ref={setMinimapControlsSlot} className="flex items-center" />
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  aria-label={msg('managedAgents.sessions.detail.copyAll', 'Copy all')}
                  className="text-muted-foreground hover:text-foreground"
                  onClick={onCopyAll}
                />
              }
            >
              <Copy className="size-4" aria-hidden />
            </TooltipTrigger>
            <TooltipContent>{msg('managedAgents.sessions.detail.copyAll', 'Copy all')}</TooltipContent>
          </Tooltip>
          {!inspectorOpen ? (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    ref={inspectorOpenTriggerRef}
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={msg('managedAgents.sessions.inspector.label', 'Inspector')}
                    data-inspector-open=""
                    className="session-inspector-open-trigger text-muted-foreground hover:text-foreground"
                    onClick={onOpenInspector}
                  />
                }
              >
                <PanelRightOpen className="size-4" aria-hidden />
                <span className="session-inspector-open-label">
                  {msg('managedAgents.sessions.inspector.label', 'Inspector')}
                </span>
              </TooltipTrigger>
              <TooltipContent>{msg('managedAgents.sessions.inspector.label', 'Inspector')}</TooltipContent>
            </Tooltip>
          ) : null}
        </div>
      </div>

      <div className="relative" data-testid="session-minimap-rail">
        {minimapLoading ? (
          <EventsMinimapSkeleton />
        ) : hasMinimapEvents ? (
          <EventsMinimap
            lanes={timeline}
            activeLane={activeLane}
            selectedEntryId={selectedEntry?.id ?? selectedEntryId}
            hoveredEventId={resolveSelectedSessionEventEntry(entries, hoveredEventId)?.id ?? hoveredEventId}
            controlsSlot={minimapControlsSlot}
            visibleIds={timelineVisibleIds}
            scrollerRef={scrollerRef}
            suppressScrollSeekUntilRef={suppressScrollSeekUntilRef}
            onLaneChange={onSelectLane}
            onHoverEvent={onHoverEvent}
            onSeek={onTimelineSeek}
          />
        ) : null}
      </div>

      <div
        className="flex min-h-0 flex-1 flex-col overflow-hidden border-t border-border"
        data-testid="session-trace-shell"
      >
        <SessionTraceWorkspaceLayout
          inspectorOpen={inspectorOpen}
          onInspectorCollapse={onCloseInspector}
          resizeLabel={msg('managedAgents.sessions.trace.resizeInspector', 'Resize event inspector')}
          primary={
            <div className="flex h-full min-h-0 min-w-0 flex-col overflow-hidden" data-testid="session-transcript-pane">
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
              <MessageScrollerProvider key={activeLane} autoScroll defaultScrollPosition="end">
                <MessageScroller className="min-h-0 flex-1">
                  <MessageScrollerViewport
                    ref={scrollerRef}
                    data-testid="session-trace-list-pane"
                    tabIndex={0}
                    aria-label={msg('managedAgents.sessions.trace.eventList', 'Session events')}
                    className="scrollbar-none overflow-x-hidden px-4 py-3 focus-visible:outline-none"
                    onKeyDown={handleTraceNavigation}
                  >
                    <MessageScrollerContent
                      className="mx-auto w-full max-w-[720px] pb-8"
                      data-testid="session-trace-column"
                    >
                      {childLoading && !events.length ? (
                        <SessionTraceSkeleton />
                      ) : filteredEntries.length ? (
                        <SessionTranscriptView
                          entries={entries}
                          visibleEntries={filteredEntries}
                          openModelRequest={query.trim() ? null : openModelRequest}
                          selectedEntryId={selectedEntryId}
                          hoveredEventId={hoveredEventId}
                          onHoverEvent={onHoverEvent}
                          onSelectEntry={onSelectEntry}
                          threadNameById={threadNameById}
                          onThreadClick={onThreadClick}
                          traceStartMs={traceStartMs}
                        />
                      ) : (
                        <SessionTraceEmpty
                          message={
                            entries.length === 0
                              ? msg(
                                  'managedAgents.sessions.trace.noEvents',
                                  'No events yet. Events will appear here as they occur.',
                                )
                              : msg(
                                  'managedAgents.sessions.trace.noMatchingEvents',
                                  'No events match the current filters.',
                                )
                          }
                          onClear={hasFilter ? onClearFilters : undefined}
                        />
                      )}
                    </MessageScrollerContent>
                  </MessageScrollerViewport>
                  <MessageScrollerButton
                    aria-label={msg('managedAgents.sessions.trace.jumpToLatest', 'Jump to latest event')}
                    className="rounded-full shadow-sm"
                  >
                    <ArrowDown className="size-4" aria-hidden />
                    <span className="sr-only">
                      {msg('managedAgents.sessions.trace.jumpToLatest', 'Jump to latest event')}
                    </span>
                  </MessageScrollerButton>
                </MessageScroller>
              </MessageScrollerProvider>
              {pendingAction ? (
                <div className="flex-none px-4 pt-2" data-testid="session-pending-action">
                  <div className="mx-auto w-full max-w-[720px]">{pendingAction}</div>
                </div>
              ) : null}
              {composer}
            </div>
          }
          inspector={inspector}
        />
      </div>
    </div>
  );
}
