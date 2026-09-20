import { consoleApi } from '../../shared/api/client';

const dreamBetaHeader = 'managed-agents-2026-04-01,dreaming-2026-04-21';

export type DreamStatus = 'pending' | 'running' | 'completed' | 'failed' | 'canceled';

export type DreamOutput = {
  type?: string;
  memory_store_id?: string;
};

export type DreamUsage = {
  input_tokens?: number;
  output_tokens?: number;
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
};

export type DreamError = {
  type: string;
  message?: string;
};

export type DreamModel = {
  id: string;
};

export type DreamInput = {
  type: string;
  memory_store_id?: string;
  session_ids?: string[];
};

export type Dream = {
  id: string;
  type: 'dream';
  status: DreamStatus;
  model: DreamModel;
  instructions?: string | null;
  session_id?: string | null;
  created_at: string;
  updated_at: string;
  ended_at?: string | null;
  archived_at?: string | null;
  inputs: DreamInput[];
  outputs: DreamOutput | DreamOutput[];
  usage?: DreamUsage | null;
  error?: DreamError | null;
};

export type CreateDreamInput = {
  memoryStoreId: string;
  model: string;
  sessionIds: string[];
  instructions: string;
};

export type DreamPage = {
  data: Dream[];
  next_page: string | null;
};

function dreamHeaders() {
  return {
    'anthropic-version': '2023-06-01',
    'anthropic-beta': dreamBetaHeader,
  };
}

export function listDreams(page?: string | null) {
  const query = new URLSearchParams({ limit: '20' });
  if (page) {
    query.set('page', page);
  }
  return consoleApi<DreamPage>(`/v1/dreams?${query.toString()}`, { headers: dreamHeaders() });
}

export function retrieveDream(dreamId: string) {
  return consoleApi<Dream>(`/v1/dreams/${encodeURIComponent(dreamId)}`, { headers: dreamHeaders() });
}

export function archiveDream(dreamId: string, csrfToken?: string) {
  return consoleApi<Dream>(`/v1/dreams/${encodeURIComponent(dreamId)}/archive`, {
    method: 'POST',
    headers: dreamHeaders(),
    csrfToken,
  });
}

export function cancelDream(dreamId: string, csrfToken?: string) {
  return consoleApi<Dream>(`/v1/dreams/${encodeURIComponent(dreamId)}/cancel`, {
    method: 'POST',
    headers: dreamHeaders(),
    csrfToken,
  });
}

export function createDream(input: CreateDreamInput, csrfToken?: string) {
  return consoleApi<Dream>('/v1/dreams', {
    method: 'POST',
    headers: dreamHeaders(),
    csrfToken,
    body: JSON.stringify({
      inputs: [
        { type: 'memory_store', memory_store_id: input.memoryStoreId },
        { type: 'sessions', session_ids: input.sessionIds },
      ],
      model: input.model,
      ...(input.instructions ? { instructions: input.instructions } : {}),
    }),
  });
}

export function dreamModelId(dream: Dream) {
  return dream.model.id;
}

export function dreamInputMemoryStoreId(dream: Dream) {
  return dream.inputs.find((input) => input.type === 'memory_store')?.memory_store_id ?? '—';
}

export function dreamSessionIds(dream: Dream) {
  return (
    dream.inputs.find((input) => input.type === 'sessions')?.session_ids ??
    dream.inputs.find((input) => input.type === 'memory_store')?.session_ids ??
    []
  );
}

export function dreamSessionCount(dream: Dream) {
  return dreamSessionIds(dream).length;
}

const DREAM_ERROR_LABELS: Record<string, string> = {
  internal_error: '平台内部错误',
  timeout: '运行超时',
  input_memory_store_unavailable: '输入记忆存储已被归档或删除',
  input_session_unavailable: '输入会话已被删除或归档',
  output_memory_store_unavailable: '输出记忆存储已被归档或删除',
  input_memory_store_too_large: '输入记忆存储超出大小限制',
  memory_store_org_limit_exceeded: '记忆存储数量已达上限',
};

export function dreamErrorLabel(error: DreamError) {
  return DREAM_ERROR_LABELS[error.type] ?? error.type;
}

export function dreamOutput(dream: Dream): DreamOutput | null {
  if (Array.isArray(dream.outputs)) return dream.outputs[0] ?? null;
  return dream.outputs && typeof dream.outputs === 'object' ? dream.outputs : null;
}
