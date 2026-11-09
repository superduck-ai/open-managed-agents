import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { listTrajectories, type QueryScope } from './api';

export function useSessionOptions(scope: QueryScope) {
  const query = useQuery({
    queryKey: ['observability-session-options', scope],
    queryFn: () => listTrajectories(scope, 0, 'recent'),
    enabled: !!scope.orgUuid && !!scope.workspaceId && !!scope.startTime,
    placeholderData: keepPreviousData,
    retry: false,
  });
  const options = (query.data?.items ?? []).map((item) => ({
    value: item.session_id,
    label: item.title || item.session_id,
    description: item.session_id,
  }));
  return { options, loading: query.isPending };
}
