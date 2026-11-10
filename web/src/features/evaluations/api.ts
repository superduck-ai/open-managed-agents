import { consoleApi } from '../../shared/api/client';
import { consoleResourceListLimit } from '../../shared/console-list';

export type EvaluationPage<T> = {
  data: T[];
  next_page: string | null;
};

export type EvaluationDataset = {
  id: string;
  name: string;
  desc: string;
  version_id: string;
  version: number;
  case_count: number;
  created_at: string;
  updated_at: string;
};

export type EvaluationCase = {
  id: string;
  query: string;
  expected_answer: string;
  expected_tool_trace: unknown;
  source_session_id: string;
  polarity: string;
  acceptance: string;
};

export function evaluationBase(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(
    workspaceId,
  )}/evaluations`;
}

export function evaluationListPath(path: string, page?: string) {
  const params = new URLSearchParams({ limit: String(consoleResourceListLimit) });
  if (page) params.set('page', page);
  return `${path}?${params.toString()}`;
}

export function listEvaluationDatasets(orgUuid: string, workspaceId: string, page?: string) {
  return consoleApi<EvaluationPage<EvaluationDataset>>(
    evaluationListPath(`${evaluationBase(orgUuid, workspaceId)}/datasets`, page),
  );
}

export function createEvaluationDataset(
  orgUuid: string,
  workspaceId: string,
  input: { name: string; desc: string; content?: string },
) {
  return consoleApi<EvaluationDataset>(`${evaluationBase(orgUuid, workspaceId)}/datasets`, {
    method: 'POST',
    body: JSON.stringify({
      name: input.name,
      desc: input.desc,
      content: input.content?.trim() ? input.content : undefined,
    }),
  });
}

export type SessionTurn = {
  index: number;
  query: string;
  answer: string;
};

export function listSessionTurns(orgUuid: string, workspaceId: string, datasetId: string, sessionId: string) {
  const params = new URLSearchParams({ session_id: sessionId });
  return consoleApi<EvaluationPage<SessionTurn>>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(
      datasetId,
    )}/session-turns?${params.toString()}`,
  );
}

export function importEvaluationSession(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  input: { session_id: string; turns: number[]; polarity?: string; acceptance?: string },
) {
  return consoleApi<EvaluationPage<EvaluationCase>>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}/cases:from-session`,
    { method: 'POST', body: JSON.stringify(input) },
  );
}

export function getEvaluationDataset(orgUuid: string, workspaceId: string, datasetId: string) {
  return consoleApi<EvaluationDataset>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}`,
  );
}

export function createEvaluationCases(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  cases: Array<Pick<EvaluationCase, 'query' | 'expected_answer' | 'polarity' | 'acceptance'>>,
) {
  return consoleApi<EvaluationPage<EvaluationCase>>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}/cases`,
    { method: 'POST', body: JSON.stringify({ cases }) },
  );
}

export function updateEvaluationCase(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  caseId: string,
  input: Pick<
    EvaluationCase,
    'query' | 'expected_answer' | 'expected_tool_trace' | 'polarity' | 'acceptance' | 'source_session_id'
  >,
) {
  return consoleApi<EvaluationCase>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}/cases/${encodeURIComponent(
      caseId,
    )}`,
    { method: 'POST', body: JSON.stringify(input) },
  );
}

export function deleteEvaluationCase(orgUuid: string, workspaceId: string, datasetId: string, caseId: string) {
  return consoleApi<{ id: string }>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}/cases/${encodeURIComponent(
      caseId,
    )}`,
    { method: 'DELETE' },
  );
}

export function generateEvaluationCases(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  input: { count: number; note: string; agent_id?: string; agent_version?: number },
) {
  return consoleApi<EvaluationPage<EvaluationCase>>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}/cases:generate`,
    { method: 'POST', body: JSON.stringify(input) },
  );
}

export function deleteEvaluationDataset(orgUuid: string, workspaceId: string, datasetId: string) {
  return consoleApi<{ id: string }>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}`,
    { method: 'DELETE' },
  );
}

export function listEvaluationCases(orgUuid: string, workspaceId: string, datasetId: string) {
  return consoleApi<EvaluationPage<EvaluationCase>>(
    `${evaluationBase(orgUuid, workspaceId)}/datasets/${encodeURIComponent(datasetId)}/cases`,
  );
}

export function evaluationError(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Request failed';
}
