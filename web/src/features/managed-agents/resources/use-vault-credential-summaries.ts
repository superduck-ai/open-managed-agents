import { useEffect, useRef, useState } from 'react';
import { listVaultCredentials } from '../api';
import { type I18nMsg } from '../types';
import {
  type VaultCredentialSummaryLoadState,
  vaultCredentialNames,
  vaultCredentialSummaryPresentation,
} from './model';

type SummaryMap = Record<string, VaultCredentialSummaryLoadState>;

/**
 * Lazy-load vault credential name summaries when the picker opens.
 * Callers only need presentationFor(vaultId) — fetch/cache/retry stay inside.
 */
export function useVaultCredentialSummaries({
  workspaceId,
  vaultIds,
  enabled,
  msg,
}: {
  workspaceId: string;
  vaultIds: string[];
  enabled: boolean;
  msg: I18nMsg;
}): {
  presentationFor: (vaultId: string, loadingLabel: string) => { trailing: string; detail: string };
} {
  const [summaries, setSummaries] = useState<SummaryMap>({});
  const summariesRef = useRef(summaries);
  summariesRef.current = summaries;
  const vaultIdsKey = vaultIds.join('\0');

  useEffect(() => {
    if (!enabled || !vaultIdsKey) {
      return;
    }
    const ids = vaultIdsKey.split('\0');
    const pendingIds = ids.filter((vaultId) => {
      const current = summariesRef.current[vaultId];
      return !current || current.status === 'idle' || current.status === 'error';
    });
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
          const page = await listVaultCredentials(vaultId, workspaceId);
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
  }, [enabled, msg, vaultIdsKey, workspaceId]);

  return {
    presentationFor: (vaultId, loadingLabel) =>
      vaultCredentialSummaryPresentation(summaries[vaultId], loadingLabel, msg),
  };
}
