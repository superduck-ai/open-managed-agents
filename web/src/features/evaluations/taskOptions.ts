import { listAgents, listManagedEntities } from '../managed-agents/api';
import { allPages } from '../managed-agents/quickstart/api';
import type { AgentApiResponse, ManagedEntitySection } from '../managed-agents/types';
import { listEvaluationDatasets, listEvaluationRubrics } from './api';

export function listTaskAgents(workspaceId: string) {
  return allPages<AgentApiResponse>((page) => listAgents(workspaceId, page, { created: 'all', status: 'active' }, 100));
}

export function listTaskEntities(section: ManagedEntitySection, workspaceId: string) {
  return allPages((page) => listManagedEntities(section, workspaceId, page));
}

export function listTaskDatasets(orgUuid: string, workspaceId: string) {
  return allPages((page) => listEvaluationDatasets(orgUuid, workspaceId, page));
}

export function listTaskRubrics(orgUuid: string, workspaceId: string) {
  return allPages((page) => listEvaluationRubrics(orgUuid, workspaceId, page));
}
