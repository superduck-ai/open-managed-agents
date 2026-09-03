import { useEffect, useRef, useState } from 'react';
import { listVaultCredentials } from '../api';
import { type I18nMsg, type VaultCredentialApiResponse } from '../types';
import {
  type VaultCredentialSummaryCache,
  type VaultCredentialSummaryLoadState,
  vaultCredentialNames,
  vaultCredentialSummariesForWorkspace,
  vaultCredentialSummaryCacheKey,
  vaultCredentialSummaryPendingIds,
  vaultCredentialSummaryPresentation,
} from './model';

type ListVaultCredentials = (
  vaultId: string,
  workspaceId: string,
) => Promise<{ data?: VaultCredentialApiResponse[] | null }>;

function patchSummaryCache(
  current: VaultCredentialSummaryCache,
  workspaceId: string,
  patch: Record<string, VaultCredentialSummaryLoadState>,
): VaultCredentialSummaryCache {
  const base = current.workspaceId === workspaceId ? current.summaries : {};
  return {
    workspaceId,
    summaries: { ...base, ...patch },
  };
}

/**
 * Lazy-load vault credential name summaries when the picker opens.
 * Callers only need presentationFor(vaultId) — fetch/cache/retry stay inside.
 * Cache is workspace-scoped: a mismatched workspace reads as empty (loading).
 */
export function useVaultCredentialSummaries({
  workspaceId,
  vaultIds,
  enabled,
  msg,
  listCredentials = listVaultCredentials,
}: {
  workspaceId: string;
  vaultIds: string[];
  enabled: boolean;
  msg: I18nMsg;
  listCredentials?: ListVaultCredentials;
}): {
  presentationFor: (vaultId: string, loadingLabel: string) => { trailing: string; detail: string };
} {
  const [cache, setCache] = useState<VaultCredentialSummaryCache>(() => ({
    workspaceId,
    summaries: {},
  }));
  const activeSummaries = vaultCredentialSummariesForWorkspace(cache, workspaceId);
  const summariesRef = useRef(activeSummaries);
  summariesRef.current = activeSummaries;
  const vaultIdsKey = vaultCredentialSummaryCacheKey(vaultIds);

  useEffect(() => {
    // Drop foreign-workspace rows even while the picker is closed.
    setCache((current) => (current.workspaceId === workspaceId ? current : { workspaceId, summaries: {} }));

    if (!enabled || !vaultIdsKey) {
      return;
    }

    const ids = vaultIdsKey.split('\0');
    const pendingIds = vaultCredentialSummaryPendingIds(ids, summariesRef.current);
    if (!pendingIds.length) {
      return;
    }

    let active = true;
    const loadingPatch: Record<string, VaultCredentialSummaryLoadState> = {};
    for (const vaultId of pendingIds) {
      loadingPatch[vaultId] = { status: 'loading', names: summariesRef.current[vaultId]?.names ?? [] };
    }
    setCache((current) => patchSummaryCache(current, workspaceId, loadingPatch));

    void Promise.all(
      pendingIds.map(async (vaultId) => {
        try {
          const page = await listCredentials(vaultId, workspaceId);
          if (!active) {
            return;
          }
          const names = vaultCredentialNames(page.data ?? [], msg);
          setCache((current) => patchSummaryCache(current, workspaceId, { [vaultId]: { status: 'ready', names } }));
        } catch {
          if (!active) {
            return;
          }
          setCache((current) => patchSummaryCache(current, workspaceId, { [vaultId]: { status: 'error', names: [] } }));
        }
      }),
    );

    return () => {
      active = false;
    };
  }, [enabled, listCredentials, msg, vaultIdsKey, workspaceId]);

  return {
    presentationFor: (vaultId, loadingLabel) =>
      vaultCredentialSummaryPresentation(activeSummaries[vaultId], loadingLabel, msg),
  };
}
