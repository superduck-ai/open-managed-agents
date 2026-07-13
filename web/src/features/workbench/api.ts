import { anthropicBetaApi } from '../../shared/api/anthropic';
import { consoleApi } from '../../shared/api/client';
import { postJsonSseStream, type ServerSentEvent } from '../../shared/api/streaming';

export type WorkbenchRole = 'human' | 'assistant';

export type WorkbenchContentBlock = {
  type: string;
  text?: string;
  [key: string]: unknown;
};

export type WorkbenchMessage = {
  role: WorkbenchRole | string;
  content: string | WorkbenchContentBlock[];
};

export type WorkbenchThinking = {
  type: 'disabled' | 'enabled' | 'adaptive' | string;
  effort?: 'low' | 'medium' | 'high' | string;
  budget_tokens?: number;
  [key: string]: unknown;
};

export type WorkbenchTool = {
  id?: string;
  type?: string;
  name: string;
  description?: string;
  input_schema?: Record<string, unknown>;
  max_uses?: number;
  allowed_domains?: string[];
  blocked_domains?: string[];
  [key: string]: unknown;
};

export type WorkbenchRevision = {
  id: string;
  created_at: string;
  is_latest: boolean;
  model_name: string;
  system_prompt: string;
  messages: WorkbenchMessage[];
  variables: string[];
  tools: WorkbenchTool[];
  max_tokens_to_sample: number;
  temperature?: number;
  thinking: WorkbenchThinking;
  show_raw_thinking: boolean;
  skip_system_modification: boolean;
  [key: string]: unknown;
};

export type WorkbenchPromptSummary = {
  id: string;
  name: string;
  workspace_id?: string;
  created_at?: string;
  updated_at?: string;
  is_shared_with_workspace?: boolean;
  creator?: {
    tagged_id?: string;
    uuid?: string;
    full_name?: string;
    email_address?: string;
    [key: string]: unknown;
  };
};

export type WorkbenchPromptDetail = WorkbenchPromptSummary & {
  latest_revision?: WorkbenchRevision;
  kv_store?: {
    draft_revision?: string;
    [key: string]: unknown;
  };
};

export type WorkbenchModel = {
  model_name: string;
  display_name?: string;
  name?: string;
  model_group?: string;
  max_tokens?: number;
  max_context_window?: number;
  supports_thinking?: boolean;
  supports_tool_use?: boolean;
  supports_vision?: boolean;
  [key: string]: unknown;
};

export type WorkbenchUploadedFile = {
  id: string;
  type?: string;
  filename?: string;
  mime_type?: string;
  size_bytes?: number;
  created_at?: string;
  downloadable?: boolean;
  [key: string]: unknown;
};

export type WorkbenchModelsResponse = {
  default_prompt_settings?: {
    model_name?: string;
    system_prompt?: string;
    temperature?: number;
    max_tokens_to_sample?: number;
  };
  models?: WorkbenchModel[];
};

export type WorkbenchKVResponse = {
  success: boolean;
  value?: string;
  version?: unknown;
};

export type WorkbenchStreamEvent = ServerSentEvent<Record<string, unknown>>;

export type WorkbenchEvaluation = {
  id: string;
  revision_id?: string;
  test_case_id?: string;
  variable_values?: Record<string, unknown>;
  golden_answer?: unknown;
  completion?: unknown;
  completion_text?: unknown;
  rating?: unknown;
  created_at?: string;
  [key: string]: unknown;
};

export function getWorkbenchModels(orgUuid: string) {
  return consoleApi<WorkbenchModelsResponse>(orgPath(orgUuid, '/models'));
}

export function listWorkspacePrompts(orgUuid: string, workspaceId: string) {
  return consoleApi<WorkbenchPromptSummary[]>(
    orgPath(orgUuid, `/workspaces/${encodeURIComponent(workspaceId)}/prompts`),
  );
}

export function listWorkbenchPrompts(orgUuid: string) {
  return consoleApi<WorkbenchPromptSummary[]>(orgPath(orgUuid, '/workbench/prompts'));
}

export function createWorkspacePrompt(
  orgUuid: string,
  workspaceId: string,
  input: { name?: string; latest_revision?: Partial<WorkbenchRevision> } = {},
) {
  return consoleApi<WorkbenchPromptDetail>(orgPath(orgUuid, `/workspaces/${encodeURIComponent(workspaceId)}/prompts`), {
    method: 'POST',
    body: JSON.stringify(input),
  });
}

