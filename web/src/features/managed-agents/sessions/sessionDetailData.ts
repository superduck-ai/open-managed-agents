import {
  cleanupIncompleteSessionStreamEvents,
  mergeSessionStreamFrame,
  reconcileIncompleteSessionStreamEvents,
  SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS,
  SESSION_DETAIL_STREAM_FALLBACK_LIMIT,
  sessionDetailDeltaFrames,
  sessionDetailScopeEvents,
  sessionEventHistoryShouldSkipStream,
  sessionIncompleteStreamEventIds,
  sessionPrimaryHistoryShouldSkipStream,
  sessionStreamBackoff,
  sessionStreamShouldStop,
  sessionThreadShouldFetchEvents,
  sessionThreadIsChild,
  sleepWithAbort,
  streamSessionEvents,
  syncSessionEventHistory,
} from '../api';
import { type QuickstartSessionEvent, type SessionDetailDeltaFrames, type SessionThreadApiResponse } from '../types';
import { errorMessage } from '../utils';
import { sessionEventType } from './sessionTraceModel';
import { type QueryClient, useQueryClient } from '@tanstack/react-query';
import { createContext, useCallback, useEffect, useMemo, useState } from 'react';

export const SessionDetailDeltaFramesContext = createContext<SessionDetailDeltaFrames>({});

const SESSION_IDLE_RECONCILIATION_GRACE_MS = 1500;

export function useSessionDetailEventData({
  sessionId,
  workspaceId,
  threads,
  includeArchivedThreads,
  live,
  onPrimaryEvent,
  refreshKey,
}: {
  sessionId: string | null;
  workspaceId: string;
  threads: SessionThreadApiResponse[];
  includeArchivedThreads: boolean;
  live: boolean;
  onPrimaryEvent?: (event: QuickstartSessionEvent) => void;
  refreshKey: number;
}) {
  const queryClient = useQueryClient();
  const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(false);
  const [childLoading, setChildLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const primaryThreadId = threads.find((thread) => !sessionThreadIsChild(thread))?.id ?? '';
  const childThreadIds = useMemo(
    () =>
      threads
        .filter((thread) => sessionThreadShouldFetchEvents(thread, includeArchivedThreads))
        .map((thread) => thread.id),
    [includeArchivedThreads, threads],
  );
  const scopeThreadIds = useMemo(() => ['', ...childThreadIds], [childThreadIds]);
  const scopeKey = scopeThreadIds.join('\0');
  const bump = useCallback(() => setVersion((value) => value + 1), []);
  const appendPrimaryEvents = useCallback(
    (events: QuickstartSessionEvent[]) => {
      if (!sessionId) {
        return;
      }
      events.forEach((event) => mergeSessionStreamFrame(queryClient, workspaceId, sessionId, '', event));
      bump();
    },
    [bump, queryClient, sessionId, workspaceId],
  );

  useEffect(() => {
    if (!sessionId) {
      return;
    }
    const controller = new AbortController();
    let active = true;
    const syncScope = async (threadId = '') => {
      await syncSessionEventHistory({
        queryClient,
        sessionId,
        workspaceId,
        threadId,
        signal: controller.signal,
      });
      if (active) {
        bump();
      }
    };

    setLoading(true);
    setChildLoading(childThreadIds.length > 0);
    setError(null);
    void (async () => {
      try {
        await syncScope('');
      } catch (syncError) {
        if (!controller.signal.aborted && active) {
          setError(errorMessage(syncError));
        }
      } finally {
        if (active) {
          setLoading(false);
        }
      }

      try {
        await Promise.all(childThreadIds.map((threadId) => syncScope(threadId)));
      } catch (syncError) {
        if (!controller.signal.aborted && active) {
          setError(errorMessage(syncError));
        }
      } finally {
        if (active) {
          setChildLoading(false);
        }
      }
    })();

    return () => {
      active = false;
      controller.abort();
    };
  }, [bump, childThreadIds, queryClient, refreshKey, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId) {
      return;
    }
    const handleRefetch = () => {
      if (document.visibilityState && document.visibilityState !== 'visible') {
        return;
      }
      const controller = new AbortController();
      void Promise.all(
        scopeThreadIds.map((threadId) =>
          syncSessionEventHistory({
            queryClient,
            sessionId,
            workspaceId,
            threadId,
            signal: controller.signal,
          }),
        ),
      )
        .then(bump)
        .catch(() => undefined);
    };
    window.addEventListener('online', handleRefetch);
    document.addEventListener('visibilitychange', handleRefetch);
    return () => {
      window.removeEventListener('online', handleRefetch);
      document.removeEventListener('visibilitychange', handleRefetch);
    };
  }, [bump, queryClient, scopeThreadIds, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId || !live) {
      return;
    }
    const controller = new AbortController();
    void runSessionEventStreamLoop({
      queryClient,
      sessionId,
      workspaceId,
      threadId: '',
      primaryThreadId,
      signal: controller.signal,
      onCacheChange: bump,
      onPrimaryEvent,
    }).catch(() => undefined);
    return () => {
      controller.abort();
      cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, '');
      bump();
    };
  }, [bump, live, onPrimaryEvent, primaryThreadId, queryClient, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId || !live || !childThreadIds.length) {
      return;
    }
    const controller = new AbortController();
    const syncChildren = () => {
      if (controller.signal.aborted || (document.visibilityState && document.visibilityState !== 'visible')) {
        return;
      }
      void Promise.all(
        childThreadIds.map((threadId) =>
          syncSessionEventHistory({
            queryClient,
            sessionId,
            workspaceId,
            threadId,
            signal: controller.signal,
          }),
        ),
      )
        .then(bump)
        .catch(() => undefined);
    };
    const interval = window.setInterval(syncChildren, SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS);
    return () => {
      window.clearInterval(interval);
      controller.abort();
    };
  }, [bump, childThreadIds, live, queryClient, sessionId, workspaceId]);

  const events = useMemo(
    () => (sessionId ? sessionDetailScopeEvents(queryClient, workspaceId, sessionId, scopeThreadIds) : []),
    [queryClient, scopeKey, sessionId, version, workspaceId],
  );
  const deltaFrames = useMemo(
    () => (sessionId ? sessionDetailDeltaFrames(queryClient, workspaceId, sessionId, scopeThreadIds) : {}),
    [queryClient, scopeKey, sessionId, version, workspaceId],
  );

  return { events, deltaFrames, loading, childLoading, error, appendPrimaryEvents };
}

