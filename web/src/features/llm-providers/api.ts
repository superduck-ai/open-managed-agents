import { consoleApi, type ApiError } from '../../shared/api/client';

export type APIBackend = 'messages' | 'chat_completions';

export type LLMModel = {
  name?: string;
  id: string;
  context_window?: number;
  max_output_tokens?: number;
  input_modalities?: ('text' | 'image')[];
};

export type LLMProvider = {
  api_backend: APIBackend;
  type: 'llm_provider';
  id: string;
  name: string;
  slug: string;
  base_url: string;
  has_api_key: boolean;
  api_key_last4: string;
  models: LLMModel[];
  scope?: 'organization';
  created_at: string;
  updated_at: string;
};

export type LLMProviderInput = {
  api_backend: APIBackend;
  name: string;
  slug?: string;
  base_url: string;
  api_key?: string;
  models: LLMModel[];
};

function providersPath(orgUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/llm_providers`;
}

function organizationLLMProviderPath(orgUuid: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/llm_provider`;
}

export function getOrganizationLLMProvider(orgUuid: string) {
  return consoleApi<LLMProvider>(organizationLLMProviderPath(orgUuid)).catch((error: unknown) => {
    if (isMissingProvider(error)) {
      return null;
    }
    throw error;
  });
}

export function upsertOrganizationLLMProvider(orgUuid: string, input: LLMProviderInput, csrfToken?: string) {
  return consoleApi<LLMProvider>(organizationLLMProviderPath(orgUuid), {
    method: 'PUT',
    body: JSON.stringify(input),
    csrfToken,
  });
}

export function deleteOrganizationLLMProvider(orgUuid: string, csrfToken?: string) {
  return consoleApi<void>(organizationLLMProviderPath(orgUuid), {
    method: 'DELETE',
    csrfToken,
  });
}

export function syncOrganizationLLMProvider(orgUuid: string, csrfToken?: string) {
  return consoleApi<LLMProvider>(`${organizationLLMProviderPath(orgUuid)}/models/sync`, {
    method: 'POST',
    csrfToken,
  });
}

export function persistLLMProvider(
  orgUuid: string,
  workspaceId: string,
  editing: LLMProvider | null,
  input: LLMProviderInput,
  csrfToken?: string,
) {
  if (editing) {
    return updateLLMProvider(orgUuid, workspaceId, editing.id, input, csrfToken);
  }
  return createLLMProvider(orgUuid, workspaceId, input, csrfToken);
}

function isMissingProvider(error: unknown): error is ApiError {
  return Boolean(error && typeof error === 'object' && 'status' in error && (error as ApiError).status === 404);
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
  input: { base_url: string; api_key: string; api_backend: APIBackend },
  csrfToken?: string,
) {
  return consoleApi<{ models: LLMModel[] }>(`${providersPath(orgUuid, workspaceId)}/preview_models`, {
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

export function previewOrganizationProviderModels(
  orgUuid: string,
  input: { base_url: string; api_key: string; api_backend: APIBackend },
  csrfToken?: string,
) {
  return consoleApi<{ models: LLMModel[] }>(`${organizationLLMProviderPath(orgUuid)}/preview_models`, {
    method: 'POST',
    body: JSON.stringify(input),
    csrfToken,
  });
}

export function previewSavedProviderModels(
  orgUuid: string,
  workspaceId: string,
  provider: LLMProvider,
  csrfToken?: string,
) {
  const path =
    provider.scope === 'organization'
      ? organizationLLMProviderPath(orgUuid)
      : `${providersPath(orgUuid, workspaceId)}/${encodeURIComponent(provider.id)}`;
  return consoleApi<{ models: LLMModel[] }>(`${path}/models/preview`, { method: 'POST', csrfToken });
}

export type LLMProviderPreset = {
  order?: number;
  id: string;
  name: string;
  api_backend: APIBackend;
  base_url: string;
  models: LLMModel[];
};

export function listProviderPresets(orgUuid: string) {
  return consoleApi<ProviderPresetCatalog>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/llm_provider_presets`,
  );
}

export type ProviderPresetCatalog = {
  providers: LLMProviderPreset[];
  revision: string;
  writable: boolean;
};

export function saveProviderPresetModels(
  orgUuid: string,
  presetId: string,
  revision: string,
  models: LLMModel[],
  csrfToken?: string,
) {
  return consoleApi<ProviderPresetCatalog>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/llm_provider_presets/${encodeURIComponent(presetId)}/models`,
    {
      method: 'PUT',
      body: JSON.stringify({ revision, models }),
      csrfToken,
    },
  );
}
