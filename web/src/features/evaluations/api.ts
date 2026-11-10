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

export type EvaluationContentBlock =
  { type: 'text'; text: string } | { type: 'image' | 'document'; source: { type: 'file'; file_id: string } };

export type EvaluationCase = {
  id: string;
  query: string;
  expected_answer: string;
  expected_tool_trace: unknown;
  source_session_id: string;
  polarity: string;
  acceptance: string;
  inputs?: EvaluationContentBlock[];
};

export type EvaluationRubricVersion = {
  id: string;
  version: number;
  body: string;
  pass_score: number;
  model: string;
  created_at: string;
};

export type EvaluationRubric = {
  id: string;
  name: string;
  desc: string;
  version_id: string;
  version: number;
  body: string;
  pass_score: number;
  model: string;
  created_at: string;
};

export type EvaluationSummary = {
  round: number;
  score: number | null;
  pass_rate: number | null;
  scored: number;
  passed: number;
  exceptions: number;
  total: number;
};

export type EvaluationScore = {
  status: string;
  score: number;
  passed: boolean;
  explanation: string;
  dimensions: Array<{ name?: string; score?: number }>;
  error_message: string;
};

export type EvaluationItem = {
  id: string;
  round: number;
  case_id: string;
  query: string;
  expected_answer: string;
  session_id: string;
  status: string;
  error_message: string;
  score: EvaluationScore | null;
};

export type EvaluationTask = {
  id: string;
  name: string;
  mode: 'evaluate';
  status: string;
  agent_id: string;
  agent_version: number;
  environment_id: string;
  dataset_id: string;
  dataset_version_id: string;
  rubric_id: string;
  rubric_version_id: string;
  pass_score: number;
  error_message: string;
  created_at: string;
  summary: EvaluationSummary;
  rounds?: EvaluationSummary[];
  items?: EvaluationItem[];
};

export type EvaluationTaskInput = {
  name: string;
  mode: 'evaluate';
  agent_id: string;
  environment_id: string;
  dataset_id: string;
  rubric_id: string;
  agent_version?: number;
  focus?: string;
  vault_ids?: string[];
  resources?: unknown[];
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
  inputs?: EvaluationContentBlock[];
};

export type SessionImportResult = EvaluationPage<EvaluationCase> & { skipped: number };

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
  return consoleApi<SessionImportResult>(
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
  cases: Array<Pick<EvaluationCase, 'query' | 'expected_answer' | 'polarity' | 'acceptance' | 'inputs'>>,
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
    'query' | 'expected_answer' | 'expected_tool_trace' | 'polarity' | 'acceptance' | 'source_session_id' | 'inputs'
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

export function listEvaluationRubrics(orgUuid: string, workspaceId: string, page?: string) {
  return consoleApi<EvaluationPage<EvaluationRubric>>(
    evaluationListPath(`${evaluationBase(orgUuid, workspaceId)}/rubrics`, page),
  );
}

export function createEvaluationRubric(
  orgUuid: string,
  workspaceId: string,
  input: { name: string; desc: string; body: string; pass_score: number },
) {
  return consoleApi<EvaluationRubric>(`${evaluationBase(orgUuid, workspaceId)}/rubrics`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function generateEvaluationRubric(
  orgUuid: string,
  workspaceId: string,
  input: {
    name: string;
    desc: string;
    pass_score: number;
    goal?: string;
    example?: string;
    agent_id?: string;
    dataset_id?: string;
    positive?: string;
    negative?: string;
  },
) {
  return consoleApi<EvaluationRubric>(`${evaluationBase(orgUuid, workspaceId)}/rubrics/generate`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function listEvaluationRubricVersions(orgUuid: string, workspaceId: string, rubricId: string) {
  return consoleApi<EvaluationPage<EvaluationRubricVersion>>(
    `${evaluationBase(orgUuid, workspaceId)}/rubrics/${encodeURIComponent(rubricId)}/versions`,
  );
}

export function saveEvaluationRubricVersion(
  orgUuid: string,
  workspaceId: string,
  rubricId: string,
  input: { body: string; pass_score: number },
) {
  return consoleApi<EvaluationRubric>(
    `${evaluationBase(orgUuid, workspaceId)}/rubrics/${encodeURIComponent(rubricId)}/versions`,
    { method: 'POST', body: JSON.stringify(input) },
  );
}

export function deleteEvaluationRubric(orgUuid: string, workspaceId: string, rubricId: string) {
  return consoleApi<{ id: string }>(`${evaluationBase(orgUuid, workspaceId)}/rubrics/${encodeURIComponent(rubricId)}`, {
    method: 'DELETE',
  });
}

export function listEvaluationTasks(orgUuid: string, workspaceId: string, page?: string) {
  return consoleApi<EvaluationPage<EvaluationTask>>(
    evaluationListPath(`${evaluationBase(orgUuid, workspaceId)}/tasks`, page),
  );
}

export function createEvaluationTask(orgUuid: string, workspaceId: string, input: EvaluationTaskInput) {
  return consoleApi<EvaluationTask>(`${evaluationBase(orgUuid, workspaceId)}/tasks`, {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function getEvaluationTask(orgUuid: string, workspaceId: string, taskId: string) {
  return consoleApi<EvaluationTask>(`${evaluationBase(orgUuid, workspaceId)}/tasks/${encodeURIComponent(taskId)}`);
}

export function postEvaluationTaskAction(
  orgUuid: string,
  workspaceId: string,
  taskId: string,
  action: 'cancel' | 'retry',
) {
  return consoleApi<EvaluationTask>(
    `${evaluationBase(orgUuid, workspaceId)}/tasks/${encodeURIComponent(taskId)}/${action}`,
    { method: 'POST' },
  );
}

export function formatPassRate(rate: number | null | undefined) {
  if (rate == null || Number.isNaN(rate)) return '—';
  return `${Math.round(rate * 100)}%`;
}

export function formatScore(score: number | null | undefined) {
  if (score == null || Number.isNaN(score)) return '—';
  return score.toFixed(1);
}

export function evaluationError(error: unknown) {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string') {
    return error.message;
  }
  return 'Request failed';
}
