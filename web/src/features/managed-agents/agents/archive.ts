import { archiveAgent } from '../api';
import { errorMessage } from '../utils';

export async function archiveAgentBatch(agentIds: string[], workspaceId: string) {
  const results = await Promise.allSettled(agentIds.map((id) => archiveAgent(id, workspaceId)));
  const archivedIds: string[] = [];
  const errors: string[] = [];
  results.forEach((result, index) => {
    if (result.status === 'fulfilled') {
      archivedIds.push(agentIds[index]);
    } else {
      errors.push(errorMessage(result.reason));
    }
  });
  return { archivedIds, error: [...new Set(errors)].join('; ') || null };
}
