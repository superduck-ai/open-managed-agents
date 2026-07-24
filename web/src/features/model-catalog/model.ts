export type ModelCatalogModel = {
  model_name: string;
  display_name?: string;
  description?: string;
  max_tokens?: number;
  max_output_tokens?: number;
  supports_thinking?: boolean;
  supports_tool_use?: boolean;
  [key: string]: unknown;
};

export type ModelCatalogResponse = {
  default_prompt_settings?: {
    model_name?: string;
  };
  models?: ModelCatalogModel[];
  model_catalog?: {
    stale?: boolean;
    default_available?: boolean;
    last_attempt_at?: string;
    last_success_at?: string;
  };
};

export function catalogModelIDs(models: readonly ModelCatalogModel[]) {
  return Array.from(new Set(models.map((model) => model.model_name.trim()).filter((modelID) => Boolean(modelID))));
}

export function isCatalogModelID(modelID: string, models: readonly ModelCatalogModel[]) {
  const normalizedModelID = modelID.trim();
  return Boolean(normalizedModelID) && models.some((model) => model.model_name === normalizedModelID);
}

export function resolveCatalogDefaultModelID(catalog: ModelCatalogResponse) {
  if (!catalog.model_catalog?.default_available) {
    return '';
  }
  const modelID = catalog.default_prompt_settings?.model_name?.trim() ?? '';
  return isCatalogModelID(modelID, catalog.models ?? []) ? modelID : '';
}

export function modelCatalogDisplayName(model: ModelCatalogModel) {
  return model.display_name?.trim() || model.model_name;
}
