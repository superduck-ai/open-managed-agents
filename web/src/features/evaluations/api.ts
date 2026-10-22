import { consoleApi, type ApiError } from '../../shared/api/client';

export type EvaluationDatasetVersion = {
  id: string;
  version: number;
  case_count: number;
};

export type EvaluationDataset = {
  id: string;
  type: 'evaluation_dataset';
  name: string;
  desc: string;
  status: 'available' | 'archived';
  version: EvaluationDatasetVersion;
  created_at: string;
  updated_at: string;
};

export type EvaluationDatasetPage = {
  data: EvaluationDataset[];
  next_page: string | null;
};

export type EvaluationCase = {
  id: string;
  type: 'evaluation_case';
  dataset_id: string;
  version_id: string;
  position: number;
  name: string;
  desc: string;
  turns: string[];
  checkpoints: unknown;
  input_files: unknown;
  reference_files: unknown;
  created_at: string;
  updated_at: string;
};

export type EvaluationCasePage = {
  data: EvaluationCase[];
  next_page: string | null;
};

export const evaluationVariables = ['input', 'output', 'checkpoints', 'agent_trajectory', 'invocation_result'] as const;

export type EvaluationVariable = (typeof evaluationVariables)[number];

export type EvaluationEvaluatorType = 'llm' | 'agent';

export type EvaluationEvaluatorContract = {
  result_path: string;
  score_path: string;
  score_fields: string[];
};

export type EvaluationEvaluatorVersion = {
  id: string;
  version: number;
  evaluator_type: EvaluationEvaluatorType;
  model?: string;
  agent_id?: string;
  prompt_template?: string;
  variables: string[];
  agent_contract?: EvaluationEvaluatorContract;
};

export type EvaluationEvaluator = {
  id: string;
  type: 'evaluation_evaluator';
  name: string;
  desc: string;
  status: 'available' | 'archived';
  version: EvaluationEvaluatorVersion;
  created_at: string;
  updated_at: string;
};

export type EvaluationEvaluatorPage = {
  data: EvaluationEvaluator[];
  next_page: string | null;
};

export type CreateEvaluationDatasetInput = {
  name: string;
  desc?: string;
};

export type CreateEvaluationEvaluatorInput = {
  name: string;
  desc?: string;
  evaluator_type: EvaluationEvaluatorType;
  model?: string;
  agent_id?: string;
  prompt_template?: string;
  variables: string[];
};

export type CreateEvaluationCaseInput = {
  name: string;
  desc?: string;
  turns: string[];
};

function collectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-datasets`;
}

function evaluatorCollectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-evaluators`;
}

function datasetPath(orgUuid: string, workspaceId: string, datasetId: string) {
  return `${collectionPath(orgUuid, workspaceId)}/${encodeURIComponent(datasetId)}`;
}

function workspaceHeaders(workspaceId: string) {
  return { 'X-Workspace-ID': workspaceId };
}

export function listEvaluationDatasets(orgUuid: string, workspaceId: string) {
  return consoleApi<EvaluationDatasetPage>(collectionPath(orgUuid, workspaceId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function getEvaluationDataset(orgUuid: string, workspaceId: string, datasetId: string) {
  return consoleApi<EvaluationDataset>(datasetPath(orgUuid, workspaceId, datasetId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function createEvaluationDataset(
  orgUuid: string,
  workspaceId: string,
  input: CreateEvaluationDatasetInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationDataset>(collectionPath(orgUuid, workspaceId), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function listEvaluationCases(orgUuid: string, workspaceId: string, datasetId: string) {
  return consoleApi<EvaluationCasePage>(`${datasetPath(orgUuid, workspaceId, datasetId)}/cases`, {
    headers: workspaceHeaders(workspaceId),
  });
}

export function createEvaluationCase(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  input: CreateEvaluationCaseInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationCase>(`${datasetPath(orgUuid, workspaceId, datasetId)}/cases`, {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function importEvaluationCases(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  jsonl: string,
  csrfToken?: string,
) {
  return consoleApi<EvaluationCasePage>(`${datasetPath(orgUuid, workspaceId, datasetId)}/cases:import`, {
    method: 'POST',
    headers: { ...workspaceHeaders(workspaceId), 'Content-Type': 'application/x-ndjson' },
    csrfToken,
    body: jsonl,
  });
}

export function listEvaluationEvaluators(orgUuid: string, workspaceId: string) {
  return consoleApi<EvaluationEvaluatorPage>(evaluatorCollectionPath(orgUuid, workspaceId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function createEvaluationEvaluator(
  orgUuid: string,
  workspaceId: string,
  input: CreateEvaluationEvaluatorInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationEvaluator>(evaluatorCollectionPath(orgUuid, workspaceId), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function evaluationErrorMessage(error: unknown) {
  const apiError = error as Partial<ApiError> | undefined;
  if (apiError && typeof apiError.message === 'string' && apiError.message.trim()) {
    return apiError.message;
  }
  return error instanceof Error ? error.message : 'Request failed';
}
