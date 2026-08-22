import { consoleApi } from '../../shared/api/client';

export type LLMProvider = {
  type: 'llm_provider';
  id: string;
  name: string;
  base_url: string;
  has_api_key: boolean;
  api_key_last4: string;
  model_ids: string[];
  skipped_model_ids?: string[];
  created_at: string;
  updated_at: string;
};

export type LLMProviderInput = {
  name: string;
  base_url: string;
  api_key?: string;
  model_ids: string[];
};

function providersPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/llm_providers`;
}

export function listLLMProviders(orgUuid: string, workspaceId: string) {
  return consoleApi<LLMProvider[]>(providersPath(orgUuid, workspaceId));
}

export function createLLMProvider(orgUuid: string, workspaceId: string, input: LLMProviderInput, csrfToken?: string) {
  return consoleApi<LLMProvider>(providersPath(orgUuid, workspaceId), {
    method: 'POST',
    body: JSON.stringify(input),
    csrfToken,
  });
}

export function updateLLMProvider(
  orgUuid: string,
  workspaceId: string,
  providerId: string,
  input: LLMProviderInput,
  csrfToken?: string,
) {
  return consoleApi<LLMProvider>(`${providersPath(orgUuid, workspaceId)}/${encodeURIComponent(providerId)}`, {
    method: 'PUT',
    body: JSON.stringify(input),
    csrfToken,
  });
}

export function deleteLLMProvider(orgUuid: string, workspaceId: string, providerId: string, csrfToken?: string) {
  return consoleApi<void>(`${providersPath(orgUuid, workspaceId)}/${encodeURIComponent(providerId)}`, {
    method: 'DELETE',
    csrfToken,
  });
}

export function previewProviderModels(
  orgUuid: string,
  workspaceId: string,
  input: { base_url: string; api_key: string },
  csrfToken?: string,
) {
  return consoleApi<{ model_ids: string[] }>(`${providersPath(orgUuid, workspaceId)}/preview_models`, {
    method: 'POST',
    body: JSON.stringify(input),
    csrfToken,
  });
}

export function syncProviderModels(orgUuid: string, workspaceId: string, providerId: string, csrfToken?: string) {
  return consoleApi<LLMProvider>(
    `${providersPath(orgUuid, workspaceId)}/${encodeURIComponent(providerId)}/models/sync`,
    {
      method: 'POST',
      csrfToken,
    },
  );
}
