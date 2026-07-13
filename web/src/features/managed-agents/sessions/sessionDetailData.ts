import {
  cleanupIncompleteSessionStreamEvents,
  mergeSessionStreamFrame,
  SESSION_DETAIL_CHILD_REFETCH_INTERVAL_MS,
  SESSION_DETAIL_STREAM_FALLBACK_LIMIT,
  sessionDetailDeltaFrames,
  sessionDetailScopeEvents,
  sessionEventHistoryShouldSkipStream,
  sessionPrimaryHistoryShouldSkipStream,
  sessionStreamBackoff,
  sessionStreamShouldStop,
  sessionThreadShouldFetchEvents,
  sleepWithAbort,
  streamSessionEvents,
  syncSessionEventHistory,
} from "../api";
import { type QuickstartSessionEvent, type SessionDetailDeltaFrames, type SessionThreadApiResponse } from "../types";
import { errorMessage } from "../utils";
import { type QueryClient, useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useEffect, useMemo, useRef, useState } from "react";

export const SessionDetailDeltaFramesContext = createContext<SessionDetailDeltaFrames>({});

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
  const previousRefreshKeyRef = useRef(refreshKey);
  const childThreadIds = useMemo(
    () =>
      threads
        .filter((thread) => sessionThreadShouldFetchEvents(thread, includeArchivedThreads))
        .map((thread) => thread.id),
    [includeArchivedThreads, threads],
  );
  const scopeThreadIds = useMemo(() => ["", ...childThreadIds], [childThreadIds]);
  const scopeKey = scopeThreadIds.join("\0");
  const bump = useCallback(() => setVersion((value) => value + 1), []);

  useEffect(() => {
    if (!sessionId) {
      return;
    }
    const controller = new AbortController();
    let active = true;
    const fromStart = refreshKey !== previousRefreshKeyRef.current;
    previousRefreshKeyRef.current = refreshKey;
    const syncScope = async (threadId = "") => {
      await syncSessionEventHistory({
        queryClient,
        sessionId,
        workspaceId,
        threadId,
        signal: controller.signal,
        fromStart,
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
        await syncScope("");
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
      if (document.visibilityState && document.visibilityState !== "visible") {
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
            force: true,
          }),
        ),
      )
        .then(bump)
        .catch(() => undefined);
    };
    window.addEventListener("online", handleRefetch);
    document.addEventListener("visibilitychange", handleRefetch);
    return () => {
      window.removeEventListener("online", handleRefetch);
      document.removeEventListener("visibilitychange", handleRefetch);
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
      threadId: "",
      signal: controller.signal,
      onCacheChange: bump,
      onPrimaryEvent,
    }).catch(() => undefined);
    return () => {
      controller.abort();
      cleanupIncompleteSessionStreamEvents(queryClient, workspaceId, sessionId, "");
      bump();
    };
  }, [bump, live, onPrimaryEvent, queryClient, sessionId, workspaceId]);

  useEffect(() => {
    if (!sessionId || !live || !childThreadIds.length) {
      return;
    }
    const controller = new AbortController();
    const syncChildren = () => {
      if (controller.signal.aborted || (document.visibilityState && document.visibilityState !== "visible")) {
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
            force: true,
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

  return { events, deltaFrames, loading, childLoading, error };
}

export async function runSessionEventStreamLoop({
  queryClient,
  sessionId,
  workspaceId,
  threadId,
  signal,
  onCacheChange,
  onPrimaryEvent,
}: {
  queryClient: QueryClient;
  sessionId: string;
  workspaceId: string;
  threadId: string;
  signal: AbortSignal;
  onCacheChange: () => void;
  onPrimaryEvent?: (event: QuickstartSessionEvent) => void;
}) {
  let consecutiveFailures = 0;
  let everConnected = false;
  let fallbackCount = 0;
  let backoff = 0;
  let historySynced = false;
  while (!signal.aborted) {
    const isFallback =
      !everConnected && fallbackCount < SESSION_DETAIL_STREAM_FALLBACK_LIMIT && consecutiveFailures >= 3;
    if (isFallback) {
      fallbackCount += 1;
      await sleepWithAbort(Math.max(3000, backoff), signal);
      await syncSessionEventHistory({ queryClient, sessionId, workspaceId, threadId, signal, force: true });
      onCacheChange();
      consecutiveFailures = 0;
    }
    try {
      if (!historySynced) {
        const historyCache = await syncSessionEventHistory({ queryClient, sessionId, workspaceId, threadId, signal });
        onCacheChange();
        historySynced = true;
        if (
          sessionEventHistoryShouldSkipStream(historyCache.events, threadId) ||
          (threadId && sessionPrimaryHistoryShouldSkipStream(queryClient, workspaceId, sessionId))
        ) {
          return;
        }
      }
      await streamSessionEvents({
        sessionId,
        threadId: threadId || undefined,
        workspaceId,
        signal,
        onOpen: () => {
          everConnected = true;
          consecutiveFailures = 0;
          backoff = 0;
        },
        onEvent: (event) => {
          mergeSessionStreamFrame(queryClient, workspaceId, sessionId, threadId, event);
          if (!threadId) {
            onPrimaryEvent?.(event);
          }
          onCacheChange();
        },
      });
      everConnected = true;
      consecutiveFailures = 0;
      backoff = 0;
      return;
    } catch (streamError) {
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
