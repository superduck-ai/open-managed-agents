import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useMemo } from 'react';
import { loadModelCatalog, refreshModelCatalog } from '../../shared/api/model-catalog';
import { catalogModelIDs, resolveCatalogDefaultModelID } from './model';

export function modelCatalogQueryKey(orgUuid?: string) {
  return ['model-catalog', orgUuid ?? ''] as const;
}

export function useModelCatalog(orgUuid?: string) {
  const query = useQuery({
    queryKey: modelCatalogQueryKey(orgUuid),
    queryFn: () => loadModelCatalog(orgUuid as string),
    enabled: Boolean(orgUuid),
    staleTime: 30_000,
  });
  const models = useMemo(() => query.data?.models ?? [], [query.data]);
  const modelIDs = useMemo(() => catalogModelIDs(models), [models]);

  return {
    ...query,
    models,
    modelIDs,
    defaultModelID: query.data ? resolveCatalogDefaultModelID(query.data) : '',
    catalogState: query.data?.model_catalog,
  };
}

export function useRefreshModelCatalog(orgUuid?: string, csrfToken?: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => {
      if (!orgUuid || !csrfToken) {
        throw new Error('Organization and CSRF token are required to refresh the model catalog.');
      }
      return refreshModelCatalog(orgUuid, csrfToken);
    },
    onSuccess: (catalog) => queryClient.setQueryData(modelCatalogQueryKey(orgUuid), catalog),
  });
}
