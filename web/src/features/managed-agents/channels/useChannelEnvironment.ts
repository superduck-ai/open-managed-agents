import { useQuery } from '@tanstack/react-query';
import { channelApi } from './api';
import { anthropicBetaApi } from '../../../shared/api/anthropic';
import type { EnvironmentApiResponse } from '../types';

export interface EnvironmentSettings {
  environment_id: string | null;
}

export function useChannelEnvironment(agentId: string, workspaceId: string) {
  const queryKey = ['channel-environment', workspaceId, agentId];
  const settings = useQuery({
    queryKey,
    queryFn: () => channelApi<EnvironmentSettings>(agentId, workspaceId, '/settings'),
    retry: false,
  });
  const environments = useQuery({
    queryKey: ['channel-environment-options', workspaceId],
    queryFn: async () => {
      const result: EnvironmentApiResponse[] = [];
      let cursor: string | null | undefined;
      do {
        const page = await anthropicBetaApi.environments.list<EnvironmentApiResponse>(
          { limit: 100, include_archived: false, ...(cursor ? { page: cursor } : {}) },
          workspaceId,
        );
        result.push(...page.data);
        cursor = page.next_page;
      } while (cursor);
      return result;
    },
    retry: false,
  });
  return {
    queryKey,
    environments: environments.data ?? [],
    selectedEnvironment: environments.data?.find((environment) => environment.id === settings.data?.environment_id),
    isPending: settings.isPending || environments.isPending,
    error: settings.error || environments.error,
  };
}
