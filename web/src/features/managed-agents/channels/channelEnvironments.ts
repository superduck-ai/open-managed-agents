import { anthropicBetaApi } from '../../../shared/api/anthropic';
import type { EnvironmentApiResponse } from '../types';

export async function listChannelEnvironments(workspaceId: string) {
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
}
