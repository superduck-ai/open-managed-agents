import { afterEach, expect, mock, test } from 'bun:test';
import { type ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { resetTestDom } from '../../../test/setup';
import { AuthContext } from '../../../shared/auth/context';
import { CredentialDialog } from './dialogs';
import type { VaultCredentialApiResponse } from '../types';

const originalFetch = globalThis.fetch;
const originalOpen = window.open;
const credential: VaultCredentialApiResponse = {
  id: 'cred_fixture',
  type: 'vault_credential',
  display_name: 'Example credential',
  auth: { type: 'static_bearer', mcp_server_url: 'https://mcp.example.com/mcp' },
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
};

function AuthHarness({ children }: { children: ReactNode }) {
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
      {children}
    </AuthContext.Provider>
  );
}

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  window.open = originalOpen;
});

test('blocks dismissal and duplicate saves while preserving the credential draft on failure', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  let failSave: (reason: Error) => void = () => {};
  const onSubmit = mock(
    () =>
      new Promise<void>((_resolve, reject) => {
        failSave = reject;
      }),
  );
  const onClose = mock(() => {});
  render(
    <AuthHarness>
      <CredentialDialog
        credential={credential}
        vaultId="vlt_fixture"
        workspaceId="default"
        onClose={onClose}
        onSubmit={onSubmit}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Edit credential' });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }));
  await waitFor(() =>
    expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true),
  );
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  expect((dialog.querySelector('fieldset') as HTMLFieldSetElement).disabled).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  fireEvent.submit(dialog.querySelector('form')!);
  expect(onSubmit).toHaveBeenCalledTimes(1);
  expect(onClose).toHaveBeenCalledTimes(0);
  await act(async () => failSave(new Error('Sample credential save failure')));
  await within(dialog).findByText('Sample credential save failure');
  expect((within(dialog).getByLabelText('Name') as HTMLInputElement).value).toBe('Example credential');
  expect(within(dialog).getByRole('checkbox').getAttribute('aria-checked')).toBe('true');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(dialog.hasAttribute('data-closed')).toBe(true);
  expect((within(dialog).getByLabelText('Name') as HTMLInputElement).value).toBe('Example credential');
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
});

test('closes through the existing exit lifecycle without saving or clearing the draft', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const onSubmit = mock(async () => {});
  const onClose = mock(() => {});
  render(
    <AuthHarness>
      <CredentialDialog
        credential={credential}
        vaultId="vlt_fixture"
        workspaceId="default"
        onClose={onClose}
        onSubmit={onSubmit}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Edit credential' });
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Unsaved example name' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Close' }));
  expect(dialog.hasAttribute('data-closed')).toBe(true);
  expect((within(dialog).getByLabelText('Name') as HTMLInputElement).value).toBe('Unsaved example name');
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  expect(onSubmit).toHaveBeenCalledTimes(0);
});

test('allows cancellation while waiting for OAuth and closes the simulated popup', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const popup = {
    closed: false,
    location: { href: '' },
    close: mock(() => {
      popup.closed = true;
    }),
  };
  window.open = mock(() => popup as unknown as Window);
  globalThis.fetch = mock(async (_input, init) =>
    Response.json(
      init?.method === 'POST'
        ? { oauth_flow_id: 'flow_fixture', redirect_url: 'https://example.com/authorize' }
        : { data: [] },
    ),
  );
  const onClose = mock(() => {});
  const onSubmit = mock(async () => {});
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={onClose}
        onSubmit={onSubmit}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'MCP server' }));
  fireEvent.click(await screen.findByText('Custom server'));
  const server = within(dialog).getByRole('textbox', { name: 'MCP server' });
  fireEvent.change(server, { target: { value: 'https://mcp.example.com/mcp' } });
  fireEvent.keyDown(server, { key: 'Enter', code: 'Enter' });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  await waitFor(() => expect(popup.location.href).toBe('https://example.com/authorize'));
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  expect(popup.close).toHaveBeenCalledTimes(1);
  expect(dialog.hasAttribute('data-closed')).toBe(true);
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
  expect(onSubmit).toHaveBeenCalledTimes(0);
});

test('keeps OAuth token fields empty on edit and describes preserving the existing token without offering a popup', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const onSubmit = mock(async () => {});
  render(
    <AuthHarness>
      <CredentialDialog
        credential={{
          ...credential,
          auth: {
            type: 'mcp_oauth',
            mcp_server_url: 'https://mcp.example.test/mcp',
            refresh: { token_endpoint: 'https://auth.example.test/token', client_id: 'example-client' },
          },
        }}
        vaultId="vlt_fixture"
        workspaceId="default"
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    </AuthHarness>,
  );
  expect(screen.queryByText('Connect an MCP server with OAuth, or paste an access token.')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Access token Optional' }));
  const input = await screen.findByPlaceholderText('Leave blank to keep the current token');
  expect(input).toHaveProperty('value', '');
  expect(input).toHaveProperty('type', 'password');
  expect(screen.queryByRole('button', { name: 'Connect', exact: true })).toBeNull();
  expect(onSubmit).not.toHaveBeenCalled();
});

test('retains the saved form during exit and closes only when the transition completes', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const onClose = mock(() => {});
  const onSubmit = mock(async () => {});
  render(
    <AuthHarness>
      <CredentialDialog
        credential={credential}
        vaultId="vlt_fixture"
        workspaceId="default"
        onClose={onClose}
        onSubmit={onSubmit}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Edit credential' });
  const nameInput = within(dialog).getByLabelText('Name');
  fireEvent.click(within(dialog).getByRole('checkbox'));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save changes' }));
  await waitFor(() => expect(dialog.hasAttribute('data-closed')).toBe(true));
  expect(onSubmit).toHaveBeenCalledTimes(1);
  expect(nameInput).toHaveProperty('value', 'Example credential');
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
});

test('ignores unrelated OAuth completions and preserves one exit lifecycle for the matching flow', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const popup = { closed: false, location: { href: '' }, close: mock(() => {}) };
  window.open = mock(() => popup as unknown as Window);
  globalThis.fetch = mock(async (_input, init) =>
    Response.json(
      init?.method === 'POST'
        ? { oauth_flow_id: 'flow_fixture', redirect_url: 'https://example.com/authorize' }
        : { data: [] },
    ),
  );
  const onClose = mock(() => {});
  const onOAuthComplete = mock(() => {});
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={onClose}
        onSubmit={async () => {}}
        onOAuthComplete={onOAuthComplete}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'MCP server' }));
  fireEvent.click(await screen.findByText('Custom server'));
  const server = within(dialog).getByRole('textbox', { name: 'MCP server' });
  fireEvent.change(server, { target: { value: 'https://mcp.example.com/mcp' } });
  fireEvent.keyDown(server, { key: 'Enter', code: 'Enter' });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  await waitFor(() => expect(popup.location.href).toBe('https://example.com/authorize'));
  const complete = (flowId: string) =>
    window.dispatchEvent(
      new window.MessageEvent('message', {
        origin: window.location.origin,
        data: { type: 'vault_oauth_complete', vault_id: 'vlt_fixture', flow_id: flowId },
      }),
    );
  await act(async () => {
    complete('other_flow');
  });
  expect(onOAuthComplete).not.toHaveBeenCalled();
  await act(async () => {
    complete('flow_fixture');
    complete('flow_fixture');
  });
  expect(dialog.hasAttribute('data-closed')).toBe(true);
  expect(onOAuthComplete).toHaveBeenCalledTimes(1);
  await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
});
