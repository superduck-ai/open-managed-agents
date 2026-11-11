import { useQuery } from '@tanstack/react-query';
import { listManagedEntities, listMemoryStoreOptions } from '../api';
import type { EntityOption, VaultApiResponse } from '../types';

async function listChannelResources(workspaceId: string) {
  const [memoryPage, vaults] = await Promise.all([
    listMemoryStoreOptions(workspaceId),
    (async () => {
      const rows: EntityOption[] = [];
      let cursor: string | undefined;
      do {
        const page = await listManagedEntities('credential-vaults', workspaceId, cursor);
        rows.push(
          ...(page.data as VaultApiResponse[]).map((vault) => ({
            id: vault.id,
            label: vault.display_name || vault.id,
          })),
        );
        cursor = page.next_page ?? undefined;
      } while (cursor);
      return rows;
    })(),
  ]);
  return {
    vaults,
    memoryStores: memoryPage.data.map((store) => ({ id: store.id, label: store.name || store.id })),
  };
}

export function useChannelResources(workspaceId: string) {
  return useQuery({
    queryKey: ['channel-resource-options', workspaceId],
    queryFn: () => listChannelResources(workspaceId),
  });
}
