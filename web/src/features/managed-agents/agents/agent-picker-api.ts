import { exactAgentIdPattern, listAgents, retrieveAgent, searchAgentsByNamePage } from '../api';
import { type AgentPageResponse, type PageCursor } from '../types';

export async function listAgentPickerPage(
  workspaceId: string,
  search: string,
  page?: PageCursor,
): Promise<AgentPageResponse> {
  if (exactAgentIdPattern.test(search)) {
    try {
      const agent = await retrieveAgent(search, workspaceId);
      return { data: agent.archived_at ? [] : [agent], next_page: null };
    } catch (error) {
      if ((error as { status?: number }).status === 404) return { data: [], next_page: null };
      throw error;
    }
  }
  return search ? searchAgentsByNamePage(workspaceId, search, 20, page) : listAgents(workspaceId, page);
}
