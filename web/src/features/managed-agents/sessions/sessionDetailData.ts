import {
  cleanupIncompleteSessionStreamEvents,
  mergeSessionStreamFrame,
  reconcileIncompleteSessionStreamEvents,
  sessionDetailEventCacheKey,
  sessionDetailDeltaFrames,
  sessionDetailScopeEvents,
  sessionIncompleteStreamEventIds,
  sessionStreamBackoff,
  sessionStreamShouldStop,
  sessionThreadShouldFetchEvents,
  sleepWithAbort,
  streamSessionEvents,
  syncSessionEventHistory,
} from '../api';
import { type QuickstartSessionEvent, type SessionDetailDeltaFrames, type SessionThreadApiResponse } from '../types';
import { errorMessage } from '../utils';
import { sessionEventType } from './sessionTraceModel';
import { type QueryClient, skipToken, useQueries, useQueryClient } from '@tanstack/react-query';
import { createContext, useCallback, useEffect, useMemo, useRef, useState } from 'react';

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
  const [connectedScope, setConnectedScope] = useState<string | null>(null);
  const previousRefreshKeyRef = useRef(refreshKey);
  const childThreadIds = useMemo(
    () =>
      threads
        .filter((thread) => sessionThreadShouldFetchEvents(thread, includeArchivedThreads))
        .map((thread) => thread.id),
    [includeArchivedThreads, threads],
  );
  const scopeThreadIds = useMemo(() => ['', ...childThreadIds], [childThreadIds]);
  const scopeKey = scopeThreadIds.join('\0');
  // Keep manually populated event caches observed while this page is open.
  useQueries({
    queries: sessionId
      ? scopeThreadIds.map((threadId) => ({
          queryKey: sessionDetailEventCacheKey(workspaceId, sessionId, threadId),
          queryFn: skipToken,
        }))
      : [],
  });
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
    if (!sessionId || live) {
      return;
    }
    const controller = new AbortController();
    let active = true;
    const fromStart = refreshKey !== previousRefreshKeyRef.current;
    previousRefreshKeyRef.current = refreshKey;
    const syncScope = async (threadId = '') => {
      await syncSessionEventHistory({
        queryClient,
        sessionId,
        workspaceId,
        threadId,
        signal: controller.signal,
        mode: fromStart ? 'reset' : 'resume',
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
  }, [bump, childThreadIds, live, queryClient, refreshKey, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId || live) {
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
            mode: 'refresh',
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
  }, [bump, live, queryClient, scopeThreadIds, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId || !live) {
      return;
    }
    let active = true;
    const controller = new AbortController();
    setLoading(true);
    setError(null);
    void runSessionEventStreamLoop({
      queryClient,
      sessionId,
      workspaceId,
      threadId: '',
      signal: controller.signal,
      onCacheChange: bump,
      onHistorySynced: () => {
        bump();
        setLoading(false);
      },
      onPrimaryEvent,
      onConnectionChange: (connected) => {
        if (active) setConnectedScope(connected ? `${workspaceId}:${sessionId}` : null);
      },
    })
      .catch((streamError: unknown) => {
        if (!controller.signal.aborted) setError(errorMessage(streamError));
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => {
      active = false;
      setConnectedScope(null);
      controller.abort();
      cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, '');
      bump();
    };
  }, [bump, live, onPrimaryEvent, queryClient, refreshKey, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId || !live || !childThreadIds.length) {
      return;
    }
    const controller = new AbortController();
    const pending = new Set(childThreadIds);
    setChildLoading(true);
    for (const threadId of childThreadIds) {
      void runSessionEventStreamLoop({
        queryClient,
        sessionId,
        workspaceId,
        threadId,
        signal: controller.signal,
        onCacheChange: bump,
        onHistorySynced: () => {
          pending.delete(threadId);
          if (!pending.size) setChildLoading(false);
          bump();
        },
      })
        .catch((streamError: unknown) => {
          if (!controller.signal.aborted) setError(errorMessage(streamError));
        })
        .finally(() => {
          if (controller.signal.aborted) return;
          pending.delete(threadId);
          if (!pending.size) setChildLoading(false);
        });
    }
    return () => {
      controller.abort();
      childThreadIds.forEach((threadId) =>
        cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, threadId),
      );
    };
  }, [bump, childThreadIds, live, queryClient, refreshKey, sessionId, workspaceId]);

  const events = useMemo(
    () => (sessionId ? sessionDetailScopeEvents(queryClient, workspaceId, sessionId, scopeThreadIds) : []),
    [queryClient, scopeKey, sessionId, version, workspaceId],
  );
  const deltaFrames = useMemo(
    () => (sessionId ? sessionDetailDeltaFrames(queryClient, workspaceId, sessionId, scopeThreadIds) : {}),
    [queryClient, scopeKey, sessionId, version, workspaceId],
  );

  return {
    events,
    deltaFrames,
    loading,
    childLoading,
    error,
    appendPrimaryEvents,
    connected: connectedScope === `${workspaceId}:${sessionId}`,
  };
}

