import { consoleApi } from '../../shared/api/client';
import { type ModelCatalogResponse } from './model';

function modelCatalogPath(orgUuid: string) {
  return `/api/organizations/${encodeURIComponent(orgUuid)}/models`;
}

export function loadModelCatalog(orgUuid: string) {
  return consoleApi<ModelCatalogResponse>(modelCatalogPath(orgUuid));
}

export function refreshModelCatalog(orgUuid: string, csrfToken?: string) {
  return consoleApi<ModelCatalogResponse>(`${modelCatalogPath(orgUuid)}/refresh`, {
    method: 'POST',
    body: JSON.stringify({}),
    csrfToken,
  });
}
