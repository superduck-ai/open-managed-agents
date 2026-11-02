import '../../../test/setup';
import { afterEach, expect, mock, test } from 'bun:test';
import { useState } from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { resetTestDom } from '../../../test/setup';
import { AuthContext } from '../../../shared/auth/context';
import { I18nProvider } from '../../../shared/i18n';
import { defaultWorkspace } from '../../../shared/workspaces/api';
import { WorkspaceContext } from '../../../shared/workspaces/context';
import { type VaultApiResponse, type VaultCredentialApiResponse } from '../types';
import { VaultCredentialsPanel } from './vault-credentials-panel';

const originalFetch = globalThis.fetch;
const originalClipboard = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
const vault: VaultApiResponse = {
  id: 'vlt_example',
  type: 'vault',
  display_name: 'Example vault',
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
  archived_at: null,
};
const credential: VaultCredentialApiResponse = {
  id: 'cred_example_abcdefghijklmnopqrstuvwxyz012345',
  type: 'vault_credential',
  vault_id: vault.id,
  display_name: 'Research credential with a long name',
  created_at: vault.created_at,
  updated_at: vault.updated_at,
  archived_at: null,
  auth: { type: 'static_bearer', mcp_server_url: 'https://mcp.example.test/mcp' },
};

function PanelHarness({ edit = false, onRefresh }: { edit?: boolean; onRefresh?: () => void }) {
  const [refreshKey, setRefreshKey] = useState(0);
  return (
    <AuthContext.Provider
      value={{
        account: null,
        status: 'authenticated',
        csrfToken: 'csrf_fixture',
        refresh: async () => undefined,
        logout: async () => {},
      }}
    >
      <I18nProvider initialLocale="en">
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'org_example',
            activeWorkspace: defaultWorkspace,
            activeWorkspaceId: defaultWorkspace.id,
            workspaces: [defaultWorkspace],
            isLoading: false,
            error: null,
            selectWorkspace: () => {},
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          <VaultCredentialsPanel
            dialog={edit ? { mode: 'edit', credential } : undefined}
            onDialogChange={edit ? () => {} : undefined}
            vault={vault}
            workspaceId={defaultWorkspace.id}
            refreshKey={refreshKey}
            onRefresh={() => {
              onRefresh?.();
              setRefreshKey((current) => current + 1);
            }}
          />
        </WorkspaceContext.Provider>
      </I18nProvider>
    </AuthContext.Provider>
  );
}

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  if (originalClipboard) Object.defineProperty(navigator, 'clipboard', originalClipboard);
  else Reflect.deleteProperty(navigator, 'clipboard');
});

test('copies the full credential identifier while retaining compact display and the full name', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/vlt_example');
  const requests: string[] = [];
  globalThis.fetch = mock(async (_input, init) => {
    requests.push(init?.method ?? 'GET');
    return Response.json({ data: [credential], has_more: false });
  });
  const writeText = mock(async (_value: string) => {});
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
  render(<PanelHarness />);
  fireEvent.click(await screen.findByRole('button', { name: `Copy ${credential.id}` }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(credential.id));
  expect(screen.getByTitle(credential.display_name)).toBeTruthy();
  expect(screen.getByTitle(credential.id)).toBeTruthy();
  expect(requests).toEqual(['GET']);
});

test('retries a failed credential directory without showing an empty state or making credential writes', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/vlt_example');
  const requests: string[] = [];
  globalThis.fetch = mock(async (_input, init) => {
    requests.push(init?.method ?? 'GET');
    return requests.length === 1
      ? Response.json({ error: { type: 'api_error', message: 'Directory unavailable' } }, { status: 503 })
      : Response.json({ data: [credential], has_more: false });
  });
  render(<PanelHarness />);
  await screen.findByRole('alert');
  expect(screen.queryByText('No credentials yet')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await screen.findByRole('button', { name: `Copy ${credential.id}` });
  expect(screen.queryByRole('alert')).toBeNull();
  expect(requests).toEqual(['GET', 'GET']);
});

test('refreshes the owning directory only after the saved dialog has entered and completed its exit', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/vlt_example');
  globalThis.fetch = mock(async (_input, init) =>
    Response.json(init?.method === 'POST' ? credential : { data: [credential], has_more: false }),
  );
  const refreshStates: boolean[] = [];
  render(
    <PanelHarness
      edit
      onRefresh={() => {
        refreshStates.push(Boolean(document.querySelector('[data-slot="dialog-content"][data-closed]')));
      }}
    />,
  );
  const dialog = await screen.findByRole('dialog', { name: 'Edit credential' });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }));
  await waitFor(() => expect(refreshStates).toEqual([true]));
});
