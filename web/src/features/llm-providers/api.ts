import { consoleApi, type ApiError } from '../../shared/api/client';

export type LLMProvider = {
  type: 'llm_provider';
  id: string;
  name: string;
  base_url: string;
  has_api_key: boolean;
  api_key_last4: string;
  model_ids: string[];
  skipped_model_ids?: string[];
  scope?: 'organization';
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

function organizationProviderPath(orgUuid: string) {
  return `/api/console/organizations/${encodeURIComponent(orgUuid)}/llm_provider`;
}

export type OrganizationLLMConsole = {
  console_url: string;
  base_url: string;
};

export function getOrganizationLLMConsole(orgUuid: string) {
  return consoleApi<OrganizationLLMConsole>(`/api/console/organizations/${encodeURIComponent(orgUuid)}/llm_console`);
}

export function getOrganizationLLMProvider(orgUuid: string) {
  return consoleApi<LLMProvider>(organizationProviderPath(orgUuid)).catch((error: unknown) => {
    if (isMissingProvider(error)) {
      return null;
    }
    throw error;
  });
}

export function upsertOrganizationLLMProvider(orgUuid: string, input: LLMProviderInput, csrfToken?: string) {
  return consoleApi<LLMProvider>(organizationProviderPath(orgUuid), {
    method: 'PUT',
    body: JSON.stringify(input),
    csrfToken,
  });
}

export function deleteOrganizationLLMProvider(orgUuid: string, csrfToken?: string) {
  return consoleApi<void>(organizationProviderPath(orgUuid), {
    method: 'DELETE',
    csrfToken,
  });
}

export function syncOrganizationLLMProvider(orgUuid: string, csrfToken?: string) {
  return consoleApi<LLMProvider>(`${organizationProviderPath(orgUuid)}/models/sync`, {
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
  if (editing?.scope === 'organization') {
    return upsertOrganizationLLMProvider(orgUuid, input, csrfToken);
  }
  if (editing) {
    return updateLLMProvider(orgUuid, workspaceId, editing.id, input, csrfToken);
  }
  return createLLMProvider(orgUuid, workspaceId, input, csrfToken);
}

export function removeLLMProvider(orgUuid: string, workspaceId: string, provider: LLMProvider, csrfToken?: string) {
  if (provider.scope === 'organization') {
    return deleteOrganizationLLMProvider(orgUuid, csrfToken);
  }
  return deleteLLMProvider(orgUuid, workspaceId, provider.id, csrfToken);
}

export function syncLLMProvider(orgUuid: string, workspaceId: string, provider: LLMProvider, csrfToken?: string) {
  if (provider.scope === 'organization') {
    return syncOrganizationLLMProvider(orgUuid, csrfToken);
  }
  return syncProviderModels(orgUuid, workspaceId, provider.id, csrfToken);
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
