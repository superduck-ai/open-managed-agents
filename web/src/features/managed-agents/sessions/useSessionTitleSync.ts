import { useEffect, useEffectEvent, type Dispatch, type SetStateAction } from 'react';
import { retrieveManagedEntity } from '../api';
import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import { sessionEventKey, sessionEventType } from './sessionTraceModel';

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
    void retrieveManagedEntity('sessions', sessionId, workspaceId)
      .then((updated) => {
        if (!active || updated.type !== 'session') return;
        setSession((current) =>
          current?.id === sessionId && current.title === title && current.title !== updated.title
            ? { ...current, title: updated.title }
            : current,
        );
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [sessionId, workspaceId, eventKey, setSession]);
}