export function uploadWorkbenchFile(file: File) {
  return anthropicBetaApi.files.upload<WorkbenchUploadedFile>(file);
}

export function getWorkbenchPrompt(orgUuid: string, promptId: string) {
  return consoleApi<WorkbenchPromptDetail>(orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}`));
}

export function updateWorkbenchPrompt(orgUuid: string, promptId: string, input: { name?: string }) {
  return consoleApi<WorkbenchPromptDetail>(orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}`), {
    method: 'PUT',
    body: JSON.stringify(input),
  });
}

export function deleteWorkbenchPrompt(orgUuid: string, promptId: string) {
  return consoleApi<Record<string, unknown>>(orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}`), {
    method: 'DELETE',
  });
}

export function shareWorkbenchPrompt(orgUuid: string, promptId: string) {
  return consoleApi<WorkbenchPromptDetail>(
    orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}/sharing`),
    {
      method: 'POST',
      body: JSON.stringify({}),
    },
  );
}

export function listWorkbenchRevisions(orgUuid: string, promptId: string, compact = false) {
  const suffix = compact ? '?compact=true' : '';
  return consoleApi<WorkbenchRevision[]>(
    orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}/revisions${suffix}`),
  );
}

export function getWorkbenchRevision(orgUuid: string, promptId: string, revisionId: string) {
  return consoleApi<WorkbenchRevision>(
    orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}/revisions/${encodeURIComponent(revisionId)}`),
  );
}

export function createWorkbenchRevision(orgUuid: string, promptId: string, revision: WorkbenchRevision) {
  return consoleApi<WorkbenchRevision>(
    orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}/revisions`),
    {
      method: 'POST',
      body: JSON.stringify(revision),
    },
  );
}

export function getWorkbenchKV(orgUuid: string, promptId: string, key: string) {
  return consoleApi<WorkbenchKVResponse>(
    orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}/kv_store/get/${encodeURIComponent(key)}`),
  );
}

export function setWorkbenchKV(orgUuid: string, promptId: string, key: string, value: string, version?: unknown) {
  return consoleApi<WorkbenchKVResponse>(
    orgPath(orgUuid, `/workbench/prompts/${encodeURIComponent(promptId)}/kv_store/set/${encodeURIComponent(key)}`),
    {
      method: 'POST',
      body: JSON.stringify({ value, version }),
    },
  );
}

export function listWorkbenchEvaluations(orgUuid: string, revisionId: string) {
  return consoleApi<WorkbenchEvaluation[]>(
    orgPath(orgUuid, `/workbench/revisions/${encodeURIComponent(revisionId)}/evaluations/list`),
  );
}

export function createWorkbenchEvaluation(orgUuid: string, revisionId: string, body: Partial<WorkbenchEvaluation>) {
  return consoleApi<WorkbenchEvaluation>(
    orgPath(orgUuid, `/workbench/revisions/${encodeURIComponent(revisionId)}/evaluations/create`),
    {
      method: 'POST',
      body: JSON.stringify(body),
    },
  );
}

export function updateWorkbenchEvaluationVariables(
  orgUuid: string,
  evaluationId: string,
  variableValues: Record<string, string>,
) {
  return consoleApi<WorkbenchEvaluation>(
    orgPath(orgUuid, `/workbench/evaluations/${encodeURIComponent(evaluationId)}/update_variables`),
    {
      method: 'POST',
      body: JSON.stringify({ variable_values: variableValues }),
    },
  );
}

export function updateWorkbenchEvaluationGoldenAnswer(orgUuid: string, evaluationId: string, goldenAnswer: string) {
  return consoleApi<WorkbenchEvaluation>(
    orgPath(orgUuid, `/workbench/evaluations/${encodeURIComponent(evaluationId)}/update_golden_answer`),
    {
      method: 'POST',
      body: JSON.stringify({ golden_answer: goldenAnswer }),
    },
  );
}

export function saveWorkbenchEvaluationCompletion(orgUuid: string, evaluationId: string, completionText: string) {
  return consoleApi<WorkbenchEvaluation>(
    orgPath(orgUuid, `/workbench/evaluations/${encodeURIComponent(evaluationId)}/save_completion`),
    {
      method: 'POST',
      body: JSON.stringify({ completion_text: completionText }),
    },
  );
}

