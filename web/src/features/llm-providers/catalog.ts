import type { LLMProvider } from './api';

type MessageFormatter = (id: string, defaultMessage: string, values?: Record<string, string>) => string;

const PROVIDER_ERROR_MESSAGES: Record<string, { id: string; defaultMessage: string }> = {
  api_key_required: {
    id: 'llmModels.errorApiKeyRequired',
    defaultMessage: 'API key is required.',
  },
  base_url_invalid: {
    id: 'llmModels.errorBaseUrlAbsolute',
    defaultMessage: 'Base URL must be an absolute HTTP or HTTPS URL.',
  },
  base_url_unsafe: {
    id: 'llmModels.errorBaseUrlUnsafe',
    defaultMessage: 'Base URL must not contain credentials, a query, or a fragment.',
  },
  model_id_invalid: {
    id: 'llmModels.errorModelIdLength',
    defaultMessage: 'Each model ID must contain 1 to 255 characters.',
  },
  upstream_models_unavailable: {
    id: 'llmModels.errorModelsListFailed',
    defaultMessage: 'Could not list models from this provider.',
  },
  llm_provider_not_found: {
    id: 'llmModels.errorProviderNotFound',
    defaultMessage: 'LLM provider not found.',
  },
  model_ids_limit: {
    id: 'llmModels.errorModelIdsLimit',
    defaultMessage: 'You can configure at most 100 model IDs.',
  },
  model_ids_duplicate: {
    id: 'llmModels.errorModelIdsDuplicate',
    defaultMessage: 'Model IDs must not contain duplicates.',
  },
  llm_provider_name_invalid: {
    id: 'llmModels.errorNameLength',
    defaultMessage: 'Provider name must contain 1 to 100 characters.',
  },
  llm_provider_name_conflict: {
    id: 'llmModels.errorProviderNameExists',
    defaultMessage: 'A provider with this name already exists.',
  },
  llm_provider_configuration_invalid: {
    id: 'llmModels.errorInvalidConfiguration',
    defaultMessage: 'The provider configuration is invalid.',
  },
  llm_provider_permission_denied: {
    id: 'llmModels.errorPermissionDenied',
    defaultMessage: 'Administrator access is required to manage LLM providers.',
  },
};

export const ALL_PROVIDERS_ID = 'all';

export type CatalogModel = {
  id: string;
  providerId: string;
  providerName: string;
};

export function catalogModels(providers: LLMProvider[]): CatalogModel[] {
  return providers.flatMap((provider) =>
    provider.model_ids.map((modelId) => ({
      id: modelId,
      providerId: provider.id,
      providerName: provider.name,
    })),
  );
}

export function filterCatalogModels(models: CatalogModel[], query: string): CatalogModel[] {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return models;
  }
  return models.filter(
    (model) => model.id.toLowerCase().includes(needle) || model.providerName.toLowerCase().includes(needle),
  );
}

export function modelsForProvider(models: CatalogModel[], providerId: string): CatalogModel[] {
  if (providerId === ALL_PROVIDERS_ID) {
    return models;
  }
  return models.filter((model) => model.providerId === providerId);
}

export type CatalogGroup = {
  providerId: string;
  providerName: string;
  models: CatalogModel[];
};

export function groupCatalogModels(
  models: CatalogModel[],
  providers: LLMProvider[],
  includeEmptyProviders: boolean,
): CatalogGroup[] {
  const byProvider = new Map<string, CatalogModel[]>();
  for (const model of models) {
    const rows = byProvider.get(model.providerId);
    if (rows) {
      rows.push(model);
    } else {
      byProvider.set(model.providerId, [model]);
    }
  }
  const groups = providers.flatMap((provider) => {
    const rows = byProvider.get(provider.id) ?? [];
    if (!includeEmptyProviders && rows.length === 0) {
      return [];
    }
    return [
      {
        providerId: provider.id,
        providerName: provider.name,
        models: rows,
      },
    ];
  });
  return groups;
}

export function resolvedProviderSelection(providers: LLMProvider[], providerId: string): string {
  if (providerId === ALL_PROVIDERS_ID) {
    return ALL_PROVIDERS_ID;
  }
  if (providers.some((provider) => provider.id === providerId)) {
    return providerId;
  }
  return providers.length === 1 ? providers[0].id : ALL_PROVIDERS_ID;
}

export function addableModelId(query: string, models: CatalogModel[]): string {
  if (query === '') {
    return '';
  }
  return models.some((model) => model.id === query) ? '' : query;
}

export function providerHost(baseUrl: string): string {
  try {
    return new URL(baseUrl).host;
  } catch {
    return baseUrl;
  }
}

export function providerInput(provider: LLMProvider, modelIds: string[]) {
  return {
    name: provider.name,
    base_url: provider.base_url,
    model_ids: modelIds,
  };
}

export function readableError(error: unknown, msg: MessageFormatter) {
  const code = errorCode(error);
  if (code === 'model_conflict') {
    const modelId = errorModelId(error);
    if (modelId) {
      return msg('llmModels.errorModelConflict', 'Model ID {modelId} is already configured by another provider.', {
        modelId,
      });
    }
  }
  const localized = PROVIDER_ERROR_MESSAGES[code];
  if (localized) {
    return msg(localized.id, localized.defaultMessage);
  }
  return msg('llmModels.errorRequestFailed', 'Request failed. Please try again.');
}

function errorCode(error: unknown): string {
  if (error && typeof error === 'object' && 'code' in error && typeof error.code === 'string') {
    return error.code;
  }
  return '';
}

function errorModelId(error: unknown): string {
  if (error && typeof error === 'object' && 'modelId' in error && typeof error.modelId === 'string') {
    return error.modelId;
  }
  return '';
}