export async function runSessionEventStreamLoop({
  queryClient,
  sessionId,
  workspaceId,
  threadId,
  primaryThreadId = '',
  signal,
  onCacheChange,
  onPrimaryEvent,
}: {
  queryClient: QueryClient;
  sessionId: string;
  workspaceId: string;
  threadId: string;
  primaryThreadId?: string;
  signal: AbortSignal;
  onCacheChange: () => void;
  onPrimaryEvent?: (event: QuickstartSessionEvent) => void;
}) {
  let consecutiveFailures = 0;
  let everConnected = false;
  let fallbackCount = 0;
  let backoff = 0;
  let idleReconciliationTimer: number | null = null;
  const cancelIdleReconciliation = () => {
    if (idleReconciliationTimer !== null) {
      window.clearTimeout(idleReconciliationTimer);
      idleReconciliationTimer = null;
    }
    signal.removeEventListener('abort', cancelIdleReconciliation);
  };
  const scheduleIdleReconciliation = (eventIds: ReadonlySet<string>) => {
    cancelIdleReconciliation();
    signal.addEventListener('abort', cancelIdleReconciliation, { once: true });
    idleReconciliationTimer = window.setTimeout(() => {
      idleReconciliationTimer = null;
      signal.removeEventListener('abort', cancelIdleReconciliation);
      void reconcileIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, threadId, signal, eventIds)
        .catch(() => undefined)
        .finally(() => {
          if (!signal.aborted) onCacheChange();
        });
    }, SESSION_IDLE_RECONCILIATION_GRACE_MS);
  };
  while (!signal.aborted) {
    const isFallback =
      !everConnected && fallbackCount < SESSION_DETAIL_STREAM_FALLBACK_LIMIT && consecutiveFailures >= 3;
    if (isFallback) {
      fallbackCount += 1;
      await sleepWithAbort(Math.max(3000, backoff), signal);
      await syncSessionEventHistory({ queryClient, sessionId, workspaceId, threadId, signal });
      onCacheChange();
      consecutiveFailures = 0;
    }
    try {
      await streamSessionEvents({
        sessionId,
        threadId: threadId || undefined,
        workspaceId,
        signal,
        onOpen: async () => {
          // The live-only stream is already established. Its response buffers
          // events while the fixed history snapshot is fetched and merged.
          await syncSessionEventHistory({ queryClient, sessionId, workspaceId, threadId, signal });
          onCacheChange();
          everConnected = true;
          consecutiveFailures = 0;
          backoff = 0;
        },
        onEvent: (event) => {
          mergeSessionStreamFrame(queryClient, workspaceId, sessionId, threadId, event, primaryThreadId);
          const incompletePreviewIds = sessionIncompleteStreamEventIds(queryClient, workspaceId, sessionId, threadId);
          const type = sessionEventType(event);
          const scopeIdle =
            type === 'session.status_idle' ||
            (type === 'session.thread_status_idle' &&
              event.session_thread_id === (threadId || primaryThreadId) &&
              Boolean(event.session_thread_id));
          if (scopeIdle && incompletePreviewIds.size) {
            scheduleIdleReconciliation(incompletePreviewIds);
          } else if (!incompletePreviewIds.size) {
            cancelIdleReconciliation();
          }
          if (!threadId) {
            onPrimaryEvent?.(event);
          }
          onCacheChange();
        },
      });
      everConnected = true;
      consecutiveFailures = 0;
      backoff = 0;
      const disconnectedPreviewIds = sessionIncompleteStreamEventIds(queryClient, workspaceId, sessionId, threadId);
      const historyCache = await syncSessionEventHistory({
        queryClient,
        sessionId,
        workspaceId,
        threadId,
        signal,
      });
      cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, threadId, disconnectedPreviewIds);
      onCacheChange();
      if (
        sessionEventHistoryShouldSkipStream(historyCache.events, threadId) ||
        (threadId && sessionPrimaryHistoryShouldSkipStream(queryClient, workspaceId, sessionId))
      )
        return;
      await sleepWithAbort(1000, signal);
    } catch (streamError) {
      cancelIdleReconciliation();
      cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, threadId);
      onCacheChange();
      if (signal.aborted || sessionStreamShouldStop(streamError)) {
        return;
      }
      consecutiveFailures += 1;
      backoff = sessionStreamBackoff(streamError, backoff);
      await sleepWithAbort(Math.max(1000, backoff), signal).catch(() => undefined);
    }
    if (fallbackCount >= SESSION_DETAIL_STREAM_FALLBACK_LIMIT) {
      fallbackCount = 0;
    }
  }
}
