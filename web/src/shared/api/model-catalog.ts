import { consoleApi } from './client';

export type ModelCatalogModel = {
  model_name: string;
  display_name?: string;
  description?: string;
  max_tokens?: number;
  max_output_tokens?: number;
  capabilities?: Record<string, unknown>;
  supports_batch?: boolean;
  supports_citations?: boolean;
  supports_code_execution?: boolean;
  supports_context_management?: boolean;
  supports_clear_thinking?: boolean;
  supports_clear_tool_uses?: boolean;
  supports_compact_context?: boolean;
  supports_image_input?: boolean;
  supports_pdf_input?: boolean;
  supports_structured_outputs?: boolean;
  supports_thinking?: boolean;
  supports_thinking_enabled?: boolean;
  supports_auto_thinking?: boolean;
  supports_tool_use?: boolean;
  supported_effort_levels?: string[];
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

function modelCatalogPath(orgUuid: string) {
  return `/api/organizations/${encodeURIComponent(orgUuid)}/models`;
}

export function loadModelCatalog(orgUuid: string) {
  return consoleApi<ModelCatalogResponse>(modelCatalogPath(orgUuid));
}

export function refreshModelCatalog(orgUuid: string, csrfToken: string) {
  return consoleApi<ModelCatalogResponse>(`${modelCatalogPath(orgUuid)}/refresh`, {
    method: 'POST',
    body: JSON.stringify({}),
    csrfToken,
  });
}
