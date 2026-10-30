import { useQuery } from '@tanstack/react-query';
import { channelApi } from './api';

export interface EnvironmentSettings {
  environment_id: string | null;
}

export function useChannelEnvironment(agentId: string, workspaceId: string) {
  return useQuery({
    queryKey: ['channel-environment', workspaceId, agentId],
    queryFn: () => channelApi<EnvironmentSettings>(agentId, workspaceId, '/settings'),
    retry: false,
  });
}
