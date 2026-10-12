import { useQuery } from '@tanstack/react-query';
import { useCallback, useEffect } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { retrieveSessionDetailSession } from '../api';
import type { SessionApiResponse } from '../types';
import { errorMessage } from '../utils';
import { sessionStatusIsLive } from './sessionDetailModel';

export function useSessionResourceRefresh(
  session: SessionApiResponse | null,
  workspaceId: string,
  resourcesVisible: boolean,
  setSession: Dispatch<SetStateAction<SessionApiResponse | null>>,
) {
  const running = Boolean(session && sessionStatusIsLive(session.status) && !session.archived_at);
  const sessionId = session?.id;
  const { data, error, refetch } = useQuery({
    queryKey: ['session-resources', workspaceId, sessionId],
    queryFn: () => retrieveSessionDetailSession(sessionId!, workspaceId),
    enabled: Boolean(sessionId),
    refetchInterval: running ? 3000 : resourcesVisible ? 10000 : false,
    refetchOnWindowFocus: true,
    retry: false,
  });
  useEffect(() => {
    if (data) setSession((current) => (current?.id === data.id ? { ...current, resources: data.resources } : current));
  }, [data, setSession, workspaceId]);
  useEffect(() => {
    if (sessionId) void refetch();
  }, [refetch, resourcesVisible, session?.status, sessionId]);
  const refresh = useCallback(async () => {
    await refetch();
  }, [refetch]);
  return {
    refresh,
    error: error ? errorMessage(error) : null,
  };
}
