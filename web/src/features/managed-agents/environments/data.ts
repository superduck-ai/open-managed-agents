import { useQuery, useQueryClient } from '@tanstack/react-query';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { getConsoleRequestContext } from '../../../shared/api/client';
import type { EnvironmentApiResponse, PageCursor } from '../types';

export function environmentQueryKey(workspaceId: string) {
  return ['environments', getConsoleRequestContext().organizationUuid, workspaceId] as const;
}

export function useEnvironment(workspaceId: string, id?: string) {
  return useQuery({
    queryKey: [...environmentQueryKey(workspaceId), 'detail', id],
    queryFn: () => anthropicBetaApi.environments.retrieve<EnvironmentApiResponse>(id!, workspaceId),
    enabled: Boolean(id),
    retry: false,
  });
}

export function useEnvironments(workspaceId: string, page: PageCursor, filtered = false) {
  return useQuery({
    queryKey: [...environmentQueryKey(workspaceId), 'list', filtered ? 'filtered' : page],
    queryFn: async () => {
      let cursor = filtered ? null : page;
      const data: EnvironmentApiResponse[] = [];
      do {
        const result = await anthropicBetaApi.environments.list<EnvironmentApiResponse>(
          { limit: 50, include_archived: true, ...(cursor ? { page: cursor } : {}) },
          workspaceId,
        );
        data.push(...result.data);
        cursor = result.next_page ?? null;
        if (!filtered) return result;
      } while (cursor);
      return { data, has_more: false, next_page: null };
    },
    retry: false,
  });
}

export function useEnvironmentRefresh(workspaceId: string) {
  const client = useQueryClient();
  return (entity?: EnvironmentApiResponse) => {
    if (entity) client.setQueryData([...environmentQueryKey(workspaceId), 'detail', entity.id], entity);
    void client.invalidateQueries({ queryKey: environmentQueryKey(workspaceId) });
  };
}
