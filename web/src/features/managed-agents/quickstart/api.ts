import { anthropicBetaApi } from '../../../shared/api/anthropic';
import { listAgents, listManagedEntities } from '../api';
import type { AgentApiResponse, EnvironmentApiResponse, PageResponse, SessionApiResponse } from '../types';
import { environmentFormBody, environmentFormValues } from '../environments/model';

async function allPages<T>(load: (page?: string) => Promise<PageResponse<T>>) {
  const items: T[] = [];
  const cursors = new Set<string>();
  let cursor: string | undefined;
  do {
    const page = await load(cursor);
    items.push(...page.data);
    cursor = page.next_page ?? undefined;
    if (cursor && cursors.has(cursor)) throw new Error('Resource pagination returned a repeated cursor.');
    if (cursor) cursors.add(cursor);
  } while (cursor);
  return items;
}

export async function loadQuickstartResources(workspaceID: string) {
  const [agents, environments] = await Promise.all([
    allPages<AgentApiResponse>((page) => listAgents(workspaceID, page, { created: 'all', status: 'active' }, 100)),
    allPages<EnvironmentApiResponse>(async (page) => {
      const result = await listManagedEntities('environments', workspaceID, page, { includeArchived: true });
      return {
        ...result,
        data: result.data.filter((item): item is EnvironmentApiResponse => item.type === 'environment'),
      };
    }),
  ]);
  return { agents, environments };
}

export function quickstartEnvironmentBody(name: string) {
  return environmentFormBody({ ...environmentFormValues(), name });
}

export function saveQuickstartEnvironment(name: string, operationID: string, workspaceID: string) {
  return anthropicBetaApi.environments.create<EnvironmentApiResponse>(
    { ...quickstartEnvironmentBody(name), metadata: { quickstart_operation_id: operationID } },
    workspaceID,
  );
}

export function saveQuickstartSession(
  agent: AgentApiResponse,
  environmentID: string,
  operationID: string,
  workspaceID: string,
) {
  return anthropicBetaApi.sessions.create<SessionApiResponse>(
    {
      agent: { type: 'agent', id: agent.id, version: agent.version },
      environment_id: environmentID,
      metadata: { quickstart_operation_id: operationID },
      vault_ids: [],
      resources: [],
    },
    workspaceID,
  );
}

export async function findQuickstartSession(operationID: string, workspaceID: string) {
  const sessions = await allPages<SessionApiResponse>(async (page) => {
    const result = await listManagedEntities('sessions', workspaceID, page);
    return { ...result, data: result.data.filter((item): item is SessionApiResponse => item.type === 'session') };
  });
  return sessions.filter((session) => session.metadata?.quickstart_operation_id === operationID);
}
