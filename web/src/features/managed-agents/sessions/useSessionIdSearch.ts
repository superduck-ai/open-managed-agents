import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';
import { retrieveSessionDetailSession } from '../api';
import { errorMessage } from '../utils';

export function useSessionIdSearch(workspaceId: string, value: string, enabled: boolean, onPage: boolean) {
  const [debouncedId, setDebouncedId] = useState('');
  const id = value.trim();
  const handled = enabled && /^sesn_[a-zA-Z0-9_]+$/.test(id) && !onPage;
  useEffect(() => {
    const timer = setTimeout(() => setDebouncedId(id), 300);
    return () => clearTimeout(timer);
  }, [id]);
  const query = useQuery({
    queryKey: ['session-id-search', workspaceId, debouncedId],
    queryFn: () => retrieveSessionDetailSession(debouncedId, workspaceId),
    enabled: handled && debouncedId === id,
    retry: false,
  });
  return {
    handled,
    session: handled && id === debouncedId ? query.data : undefined,
    loading: handled && (id !== debouncedId || query.isPending),
    error: handled && id === debouncedId && query.isError ? errorMessage(query.error) : null,
    refresh: () => void query.refetch(),
  };
}
