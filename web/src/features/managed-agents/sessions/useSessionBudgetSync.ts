import { useEffect, type Dispatch, type SetStateAction } from 'react';
import { retrieveManagedEntity } from '../api';
import { type QuickstartSessionEvent, type SessionApiResponse } from '../types';
import { isBudgetReachedError, sessionBudgetState } from '../resources/budget';

export function useSessionBudgetSync(
  session: SessionApiResponse | null,
  events: QuickstartSessionEvent[],
  workspaceId: string,
  mutationError: string | null,
  setSession: Dispatch<SetStateAction<SessionApiResponse | null>>,
) {
  const sessionId = session?.id;
  const budgetKey = JSON.stringify(session?.budget);
  const latest = events
    .slice()
    .reverse()
    .find((event) =>
      ['session.usage', 'session.status_idle', 'session.updated', 'span.model_request_end'].includes(
        String(event.type),
      ),
    );
  const eventKey = latest ? JSON.stringify(latest) : '';
  const rejected = isBudgetReachedError(mutationError);
  useEffect(() => {
    if (!sessionId || !budgetKey || budgetKey === 'null' || (!eventKey && !rejected)) return;
    let active = true;
    void (retrieveManagedEntity('sessions', sessionId, workspaceId) as Promise<SessionApiResponse>)
      .then((updated) => {
        if (!active) return;
        setSession((current) =>
          current?.id === sessionId ? { ...current, usage: updated.usage, budget: updated.budget } : current,
        );
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, [sessionId, workspaceId, budgetKey, eventKey, rejected, setSession]);
  const budget = session ? sessionBudgetState(session) : null;
  return { budget, budgetReached: Boolean(budget?.reached) };
}
