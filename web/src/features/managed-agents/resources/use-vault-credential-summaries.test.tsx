import { afterEach, describe, expect, mock, test } from 'bun:test';

import { resetTestDom } from '../../../test/setup';

const testingLibrary = await import('@testing-library/react');
const { act, cleanup, renderHook, waitFor } = testingLibrary;

import { type I18nMsg } from '../types';
import { useVaultCredentialSummaries } from './use-vault-credential-summaries';

const msgFallback: I18nMsg = ((_key, fallback) => fallback) as I18nMsg;

afterEach(() => {
  cleanup();
});

describe('useVaultCredentialSummaries', () => {
  test('recovers when an in-flight load is cancelled by a dependency change', async () => {
    resetTestDom('https://oma.duck.ai/resources');
    let releaseFirst: (() => void) | undefined;
    const firstGate = new Promise<void>((resolve) => {
      releaseFirst = resolve;
    });
    const listCredentials = mock(async (vaultId: string) => {
      if (vaultId === 'vlt_a') {
        await firstGate;
      }
      return {
        data: [
          {
            id: `vcrd_${vaultId}`,
            type: 'vault_credential' as const,
            vault_id: vaultId,
            display_name: `Name ${vaultId}`,
            auth: { type: 'static_bearer' },
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
            archived_at: null,
          },
        ],
      };
    });

    const { result, rerender } = renderHook(
      ({ workspaceId }) =>
        useVaultCredentialSummaries({
          workspaceId,
          vaultIds: ['vlt_a'],
          enabled: true,
          msg: msgFallback,
          listCredentials,
        }),
      { initialProps: { workspaceId: 'ws_1' } },
    );

    await waitFor(() => {
      expect(result.current.presentationFor('vlt_a', 'Loading...').trailing).toBe('Loading...');
    });

    rerender({ workspaceId: 'ws_2' });
    releaseFirst?.();

    await waitFor(() => {
      expect(result.current.presentationFor('vlt_a', 'Loading...').trailing).toBe('Name vlt_a');
    });
    expect(listCredentials.mock.calls.length).toBeGreaterThanOrEqual(2);
  });

  test('does not restart loads when vaultIds only change order', async () => {
    resetTestDom('https://oma.duck.ai/resources');
    const listCredentials = mock(async (vaultId: string) => ({
      data: [
        {
          id: `vcrd_${vaultId}`,
          type: 'vault_credential' as const,
          vault_id: vaultId,
          display_name: vaultId,
          auth: { type: 'static_bearer' },
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
          archived_at: null,
        },
      ],
    }));

    const { result, rerender } = renderHook(
      ({ vaultIds }) =>
        useVaultCredentialSummaries({
          workspaceId: 'ws_1',
          vaultIds,
          enabled: true,
          msg: msgFallback,
          listCredentials,
        }),
      { initialProps: { vaultIds: ['vlt_b', 'vlt_a'] } },
    );

    await waitFor(() => {
      expect(result.current.presentationFor('vlt_a', 'Loading...').trailing).toBe('vlt_a');
      expect(result.current.presentationFor('vlt_b', 'Loading...').trailing).toBe('vlt_b');
    });
    const callsAfterFirstLoad = listCredentials.mock.calls.length;

    await act(async () => {
      rerender({ vaultIds: ['vlt_a', 'vlt_b'] });
    });

    expect(listCredentials.mock.calls.length).toBe(callsAfterFirstLoad);
    expect(result.current.presentationFor('vlt_a', 'Loading...').trailing).toBe('vlt_a');
  });

  test('retries loading summaries after the picker closes mid-fetch and reopens', async () => {
    resetTestDom('https://oma.duck.ai/resources');
    let releaseFetch: (() => void) | undefined;
    const fetchGate = new Promise<void>((resolve) => {
      releaseFetch = resolve;
    });
    const listCredentials = mock(async (vaultId: string) => {
      await fetchGate;
      return {
        data: [
          {
            id: `vcrd_${vaultId}`,
            type: 'vault_credential' as const,
            vault_id: vaultId,
            display_name: `Name ${vaultId}`,
            auth: { type: 'static_bearer' },
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
            archived_at: null,
          },
        ],
      };
    });

    const { result, rerender } = renderHook(
      ({ enabled }) =>
        useVaultCredentialSummaries({
          workspaceId: 'ws_1',
          vaultIds: ['vlt_a'],
          enabled,
          msg: msgFallback,
          listCredentials,
        }),
      { initialProps: { enabled: true } },
    );

    await waitFor(() => {
      expect(result.current.presentationFor('vlt_a', 'Loading...').trailing).toBe('Loading...');
    });
    expect(listCredentials.mock.calls.length).toBe(1);

    await act(async () => {
      rerender({ enabled: false });
    });
    releaseFetch?.();

    await act(async () => {
      rerender({ enabled: true });
    });

    await waitFor(() => {
      expect(result.current.presentationFor('vlt_a', 'Loading...').trailing).toBe('Name vlt_a');
    });
    expect(listCredentials.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
});
