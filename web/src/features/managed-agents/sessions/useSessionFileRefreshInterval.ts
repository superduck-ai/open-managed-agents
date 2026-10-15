import { useEffect, useRef, useState } from 'react';
import type { SessionApiResponse } from '../types';
import { sessionStatusIsLive } from './sessionDetailModel';

const FILE_SETTLE_WINDOW_MS = 30_000;

export function useSessionFileRefreshInterval(session: SessionApiResponse | null, workspaceId: string) {
  const scope = `${workspaceId}:${session?.id ?? ''}`;
  const running = Boolean(session && sessionStatusIsLive(session.status) && !session.archived_at);
  const archived = Boolean(session?.archived_at);
  const sessionId = session?.id;
  const updatedAt = session?.updated_at;
  const previousRef = useRef<{ scope: string; running: boolean } | null>(null);
  const [settlingScope, setSettlingScope] = useState<string | null>(null);
  useEffect(() => {
    const previous = previousRef.current;
    previousRef.current = { scope, running };
    setSettlingScope(null);
    if (!sessionId || archived || running) return;
    const age = Date.now() - Date.parse(updatedAt ?? '');
    const justFinished = previous?.scope === scope && previous.running;
    if (!justFinished && !(age >= 0 && age < FILE_SETTLE_WINDOW_MS)) return;
    setSettlingScope(scope);
    const timer = window.setTimeout(() => setSettlingScope(null), FILE_SETTLE_WINDOW_MS);
    return () => window.clearTimeout(timer);
  }, [scope, sessionId, running, archived, updatedAt]);
  if (!sessionId || archived) return false;
  return running ? 3000 : settlingScope === scope ? 1000 : false;
}
