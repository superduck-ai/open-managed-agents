import { useQuery } from '@tanstack/react-query';
import { listManagedEntities, listMemoryStoreOptions } from '../api';
import type { EntityOption, EnvironmentApiResponse, MemoryStoreApiResponse, VaultApiResponse } from '../types';
import { errorMessage } from '../utils';

const emptyOptions: EntityOption[] = [];

async function loadManagedReferenceOptions(workspaceId: string) {
  const [environmentPage, vaultPage, memoryStorePage] = await Promise.all([
    listManagedEntities('environments', workspaceId),
    listManagedEntities('credential-vaults', workspaceId),
    listMemoryStoreOptions(workspaceId),
  ]);
  return {
    environments: ((environmentPage.data ?? []) as EnvironmentApiResponse[]).map((environment) => ({
      id: environment.id,
      label: environment.name || environment.id,
      secondary: environment.id,
    })),
    vaults: ((vaultPage.data ?? []) as VaultApiResponse[]).map((vault) => ({
      id: vault.id,
      label: vault.display_name || vault.id,
      createdAt: vault.created_at,
    })),
    memoryStores: ((memoryStorePage.data ?? []) as MemoryStoreApiResponse[]).map((store) => ({
      id: store.id,
      label: store.name || store.id,
      secondary: store.id,
    })),
  };
}

export function useManagedReferenceOptions(workspaceId: string, enabled = true) {
  const query = useQuery({
    queryKey: ['managed-reference-options', workspaceId],
    queryFn: () => loadManagedReferenceOptions(workspaceId),
    enabled,
    retry: false,
  });
  return {
    environments: query.data?.environments ?? emptyOptions,
    vaults: query.data?.vaults ?? emptyOptions,
    memoryStores: query.data?.memoryStores ?? emptyOptions,
    loading: enabled && query.isPending,
    error: enabled && query.isError ? errorMessage(query.error) : null,
    retry: () => void query.refetch(),
  };
}
