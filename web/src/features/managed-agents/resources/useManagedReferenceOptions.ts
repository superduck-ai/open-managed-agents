import { useQuery } from '@tanstack/react-query';
import { useWorkspaceCapabilities } from '@/shared/workspaces/context';
import { listManagedEntities, listMemoryStoreOptions } from '../api';
import type { EntityOption, EnvironmentApiResponse, MemoryStoreApiResponse, VaultApiResponse } from '../types';
import { errorMessage } from '../utils';

const emptyOptions: EntityOption[] = [];

type ReferenceFetchCapabilities = {
  credentialVaults: boolean;
  memoryStores: boolean;
};

async function loadManagedReferenceOptions(workspaceId: string, capabilities: ReferenceFetchCapabilities) {
  const [environmentPage, vaultPage, memoryStorePage] = await Promise.all([
    listManagedEntities('environments', workspaceId),
    capabilities.credentialVaults
      ? listManagedEntities('credential-vaults', workspaceId)
      : Promise.resolve({ data: [] }),
    capabilities.memoryStores ? listMemoryStoreOptions(workspaceId) : Promise.resolve({ data: [] }),
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
  const capabilities = useWorkspaceCapabilities();
  const credentialVaults = capabilities.credential_vaults;
  const memoryStores = capabilities.memory_stores;
  const query = useQuery({
    queryKey: ['managed-reference-options', workspaceId, credentialVaults, memoryStores],
    queryFn: () => loadManagedReferenceOptions(workspaceId, { credentialVaults, memoryStores }),
    enabled,
    retry: false,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
  });
  return {
    environments: query.data?.environments ?? emptyOptions,
    vaults: query.data?.vaults ?? emptyOptions,
    memoryStores: query.data?.memoryStores ?? emptyOptions,
    loading: enabled && query.isPending,
    error: enabled && query.isError && !query.data ? errorMessage(query.error) : null,
    retry: () => void query.refetch(),
  };
}
