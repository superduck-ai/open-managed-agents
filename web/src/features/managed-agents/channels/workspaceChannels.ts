import { listAgents } from '../api';
import type { AgentApiResponse, PageCursor } from '../types';
import { channelApi, type Channel } from './api';
import type { EnvironmentSettings } from './useChannelEnvironment';

export type WorkspaceChannel = Channel & {
  agentId: string;
  agentName: string;
  environmentId: string | null;
};

const agentPageLimit = 100;
const maxAgents = 100;

export async function listAllWorkspaceAgents(workspaceId: string): Promise<AgentApiResponse[]> {
  const agents: AgentApiResponse[] = [];
  let page: PageCursor = null;
  do {
    const response = await listAgents(
      workspaceId,
      page ?? undefined,
      { status: 'active', created: 'all' },
      agentPageLimit,
    );
    agents.push(...(response.data ?? []));
    page = response.next_page ?? null;
  } while (page && agents.length < maxAgents);
  return agents.slice(0, maxAgents);
}

export async function listWorkspaceChannels(workspaceId: string): Promise<WorkspaceChannel[]> {
  const agents = await listAllWorkspaceAgents(workspaceId);
  const rows = await Promise.all(
    agents.map(async (agent) => {
      try {
        const [channels, settings] = await Promise.all([
          channelApi<{ data: Channel[] }>(agent.id, workspaceId),
          channelApi<EnvironmentSettings>(agent.id, workspaceId, '/settings'),
        ]);
        return (channels.data ?? []).map((channel) => ({
          ...channel,
          agentId: agent.id,
          agentName: agent.name,
          environmentId: settings.environment_id,
        }));
      } catch {
        return [] as WorkspaceChannel[];
      }
    }),
  );
  return rows.flat();
}