export function deleteWorkbenchEvaluation(orgUuid: string, evaluationId: string) {
  return consoleApi<WorkbenchEvaluation>(
    orgPath(orgUuid, `/workbench/evaluations/${encodeURIComponent(evaluationId)}/delete`),
    {
      method: 'POST',
      body: JSON.stringify({}),
    },
  );
}

export function getPrepaidCredits(orgUuid: string) {
  return consoleApi<Record<string, unknown>>(orgPath(orgUuid, '/prepaid/credits'));
}

export function streamWorkbenchCompletion(input: {
  orgUuid: string;
  workspaceId: string;
  body: unknown;
  signal: AbortSignal;
  onEvent: (event: WorkbenchStreamEvent) => void;
}) {
  return postWorkbenchStream({
    orgUuid: input.orgUuid,
    workspaceId: input.workspaceId,
    path: '/workbench/completions',
    body: input.body,
    signal: input.signal,
    onEvent: input.onEvent,
  });
}

export function generateWorkbenchTitle(input: {
  orgUuid: string;
  workspaceId: string;
  body: { message_content: string; model: string };
  signal?: AbortSignal;
}) {
  return consoleApi<{ completion?: string }>(orgPath(input.orgUuid, '/workbench/generate_title'), {
    method: 'POST',
    headers: input.workspaceId ? { 'X-Workspace-ID': input.workspaceId } : undefined,
    body: JSON.stringify(input.body),
    signal: input.signal,
  });
}

export function streamGenerateTestCase(input: {
  orgUuid: string;
  workspaceId: string;
  body: unknown;
  signal: AbortSignal;
  onEvent: (event: WorkbenchStreamEvent) => void;
}) {
  return postWorkbenchStream({
    orgUuid: input.orgUuid,
    workspaceId: input.workspaceId,
    path: '/workbench/evaluations/generate_test_case',
    body: input.body,
    signal: input.signal,
    onEvent: input.onEvent,
  });
}

export function streamGenerateTestCases(input: {
  orgUuid: string;
  workspaceId: string;
  body: unknown;
  signal: AbortSignal;
  onEvent: (event: WorkbenchStreamEvent) => void;
}) {
  return postWorkbenchStream({
    orgUuid: input.orgUuid,
    workspaceId: input.workspaceId,
    path: '/workbench/metaprompt/generate_test_cases',
    body: input.body,
    signal: input.signal,
    onEvent: input.onEvent,
  });
}

export function streamGeneratePrompt(input: {
  orgUuid: string;
  workspaceId: string;
  body: unknown;
  signal: AbortSignal;
  onEvent: (event: WorkbenchStreamEvent) => void;
}) {
  return postWorkbenchStream({
    orgUuid: input.orgUuid,
    workspaceId: input.workspaceId,
    path: '/workbench/generate_prompt',
    body: input.body,
    signal: input.signal,
    onEvent: input.onEvent,
  });
}

async function postWorkbenchStream({
  orgUuid,
  workspaceId,
  path,
  body,
  signal,
  onEvent,
}: {
  orgUuid: string;
  workspaceId: string;
  path: string;
  body: unknown;
  signal: AbortSignal;
  onEvent: (event: WorkbenchStreamEvent) => void;
}) {
  const headers = new Headers({
    Accept: 'text/event-stream',
    'Content-Type': 'application/json',
    'X-Organization-UUID': orgUuid,
  });
  if (workspaceId) {
    headers.set('X-Workspace-ID', workspaceId);
  }
  await postJsonSseStream<Record<string, unknown>>({
    url: orgPath(orgUuid, path),
    headers,
    body,
    signal,
    onEvent,
    errorFromResponse: workbenchStreamError,
  });
}

async function workbenchStreamError(response: Response) {
  try {
    const payload = (await response.json()) as Record<string, unknown>;
    const error = payload.error;
    if (error && typeof error === 'object') {
      const message = (error as Record<string, unknown>).message;
      if (typeof message === 'string') {
        return new Error(message);
      }
    }
    if (typeof error === 'string') {
      return new Error(error);
    }
    if (typeof payload.message === 'string') {
      return new Error(payload.message);
    }
  } catch {
    // Keep the fallback below.
  }
  return new Error(response.statusText || `Request failed with status ${response.status}`);
}

function orgPath(orgUuid: string, path: string) {
  return `/api/organizations/${encodeURIComponent(orgUuid)}${path}`;
}
