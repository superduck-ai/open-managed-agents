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

export type EvaluationPresetVersion = {
  id: string;
  version: number;
  case_count: number;
};

export type EvaluationDatasetPreset = {
  id: string;
  type: 'evaluation_dataset_preset';
  name: string;
  desc: string;
  status: 'available' | 'archived';
  versions: EvaluationPresetVersion[];
  published_at: string;
};

export type EvaluationDatasetPresetPage = {
  data: EvaluationDatasetPreset[];
  next_page: string | null;
};

export type EvaluationEvaluatorPreset = Omit<EvaluationEvaluator, 'type'> & {
  type: 'evaluation_evaluator_preset';
};

export type EvaluationEvaluatorPresetPage = {
  data: EvaluationEvaluatorPreset[];
  next_page: string | null;
};

export type CreateEvaluationDatasetInput = {
  name: string;
  desc?: string;
  source_version_id?: string;
};

export type CreateEvaluationEvaluatorFromPresetInput = {
  name: string;
  desc?: string;
  source_version_id: string;
  agent_id: string;
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

export type CreateEvaluationCaseFromSessionInput = {
  session_id: string;
  name?: string;
  desc?: string;
  checkpoints?: unknown;
  include_output_files?: boolean;
};

export type CreateEvaluationCasesFromSessionsInput = {
  session_ids: string[];
  name_prefix?: string;
  desc?: string;
  checkpoints?: unknown;
  include_output_files?: boolean;
};

export type EvaluationCaseFromSessionsResult = {
  created: EvaluationCase[];
  failed: Array<{ session_id: string; error: string }>;
};

export type EvaluationExperimentStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled';

export type EvaluationExperimentEvaluator = {
  id: string;
  version_id: string;
  position: number;
};

export type EvaluationExperimentItem = {
  id: string;
  case_id: string;
  position: number;
  session_id?: string;
  status: EvaluationExperimentStatus;
  error?: string;
};

export type EvaluationExperimentScore = {
  id: string;
  item_id: string;
  evaluator_id: string;
  version_id: string;
  status: 'completed' | 'failed';
  score?: number;
  explanation?: string;
  error?: string;
};

export type EvaluationExperiment = {
  id: string;
  type: 'evaluation_experiment';
  name: string;
  desc: string;
  status: EvaluationExperimentStatus;
  dataset_id: string;
  version_id: string;
  agent_id: string;
  agent_version: number;
  environment_id: string;
  timeout_seconds: number;
  item_count: number;
  session_count: number;
  evaluator_count: number;
  evaluators?: EvaluationExperimentEvaluator[];
  items?: EvaluationExperimentItem[];
  scores?: EvaluationExperimentScore[];
  created_at: string;
  updated_at: string;
};

export type EvaluationExperimentPage = {
  data: EvaluationExperiment[];
  next_page: string | null;
};

export type CreateEvaluationExperimentInput = {
  name: string;
  desc?: string;
  dataset_version_id: string;
  agent_id: string;
  agent_version?: number;
  environment_id: string;
  timeout_seconds: number;
  evaluator_ids: string[];
};

export type CompareEvaluationExperimentsInput = {
  dataset_version_id: string;
  evaluator_ids: string[];
};

function collectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-datasets`;
}

function evaluatorCollectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-evaluators`;
}

function experimentCollectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-experiments`;
}

function datasetPath(orgUuid: string, workspaceId: string, datasetId: string) {
  return `${collectionPath(orgUuid, workspaceId)}/${encodeURIComponent(datasetId)}`;
}

function datasetPresetCollectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-dataset-presets`;
}

function evaluatorPresetCollectionPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/evaluation-evaluator-presets`;
}

function workspaceHeaders(workspaceId: string) {
  return { 'X-Workspace-ID': workspaceId };
}

export function listEvaluationDatasetPresets(orgUuid: string, workspaceId: string) {
  return consoleApi<EvaluationDatasetPresetPage>(datasetPresetCollectionPath(orgUuid, workspaceId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function listEvaluationDatasetPresetCases(
  orgUuid: string,
  workspaceId: string,
  presetId: string,
  versionId: string,
) {
  const path = `${datasetPresetCollectionPath(orgUuid, workspaceId)}/${encodeURIComponent(presetId)}/cases`;
  const query = versionId ? `?version_id=${encodeURIComponent(versionId)}` : '';
  return consoleApi<EvaluationCasePage>(`${path}${query}`, { headers: workspaceHeaders(workspaceId) });
}

export function listEvaluationEvaluatorPresets(orgUuid: string, workspaceId: string) {
  return consoleApi<EvaluationEvaluatorPresetPage>(evaluatorPresetCollectionPath(orgUuid, workspaceId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function createEvaluationEvaluatorFromPreset(
  orgUuid: string,
  workspaceId: string,
  input: CreateEvaluationEvaluatorFromPresetInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationEvaluator>(evaluatorCollectionPath(orgUuid, workspaceId), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
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

export function createEvaluationCaseFromSession(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  input: CreateEvaluationCaseFromSessionInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationCase>(`${datasetPath(orgUuid, workspaceId, datasetId)}/cases:from-session`, {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function createEvaluationCasesFromSessions(
  orgUuid: string,
  workspaceId: string,
  datasetId: string,
  input: CreateEvaluationCasesFromSessionsInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationCaseFromSessionsResult>(
    `${datasetPath(orgUuid, workspaceId, datasetId)}/cases:from-sessions`,
    {
      method: 'POST',
      headers: workspaceHeaders(workspaceId),
      csrfToken,
      body: JSON.stringify(input),
    },
  );
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

export function listEvaluationExperiments(orgUuid: string, workspaceId: string) {
  return consoleApi<EvaluationExperimentPage>(experimentCollectionPath(orgUuid, workspaceId), {
    headers: workspaceHeaders(workspaceId),
  });
}

export function getEvaluationExperiment(orgUuid: string, workspaceId: string, experimentId: string) {
  return consoleApi<EvaluationExperiment>(
    `${experimentCollectionPath(orgUuid, workspaceId)}/${encodeURIComponent(experimentId)}`,
    { headers: workspaceHeaders(workspaceId) },
  );
}

export function createEvaluationExperiment(
  orgUuid: string,
  workspaceId: string,
  input: CreateEvaluationExperimentInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationExperiment>(experimentCollectionPath(orgUuid, workspaceId), {
    method: 'POST',
    headers: workspaceHeaders(workspaceId),
    csrfToken,
    body: JSON.stringify(input),
  });
}

export function startEvaluationExperiment(
  orgUuid: string,
  workspaceId: string,
  experimentId: string,
  csrfToken?: string,
) {
  return consoleApi<EvaluationExperiment>(
    `${experimentCollectionPath(orgUuid, workspaceId)}/${encodeURIComponent(experimentId)}:start`,
    { method: 'POST', headers: workspaceHeaders(workspaceId), csrfToken },
  );
}

export function compareEvaluationExperiments(
  orgUuid: string,
  workspaceId: string,
  input: CompareEvaluationExperimentsInput,
  csrfToken?: string,
) {
  return consoleApi<EvaluationExperimentPage>(`${experimentCollectionPath(orgUuid, workspaceId)}:compare`, {
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
