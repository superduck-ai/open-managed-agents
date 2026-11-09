import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { listAgents } from '../api';
import type { AgentApiResponse, PageCursor } from '../types';
import type { Channel } from './api';

export type WorkspaceChannel = Channel & {
  agentId: string;
  agentName: string;
  environmentId: string | null;
};

export type WorkspaceChannelList = {
  channels: WorkspaceChannel[];
  incomplete: boolean;
  truncated: boolean;
};

type WorkspaceChannelPayload = Channel & {
  agent_id?: string;
  agent_name?: string;
  environment_id?: string | null;
};

type WorkspaceChannelResponse = {
  data?: WorkspaceChannelPayload[];
  incomplete?: boolean;
  truncated?: boolean;
  next_page?: string | null;
};

const channelPageLimit = 100;
const maxChannelPages = 5;

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

export async function listWorkspaceChannels(workspaceId: string): Promise<WorkspaceChannelList> {
  const channels: WorkspaceChannel[] = [];
  let cursor: string | null = null;
  let incomplete = false;
  let truncated = false;
  for (let pageIndex = 0; pageIndex < maxChannelPages; pageIndex += 1) {
    const params = { limit: channelPageLimit, page: cursor };
    const response: WorkspaceChannelResponse = await anthropicBetaApi.channels.list(workspaceId, params);
    channels.push(...(response.data ?? []).map(toWorkspaceChannel));
    incomplete = incomplete || Boolean(response.incomplete);
    truncated = truncated || Boolean(response.truncated);
    cursor = response.next_page ?? null;
    if (!cursor) break;
    if (pageIndex + 1 >= maxChannelPages) truncated = true;
  }
  return { channels, incomplete, truncated };
}

function toWorkspaceChannel(row: WorkspaceChannelPayload): WorkspaceChannel {
  const agentId = row.agent_id ?? '';
  return {
    ...row,
    agentId,
    agentName: row.agent_name || agentId,
    environmentId: row.environment_id ?? null,
  };
}
