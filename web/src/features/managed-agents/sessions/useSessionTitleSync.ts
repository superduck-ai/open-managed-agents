import { useEffect, useEffectEvent, type Dispatch, type SetStateAction } from 'react';
import { retrieveManagedEntity } from '../api';
import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import { sessionEventKey, sessionEventType } from './sessionTraceModel';

const TITLE_READ_RETRY_MS = [250, 1000];

export function useSessionTitleSync(
  session: SessionApiResponse | null,
  events: QuickstartSessionEvent[],
  workspaceId: string,
  setSession: Dispatch<SetStateAction<SessionApiResponse | null>>,
  routeSessionId = session?.id,
) {
  const sessionId = session?.id === routeSessionId ? session?.id : undefined;
  const latestUpdate = events
    .slice()
    .reverse()
    .find(
      (event) =>
        sessionEventType(event) === 'session.updated' && (event.title === null || typeof event.title === 'string'),
    );
  const eventKey = latestUpdate ? sessionEventKey(latestUpdate) : '';
  const readTitle = useEffectEvent(() => session?.title);
  useEffect(() => {
    if (!sessionId || !eventKey) return;
    const title = readTitle();
    let active = true;
    let retry: number | undefined;
    const refresh = (attempt: number) => {
      void retrieveManagedEntity('sessions', sessionId, workspaceId)
        .then((updated) => {
          if (!active || updated.type !== 'session') return;
          setSession((current) =>
            current?.id === sessionId && current.title === title && current.title !== updated.title
              ? { ...current, title: updated.title }
              : current,
          );
        })
        .catch((error: unknown) => {
          if (error && typeof error === 'object' && 'status' in error && (error.status === 401 || error.status === 403))
            return;
          const delay = TITLE_READ_RETRY_MS[attempt];
          if (active && delay !== undefined) retry = window.setTimeout(() => refresh(attempt + 1), delay);
        });
    };
    refresh(0);
    return () => {
      active = false;
      window.clearTimeout(retry);
    };
  }, [sessionId, workspaceId, eventKey, setSession]);
}