export async function runSessionEventStreamLoop({
  queryClient,
  sessionId,
  workspaceId,
  threadId,
  signal,
  onCacheChange,
  onHistorySynced,
  onPrimaryEvent,
  onConnectionChange,
}: {
  queryClient: QueryClient;
  sessionId: string;
  workspaceId: string;
  threadId: string;
  signal: AbortSignal;
  onCacheChange: () => void;
  onHistorySynced?: () => void;
  onPrimaryEvent?: (event: QuickstartSessionEvent) => void;
  onConnectionChange?: (connected: boolean) => void;
}) {
  let consecutiveFailures = 0;
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
    const attempt = new AbortController();
    const abortAttempt = () => attempt.abort(signal.reason);
    signal.addEventListener('abort', abortAttempt, { once: true });
    let historyScan: Promise<void> | undefined;
    let historyError: unknown;
    try {
      if (consecutiveFailures >= 3) {
        await syncSessionEventHistory({ queryClient, sessionId, workspaceId, threadId, signal, mode: 'refresh' });
        onCacheChange();
        onHistorySynced?.();
        consecutiveFailures = 0;
      }
      await streamSessionEvents({
        sessionId,
        threadId: threadId || undefined,
        workspaceId,
        signal: attempt.signal,
        onOpen: () => {
          consecutiveFailures = 0;
          backoff = 0;
          historyScan = syncSessionEventHistory({
            queryClient,
            sessionId,
            workspaceId,
            threadId,
            signal: attempt.signal,
            mode: 'refresh',
          })
            .then(() => {
              if (!attempt.signal.aborted) {
                onCacheChange();
                onHistorySynced?.();
                onConnectionChange?.(true);
              }
            })
            .catch((error: unknown) => {
              historyError = error;
              attempt.abort(error);
            });
        },
        onEvent: (event) => {
          mergeSessionStreamFrame(queryClient, workspaceId, sessionId, threadId, event);
          const incompletePreviewIds = sessionIncompleteStreamEventIds(queryClient, workspaceId, sessionId, threadId);
          if (sessionEventType(event).endsWith('status_idle') && incompletePreviewIds.size) {
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
      // Let a completed scan settle, but never hold a closed stream open for a stalled page.
      await Promise.race([historyScan, new Promise<void>((resolve) => window.setTimeout(resolve, 0))]);
      if (historyError) throw historyError;
      throw new Error('Session event stream ended');
    } catch (streamError) {
      attempt.abort(streamError);
      await historyScan;
      cancelIdleReconciliation();
      cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, threadId);
      onCacheChange();
      if (signal.aborted) return;
      if (sessionStreamShouldStop(streamError)) throw streamError;
      consecutiveFailures += 1;
      backoff = sessionStreamBackoff(streamError, backoff);
      await sleepWithAbort(Math.max(1000, backoff), signal).catch(() => undefined);
    } finally {
      onConnectionChange?.(false);
      signal.removeEventListener('abort', abortAttempt);
    }
  }
}
