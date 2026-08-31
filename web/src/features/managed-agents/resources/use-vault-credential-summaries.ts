import { useEffect, useRef, useState } from 'react';
import { listVaultCredentials } from '../api';
import { type I18nMsg, type VaultCredentialApiResponse } from '../types';
import {
  type VaultCredentialSummaryLoadState,
  vaultCredentialNames,
  vaultCredentialSummaryCacheKey,
  vaultCredentialSummaryPendingIds,
  vaultCredentialSummaryPresentation,
} from './model';

type SummaryMap = Record<string, VaultCredentialSummaryLoadState>;

type ListVaultCredentials = (
  vaultId: string,
  workspaceId: string,
) => Promise<{ data?: VaultCredentialApiResponse[] | null }>;

/**
 * Lazy-load vault credential name summaries when the picker opens.
 * Callers only need presentationFor(vaultId) — fetch/cache/retry stay inside.
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
  const [summaries, setSummaries] = useState<SummaryMap>({});
  const summariesRef = useRef(summaries);
  summariesRef.current = summaries;
  const workspaceIdRef = useRef(workspaceId);
  const vaultIdsKey = vaultCredentialSummaryCacheKey(vaultIds);

  useEffect(() => {
    if (!enabled || !vaultIdsKey) {
      return;
    }
    if (workspaceIdRef.current !== workspaceId) {
      workspaceIdRef.current = workspaceId;
      summariesRef.current = {};
      setSummaries({});
    }

    const ids = vaultIdsKey.split('\0');
    const pendingIds = vaultCredentialSummaryPendingIds(ids, summariesRef.current);
    if (!pendingIds.length) {
      return;
    }

    let active = true;
    setSummaries((current) => {
      const next = { ...current };
      for (const vaultId of pendingIds) {
        next[vaultId] = { status: 'loading', names: current[vaultId]?.names ?? [] };
      }
      return next;
    });

    void Promise.all(
      pendingIds.map(async (vaultId) => {
        try {
          const page = await listCredentials(vaultId, workspaceId);
          if (!active) {
            return;
          }
          const names = vaultCredentialNames(page.data ?? [], msg);
          setSummaries((current) => ({
            ...current,
            [vaultId]: { status: 'ready', names },
          }));
        } catch {
          if (!active) {
            return;
          }
          setSummaries((current) => ({
            ...current,
            [vaultId]: { status: 'error', names: [] },
          }));
        }
      }),
    );

    return () => {
      active = false;
    };
  }, [enabled, listCredentials, msg, vaultIdsKey, workspaceId]);

  return {
    presentationFor: (vaultId, loadingLabel) =>
      vaultCredentialSummaryPresentation(summaries[vaultId], loadingLabel, msg),
  };
}
