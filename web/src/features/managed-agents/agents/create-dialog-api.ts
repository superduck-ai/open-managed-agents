import { anthropicApi, anthropicBetaApi } from '../../../shared/api/anthropic';
import { exactAgentIdPattern, listAgents, retrieveAgent, searchAgentsByName } from '../api';
import { type AgentApiResponse } from '../types';
import { type AgentModelOption, type AgentSkillOption } from './create-dialog-model';
import { loadMcpDirectoryServers } from './tools/api';

type ModelsResponse = {
  data?: Array<{ id?: string; display_name?: string }>;
};

type SkillsResponse = {
  data?: Array<{
    id?: string;
    display_title?: string;
    latest_version?: string;
    source?: string;
  }>;
  next_page?: string | null;
};

export async function listCreateAgentModels(workspaceId: string): Promise<AgentModelOption[]> {
  const response = (await anthropicApi.models.list({ limit: 1000 }, workspaceId)) as ModelsResponse;
  return (response.data ?? []).flatMap((model) => {
    if (!model.id) {
      return [];
    }
    return [{ id: model.id, displayName: model.display_name || model.id }];
  });
}

export async function listCreateAgentSkills(workspaceId: string): Promise<AgentSkillOption[]> {
  const rows: AgentSkillOption[] = [];
  let page: string | undefined;
  for (let pageCount = 0; pageCount < 5; pageCount += 1) {
    const response = (await anthropicBetaApi.skills.list(
      { limit: 100, ...(page ? { page } : {}) },
      workspaceId,
    )) as SkillsResponse;
    for (const skill of response.data ?? []) {
      if (!skill.id) {
        continue;
      }
      rows.push({
        id: skill.id,
        displayTitle: skill.display_title || skill.id,
        latestVersion: skill.latest_version || 'latest',
        source: skill.source === 'anthropic' ? 'anthropic' : 'custom',
      });
    }
    page = response.next_page || undefined;
    if (!page) {
      break;
    }
  }
  return rows;
}

export async function searchCreateAgentSubagents(workspaceId: string, query: string): Promise<AgentApiResponse[]> {
  const normalizedQuery = query.trim();
  if (exactAgentIdPattern.test(normalizedQuery)) {
    try {
      const agent = await retrieveAgent(normalizedQuery, workspaceId);
      return agent.archived_at ? [] : [agent];
    } catch (error) {
      if ((error as { status?: number }).status === 404) {
        return [];
      }
      throw error;
    }
  }
  const response = normalizedQuery
    ? await searchAgentsByName(workspaceId, normalizedQuery, { created: 'all', status: 'active' })
    : await listAgents(workspaceId, undefined, { created: 'all', status: 'active' });
  return response.data.filter((agent) => !agent.archived_at);
}

export { loadMcpDirectoryServers };
