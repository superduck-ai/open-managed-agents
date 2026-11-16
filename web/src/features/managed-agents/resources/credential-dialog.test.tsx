import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { toast } from '../../../shared/ui/sonner';
import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import { type ReactNode } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { resetTestDom } from '../../../test/setup';
import { AuthContext } from '../../../shared/auth/context';
import { resetMcpDirectoryCacheForTests } from '../agents/tools/api';
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

test('finds workspace-defined MCP servers when picking a credential server', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  resetMcpDirectoryCacheForTests();
  globalThis.fetch = mock(async (input) => {
    if (String(input).includes('/mcp_servers')) {
      return Response.json({
        data: [
          {
            id: 'mcp_toolbox',
            type: 'mcp_server',
            name: 'toolbox',
            transport_type: 'url',
            url: 'https://mcp.example.com/chatdata/chatdata',
            created_at: '2026-11-09T00:00:00Z',
            updated_at: '2026-11-09T00:00:00Z',
          },
        ],
        next_page: null,
      });
    }
    return Response.json({ data: [] });
  });
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={() => {}}
        onSubmit={async () => {}}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'MCP server' }));
  const search = await screen.findByPlaceholderText('Search MCP directory...');
  fireEvent.change(search, { target: { value: 'toolbox' } });
  fireEvent.click(await screen.findByText('https://mcp.example.com/chatdata/chatdata'));
  expect(within(dialog).getByDisplayValue('toolbox (https://mcp.example.com/chatdata/chatdata)')).toBeTruthy();
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
  fireEvent.click(screen.getByRole('button', { name: 'Access token (optional)' }));
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

test.each([false, true])('creates an ENV OAuth credential through the authorization popup', async (conflict) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const popup = {
    closed: false,
    location: { href: '' },
    close: mock(() => {
      popup.closed = true;
    }),
  };
  window.open = mock(() => popup as unknown as Window);
  const errorToast = spyOn(toast, 'error').mockImplementation(() => 'toast_fixture');
  const writes: string[] = [];
  globalThis.fetch = mock(async (input, init) => {
    const url = String(input);
    if (url.includes('/env-oauth/providers')) {
      return Response.json([
        {
          id: 'builtin:feishu',
          name: 'Feishu',
          builtin: true,
          enabled: true,
          configured: true,
          authorize_endpoint: 'https://accounts.feishu.cn/open-apis/authen/v1/authorize',
          token_endpoint: 'https://open.feishu.cn/open-apis/authen/v2/oauth/token',
          scope: 'offline_access',
          default_secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
          default_allowed_hosts: ['open.feishu.cn', 'accounts.feishu.cn'],
        },
      ]);
    }
    if (url.includes('/env-oauth/vault-auth/start')) {
      writes.push(String(init?.body));
      if (conflict) return Response.json({ error_code: 'credential_key_conflict' }, { status: 409 });
      return Response.json({ oauth_flow_id: 'flow_env', authorize_url: 'https://accounts.feishu.cn/authorize' });
    }
    return Response.json({});
  }) as typeof fetch;
  const onSubmit = mock(async () => {});
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={() => {}}
        onSubmit={onSubmit}
        onOAuthComplete={() => {}}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Type' }));
  fireEvent.click(await screen.findByText('ENV OAuth'));
  fireEvent.click(await within(dialog).findByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(await screen.findByText('Feishu'));
  expect((within(dialog).getByLabelText('Variable name') as HTMLInputElement).value).toBe(
    'LARKSUITE_CLI_USER_ACCESS_TOKEN',
  );
  fireEvent.click(within(dialog).getByRole('checkbox'));
  await act(async () => {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  });
  if (conflict) {
    await waitFor(() =>
      expect(errorToast).toHaveBeenCalledWith(
        'Variable name “LARKSUITE_CLI_USER_ACCESS_TOKEN” already exists in this vault.',
      ),
    );
    expect(popup.close).toHaveBeenCalled();
    expect(screen.getByRole('dialog', { name: 'Add credential' })).toBeTruthy();
    expect((within(dialog).getByLabelText('Variable name') as HTMLInputElement).value).toBe(
      'LARKSUITE_CLI_USER_ACCESS_TOKEN',
    );
    expect(popup.location.href).toBe('');
    errorToast.mockRestore();
    return;
  }
  errorToast.mockRestore();
  await waitFor(() => expect(popup.location.href).toBe('https://accounts.feishu.cn/authorize'));
  expect(writes).toHaveLength(1);
  expect(JSON.parse(writes[0])).toEqual({
    provider_id: 'builtin:feishu',
    vault_id: 'vlt_fixture',
    workspace_id: 'default',
    secret_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
    display_name: 'LARKSUITE_CLI_USER_ACCESS_TOKEN',
    networking: { type: 'limited', allowed_hosts: ['open.feishu.cn', 'accounts.feishu.cn'] },
    redirect_url: 'https://oma.duck.ai/oauth/vault/env-success',
  });
  expect(onSubmit).not.toHaveBeenCalled();
});

test('选择无默认域名的自定义 OAuth 服务方并使用无限制网络', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const popup = { closed: false, location: { href: '' }, close: mock(() => {}) };
  window.open = mock(() => popup as unknown as Window);
  const writes: string[] = [];
  globalThis.fetch = mock(async (input, init) => {
    if (String(input).includes('/env-oauth/providers')) {
      return Response.json([
        {
          id: 'custom:example',
          name: 'Example',
          builtin: false,
          enabled: true,
          configured: true,
          default_secret_name: '',
          default_allowed_hosts: null,
        },
      ]);
    }
    if (String(input).includes('/env-oauth/vault-auth/start')) {
      writes.push(String(init?.body));
      return Response.json({ oauth_flow_id: 'flow_custom', authorize_url: 'https://example.test/authorize' });
    }
    return Response.json({});
  }) as typeof fetch;
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={() => {}}
        onSubmit={async () => {}}
        onOAuthComplete={() => {}}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Type' }));
  fireEvent.click(await screen.findByText('ENV OAuth'));
  fireEvent.click(await within(dialog).findByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(await screen.findByText('Example'));
  fireEvent.change(within(dialog).getByLabelText('Variable name'), { target: { value: 'TOKEN' } });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  expect(within(dialog).getByRole('button', { name: 'Connect' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(within(dialog).getByRole('radio', { name: 'Unrestricted' }));
  await act(async () => {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  });
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(writes[0])).toMatchObject({
    provider_id: 'custom:example',
    secret_name: 'TOKEN',
    networking: { type: 'unrestricted' },
  });
});

test.each(['Feishu', 'Custom'])('未配置服务方 %s 禁用授权但保留手工令牌保存', async (name) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  globalThis.fetch = mock(async () =>
    Response.json([
      {
        id: name === 'Feishu' ? 'builtin:feishu' : 'custom:poap_example',
        name,
        builtin: name === 'Feishu',
        enabled: true,
        configured: false,
        default_secret_name: 'TOKEN',
        default_allowed_hosts: [name === 'Custom' ? 'oauth.example.test' : 'open.feishu.cn'],
      },
    ]),
  ) as typeof fetch;
  const onSubmit = mock(async () => {});
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={() => {}}
        onSubmit={onSubmit}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Type' }));
  fireEvent.click(await screen.findByText('ENV OAuth'));
  fireEvent.click(await within(dialog).findByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(await screen.findByText(name));
  if (name === 'Custom')
    expect((within(dialog).getByLabelText('Variable name') as HTMLInputElement).placeholder).toBe('ACCESS_TOKEN');
  fireEvent.click(within(dialog).getByRole('checkbox'));
  expect(within(dialog).getByRole('button', { name: 'Connect' }).hasAttribute('disabled')).toBe(true);
  expect(
    screen.getByText(
      'Authorization requires client ID and secret. Configure this provider before connecting, or supply a token manually.',
    ),
  ).toBeTruthy();
  expect(screen.queryByRole('link', { name: 'Configure provider' })).toBeNull();
  fireEvent.click(within(dialog).getByRole('button', { name: /Use an existing token/ }));
  fireEvent.change(within(dialog).getByLabelText('Access token'), { target: { value: 'manual-token' } });
  await act(async () => {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add credential' }));
  });
  await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
});

test('ENV OAuth 编辑授权失败后可重试，回调完成后刷新且不提交手工令牌', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const popup = { closed: false, location: { href: '' }, close: mock(() => {}) };
  window.open = mock(() => popup as unknown as Window);
  let attempts = 0;
  const writes: Record<string, unknown>[] = [];
  globalThis.fetch = mock(async (input, init) => {
    if (String(input).includes('/env-oauth/providers')) {
      return Response.json([{ id: 'builtin:feishu', name: 'Feishu', builtin: true, configured: true, enabled: true }]);
    }
    writes.push(JSON.parse(String(init?.body)));
    attempts += 1;
    return attempts === 1
      ? Response.json({ error: { message: '授权服务暂不可用' } }, { status: 503 })
      : Response.json({ oauth_flow_id: 'flow_reauth', authorize_url: 'https://accounts.feishu.cn/authorize' });
  }) as typeof fetch;
  const onSubmit = mock(async () => {});
  const onOAuthComplete = mock(() => {});
  render(
    <AuthHarness>
      <CredentialDialog
        credential={{
          ...credential,
          auth: {
            type: 'env_oauth',
            provider: 'builtin:feishu',
            secret_name: 'TEAM_TOKEN',
            networking: { type: 'limited', allowed_hosts: ['open.feishu.cn'] },
          },
        }}
        vaultId="vlt_fixture"
        workspaceId="default"
        orgUuid="org_fixture"
        onClose={() => {}}
        onSubmit={onSubmit}
        onOAuthComplete={onOAuthComplete}
      />
    </AuthHarness>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Edit credential' });
  const reconnect = within(dialog).getByRole('button', { name: 'Reauthorize' });
  expect(reconnect.hasAttribute('disabled')).toBe(true);
  fireEvent.click(within(dialog).getByRole('checkbox'));
  await waitFor(() => expect(reconnect.hasAttribute('disabled')).toBe(false));
  fireEvent.change(within(dialog).getByLabelText('Allowed hosts'), {
    target: { value: 'api.example.com' },
  });
  fireEvent.click(reconnect);
  expect(within(dialog).getByText('Save network changes before reauthorizing.')).toBeTruthy();
  fireEvent.change(within(dialog).getByLabelText('Allowed hosts'), {
    target: { value: '  open.feishu.cn\n' },
  });
  fireEvent.click(within(dialog).getByRole('radio', { name: 'Unrestricted' }));
  fireEvent.click(reconnect);
  expect(within(dialog).getByText('Save network changes before reauthorizing.')).toBeTruthy();
  expect(window.open).not.toHaveBeenCalled();
  expect(writes).toHaveLength(0);
  expect(within(dialog).getByRole('button', { name: 'Save changes' }).hasAttribute('disabled')).toBe(false);
  fireEvent.click(within(dialog).getByRole('radio', { name: 'Limited' }));
  fireEvent.click(reconnect);
  await within(dialog).findByText('授权服务暂不可用');
  expect(popup.close).toHaveBeenCalledTimes(1);
  fireEvent.click(reconnect);
  await waitFor(() => expect(popup.location.href).toBe('https://accounts.feishu.cn/authorize'));
  expect(writes[1]).toMatchObject({
    credential_id: 'cred_fixture',
    provider_id: 'builtin:feishu',
    secret_name: 'TEAM_TOKEN',
    vault_id: 'vlt_fixture',
  });
  await act(async () => {
    window.dispatchEvent(
      new window.MessageEvent('message', {
        origin: window.location.origin,
        data: {
          type: 'vault_oauth_complete',
          flow_id: 'flow_reauth',
          vault_id: 'vlt_fixture',
          credential_id: 'cred_fixture',
        },
      }),
    );
  });
  expect(onOAuthComplete).toHaveBeenCalledTimes(1);
  expect(onSubmit).not.toHaveBeenCalled();
  expect(dialog.hasAttribute('data-closed')).toBe(true);
});

test('MCP 凭据门禁只展示调用方允许的认证类型', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  globalThis.fetch = mock(async () => Response.json({ data: [], next_page: null })) as typeof fetch;
  render(
    <AuthHarness>
      <CredentialDialog
        vaultId="vlt_fixture"
        workspaceId="default"
        initialMcpServerUrl="https://mcp.example.com/mcp"
        allowedAuthTypes={['static_bearer', 'mcp_oauth']}
        onClose={() => {}}
        onSubmit={async () => {}}
      />
    </AuthHarness>,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'Type' }));
  expect(screen.queryByText('ENV OAuth')).toBeNull();
  expect(screen.queryByText('Environment variable')).toBeNull();
  expect(screen.getByRole('option', { name: 'MCP OAuth' })).toBeTruthy();
  expect(screen.getByRole('option', { name: 'Bearer token' })).toBeTruthy();
});

test.each([false, true])('自定义服务方仅维护四项定义，晚到目录不清除创建结果：%s', async (directoryFails) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  const record = {
    id: 'poap_workspace',
    provider_key: 'custom:poap_workspace',
    name: 'Team OAuth',
    enabled: true,
    configured: false,
    version: 1,
    client_id: '',
    has_client_secret: false,
    authorize_endpoint: 'https://auth.example.test/authorize',
    token_endpoint: 'https://auth.example.test/token',
    scope: 'read',
    default_secret_name: '',
    default_allowed_hosts: [],
  };
  const writes: RequestInit[] = [];
  let completeDirectory: () => void = () => {};
  const directoryPending = new Promise<Response>((resolve, reject) => {
    completeDirectory = () => (directoryFails ? reject(new Error('目录加载失败')) : resolve(Response.json([])));
  });
  let completeCreate: () => void = () => {};
  const createPending = new Promise<void>((resolve) => {
    completeCreate = resolve;
  });
  globalThis.fetch = mock(async (url, options) => {
    if (!String(url).includes('/env-oauth/providers')) return Response.json([]);
    expect(String(url)).toContain('workspace_id=default');
    if (options?.method === 'POST') {
      writes.push(options);
      await createPending;
      return Response.json(record);
    }
    return String(url).includes('/poap_workspace?') ? Response.json(record) : directoryPending;
  }) as typeof fetch;
  const submit = mock(async () => {});
  window.open = mock(() => null) as typeof window.open;
  const queries = new QueryClient();
  render(
    <QueryClientProvider client={queries}>
      <AuthHarness>
        <CredentialDialog
          vaultId="vlt_fixture"
          workspaceId="default"
          orgUuid="org_fixture"
          onClose={() => {}}
          onSubmit={submit}
        />
      </AuthHarness>
    </QueryClientProvider>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Type' }));
  fireEvent.click(await screen.findByText('ENV OAuth'));
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Custom OAuth provider' }));
  let definition = await screen.findByRole('form', { name: 'Custom OAuth provider' });
  expect(document.activeElement).toBe(within(definition).getByLabelText('Name'));
  expect(within(definition).getAllByRole('textbox')).toHaveLength(4);
  expect(within(definition).queryByLabelText('Client ID')).toBeNull();
  expect(within(definition).queryByRole('button', { name: /Client configuration/ })).toBeNull();
  fireEvent.click(within(definition).getByRole('button', { name: 'Cancel' }));
  expect(document.activeElement).toBe(screen.getByPlaceholderText('Search options...'));
  fireEvent.click(screen.getByRole('option', { name: 'Custom OAuth provider' }));
  definition = await screen.findByRole('form', { name: 'Custom OAuth provider' });
  for (const [label, value] of [
    ['Name', 'Team OAuth'],
    ['Authorize endpoint', record.authorize_endpoint],
    ['Token endpoint', record.token_endpoint],
    ['Scope', 'read'],
  ]) {
    fireEvent.change(within(definition).getByLabelText(label === 'Scope' ? /Scope/ : label), { target: { value } });
  }
  fireEvent.click(within(definition).getByRole('button', { name: 'Add' }));
  await within(definition).findByRole('button', { name: 'Saving...' });
  fireEvent.keyDown(within(definition).getByLabelText('Name'), { key: 'Escape', code: 'Escape' });
  fireEvent.pointerDown(within(dialog).getByPlaceholderText('Example credential'));
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'OAuth provider' }));
  expect(screen.getByRole('form', { name: 'Custom OAuth provider' })).toBe(definition);
  await act(async () => completeCreate());
  await waitFor(() => expect(screen.queryByRole('form', { name: 'Custom OAuth provider' })).toBeNull());
  expect(writes).toHaveLength(1);
  expect(new Headers(writes[0].headers).get('X-CSRF-Token')).toBe('csrf_fixture');
  expect(JSON.parse(String(writes[0].body))).toEqual({
    name: 'Team OAuth',
    authorize_endpoint: record.authorize_endpoint,
    token_endpoint: record.token_endpoint,
    scope: 'read',
  });
  fireEvent.change(within(dialog).getByLabelText('Variable name'), { target: { value: 'TEAM_TOKEN' } });
  fireEvent.change(within(dialog).getByLabelText('Allowed hosts'), { target: { value: 'api.example.test' } });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  await act(async () => completeDirectory());
  expect(within(dialog).getByRole('combobox', { name: 'OAuth provider' }).textContent).toContain(
    'Team OAuth (Workspace)',
  );
  expect(within(dialog).getByRole('button', { name: /Client configuration/ }).textContent).toContain('(required)');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  expect(await within(dialog).findByText('Custom OAuth authorization requires a client ID.')).toBeTruthy();
  expect(within(dialog).getByText('Custom OAuth authorization requires a client secret.')).toBeTruthy();
  expect(window.open).not.toHaveBeenCalled();
  expect(submit).not.toHaveBeenCalled();
  queries.clear();
});

test.each([null, 'Client ID', 'Client secret'])('客户端配置在一级凭据表单校验，保存后授权：%s', async (firstLabel) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  let record = {
    id: 'poap_workspace_edit',
    provider_key: 'custom:poap_workspace_edit',
    name: 'Team OAuth',
    enabled: true,
    configured: false,
    version: 1,
    client_id: '',
    has_client_secret: false,
    authorize_endpoint: 'https://auth.example.test/authorize',
    token_endpoint: 'https://auth.example.test/token',
    scope: 'read',
    default_secret_name: '',
    default_allowed_hosts: [],
  };
  const writes: Record<string, unknown>[] = [];
  const starts: Record<string, unknown>[] = [];
  globalThis.fetch = mock(async (url, options) => {
    if (String(url).includes('/vault-auth/start')) {
      starts.push(JSON.parse(String(options?.body)));
      return Response.json({ oauth_flow_id: 'flow_primary', authorize_url: 'https://auth.example.test/authorize' });
    }
    if (!String(url).includes('/env-oauth/providers')) return Response.json([]);
    if (options?.method === 'PATCH') {
      expect(new Headers(options.headers).get('X-CSRF-Token')).toBe('csrf_fixture');
      const input = JSON.parse(String(options.body));
      expect(input.expected_version).toBe(record.version);
      writes.push(input);
      record = {
        ...record,
        client_id: input.client_id ?? record.client_id,
        scope: input.scope ?? record.scope,
        configured: true,
        has_client_secret: true,
        version: record.version + 1,
      };
      return Response.json(record);
    }
    return Response.json(
      String(url).includes('/poap_workspace_edit?')
        ? record
        : [{ ...record, id: record.provider_key, workspace_scoped: true }],
    );
  }) as typeof fetch;
  const popup = { closed: false, location: { href: '' }, close: mock(() => {}) };
  window.open = mock(() => popup) as unknown as typeof window.open;
  const submit = mock(async () => {});
  const queries = new QueryClient();
  render(
    <QueryClientProvider client={queries}>
      <AuthHarness>
        <CredentialDialog
          orgUuid="org_fixture"
          workspaceId="default"
          vaultId="vlt_fixture"
          onClose={() => {}}
          onSubmit={submit}
        />
      </AuthHarness>
    </QueryClientProvider>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Type' }));
  fireEvent.click(await screen.findByText('ENV OAuth'));
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Team OAuth (Workspace)' }));
  fireEvent.change(within(dialog).getByLabelText('Variable name'), { target: { value: 'MY_USER_TOKEN' } });
  fireEvent.change(within(dialog).getByLabelText('Allowed hosts'), { target: { value: 'api.example.test' } });
  fireEvent.click(within(dialog).getByRole('checkbox'));
  if (firstLabel === null) {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    await within(dialog).findByText('Custom OAuth authorization requires a client ID.');
    fireEvent.click(within(dialog).getByRole('button', { name: /Use an existing token/ }));
    fireEvent.change(await within(dialog).findByLabelText('Access token'), { target: { value: 'manual-token' } });
    expect(within(dialog).getByRole('button', { name: /Client configuration/ }).textContent).toContain('(optional)');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Add credential' }));
    await waitFor(() => expect(submit).toHaveBeenCalledTimes(1));
    expect(within(dialog).queryByText('Custom OAuth authorization requires a client ID.')).toBeNull();
    expect(writes).toHaveLength(0);
    expect(starts).toHaveLength(0);
    expect(window.open).not.toHaveBeenCalled();
    queries.clear();
    return;
  }
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  const clientId = await within(dialog).findByLabelText('Client ID');
  const secret = within(dialog).getByLabelText('Client secret');
  await waitFor(() => expect(clientId.hasAttribute('disabled')).toBe(false));
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  expect(clientId.getAttribute('aria-invalid')).toBe('true');
  expect(secret.getAttribute('aria-invalid')).toBe('true');
  expect(document.activeElement).toBe(clientId);
  fireEvent.change(within(dialog).getByLabelText(firstLabel), {
    target: { value: firstLabel === 'Client ID' ? 'workspace-client' : 'workspace-secret' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  expect(window.open).not.toHaveBeenCalled();
  expect(writes).toHaveLength(0);
  const remaining = firstLabel === 'Client ID' ? secret : clientId;
  expect(document.activeElement).toBe(remaining);
  fireEvent.change(remaining, {
    target: { value: firstLabel === 'Client ID' ? 'workspace-secret' : 'workspace-client' },
  });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  await waitFor(() => expect(popup.location.href).toBe('https://auth.example.test/authorize'));
  expect(writes).toEqual([{ client_id: 'workspace-client', client_secret: 'workspace-secret', expected_version: 1 }]);
  expect(starts).toHaveLength(1);
  expect((within(dialog).getByLabelText('Variable name') as HTMLInputElement).value).toBe('MY_USER_TOKEN');
  expect((within(dialog).getByLabelText('Allowed hosts') as HTMLTextAreaElement).value).toBe('api.example.test');
  await act(async () => {
    window.dispatchEvent(
      new window.MessageEvent('message', {
        origin: window.location.origin,
        data: {
          type: 'vault_oauth_complete',
          flow_id: 'flow_primary',
          vault_id: 'vlt_fixture',
          error_code: 'invalid_request',
        },
      }),
    );
  });
  expect((secret as HTMLInputElement).value).toBe('');
  expect((secret as HTMLInputElement).placeholder).toBe('Leave blank to keep the current secret.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Edit' }));
  const editor = await screen.findByRole('form', { name: 'Edit OAuth provider' });
  expect(within(editor).getAllByRole('textbox')).toHaveLength(4);
  expect(within(editor).queryByLabelText('Client ID')).toBeNull();
  fireEvent.change(within(editor).getByLabelText(/Scope/), { target: { value: 'updated' } });
  fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
  await waitFor(() => expect(screen.queryByRole('form', { name: 'Edit OAuth provider' })).toBeNull());
  expect(writes[1]).not.toHaveProperty('client_secret');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
  await waitFor(() => expect(starts).toHaveLength(2));
  expect(writes).toHaveLength(2);
  expect(submit).not.toHaveBeenCalled();
  queries.clear();
});

test.each(['clients', 'definition'])('版本冲突刷新后保留客户端输入和凭据草稿，可手动重试：%s', async (target) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/vaults/example');
  let record = {
    id: 'poap_conflict',
    provider_key: 'custom:poap_conflict',
    name: 'Team OAuth',
    enabled: true,
    configured: true,
    version: 1,
    client_id: 'original-client',
    has_client_secret: true,
    authorize_endpoint: 'https://auth.example.test/authorize',
    token_endpoint: 'https://auth.example.test/token',
    scope: 'read',
    default_secret_name: '',
    default_allowed_hosts: [],
  };
  const updates: Record<string, unknown>[] = [];
  const starts: Record<string, unknown>[] = [];
  let details = 0;
  globalThis.fetch = mock(async (url, options) => {
    if (String(url).includes('/vault-auth/start')) {
      starts.push(JSON.parse(String(options?.body)));
      return Response.json({ oauth_flow_id: 'flow_conflict', authorize_url: 'https://auth.example.test/authorize' });
    }
    if (!String(url).includes('/env-oauth/providers')) return Response.json([]);
    if (options?.method === 'PATCH') {
      const input = JSON.parse(String(options.body));
      updates.push(input);
      if (updates.length === 1) {
        record = { ...record, version: 2, client_id: 'remote-client', scope: 'remote-scope' };
        return Response.json(
          { error: { code: 'oauth_provider_version_conflict', message: 'conflict' } },
          { status: 409 },
        );
      }
      expect(input.expected_version).toBe(2);
      record = {
        ...record,
        name: input.name ?? record.name,
        client_id: input.client_id ?? record.client_id,
        scope: input.scope ?? record.scope,
        version: 3,
      };
      return Response.json(record);
    }
    if (String(url).includes('/poap_conflict?')) {
      details++;
      return Response.json(record);
    }
    return Response.json([{ ...record, id: record.provider_key, workspace_scoped: true }]);
  }) as typeof fetch;
  const popup = { closed: false, location: { href: '' }, close: mock(() => {}) };
  window.open = mock(() => popup) as unknown as typeof window.open;
  const queries = new QueryClient();
  const submit = mock(async () => {});
  render(
    <QueryClientProvider client={queries}>
      <AuthHarness>
        <CredentialDialog
          orgUuid="org_fixture"
          workspaceId="default"
          vaultId="vlt_fixture"
          onClose={() => {}}
          onSubmit={submit}
        />
      </AuthHarness>
    </QueryClientProvider>,
  );
  const dialog = screen.getByRole('dialog', { name: 'Add credential' });
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'Type' }));
  fireEvent.click(await screen.findByText('ENV OAuth'));
  fireEvent.click(within(dialog).getByRole('combobox', { name: 'OAuth provider' }));
  fireEvent.click(await screen.findByRole('option', { name: 'Team OAuth (Workspace)' }));
  fireEvent.change(within(dialog).getByPlaceholderText('Example credential'), {
    target: { value: 'Credential draft' },
  });
  fireEvent.change(within(dialog).getByLabelText('Variable name'), { target: { value: 'MY_TOKEN' } });
  fireEvent.click(within(dialog).getByRole('radio', { name: 'Unrestricted' }));
  fireEvent.click(within(dialog).getByRole('checkbox'));
  fireEvent.click(within(dialog).getByRole('button', { name: /Client configuration/ }));
  const clientId = await within(dialog).findByLabelText('Client ID');
  await waitFor(() => expect((clientId as HTMLInputElement).value).toBe('original-client'));
  const secret = within(dialog).getByLabelText('Client secret');
  fireEvent.change(secret, { target: { value: 'draft-secret' } });
  if (target === 'clients') {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    await within(dialog).findByText(
      'The provider configuration changed. Your input is preserved; review it and try again.',
    );
  } else {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Edit' }));
    const editor = await screen.findByRole('form', { name: 'Edit OAuth provider' });
    fireEvent.change(within(editor).getByLabelText('Name'), { target: { value: 'Provider draft' } });
    fireEvent.change(within(editor).getByLabelText(/Scope/), { target: { value: 'draft-scope' } });
    fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
    await within(editor).findByText(
      'The provider configuration changed. Your input is preserved; review it and try again.',
    );
    expect((within(editor).getByLabelText('Name') as HTMLInputElement).value).toBe('Provider draft');
    expect((within(editor).getByLabelText(/Scope/) as HTMLInputElement).value).toBe('draft-scope');
  }
  await waitFor(() => expect(details).toBe(2));
  expect(updates).toHaveLength(1);
  expect(starts).toHaveLength(0);
  expect((clientId as HTMLInputElement).value).toBe('original-client');
  expect((secret as HTMLInputElement).value).toBe('draft-secret');
  expect((within(dialog).getByPlaceholderText('Example credential') as HTMLInputElement).value).toBe(
    'Credential draft',
  );
  expect((within(dialog).getByLabelText('Variable name') as HTMLInputElement).value).toBe('MY_TOKEN');
  expect(within(dialog).getByRole('radio', { name: 'Unrestricted' }).getAttribute('aria-checked')).toBe('true');
  if (target === 'clients') {
    fireEvent.click(within(dialog).getByRole('button', { name: 'Connect' }));
    await waitFor(() => expect(starts).toHaveLength(1));
    expect(updates[1]).toMatchObject({
      expected_version: 2,
      client_id: 'original-client',
      client_secret: 'draft-secret',
    });
  } else {
    const editor = screen.getByRole('form', { name: 'Edit OAuth provider' });
    await waitFor(() =>
      expect(within(editor).getByRole('button', { name: 'Save' }).hasAttribute('disabled')).toBe(false),
    );
    fireEvent.click(within(editor).getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('form', { name: 'Edit OAuth provider' })).toBeNull());
    expect(updates[1]).toMatchObject({ expected_version: 2, name: 'Provider draft', scope: 'draft-scope' });
    expect(updates[1]).not.toHaveProperty('client_secret');
  }
  expect(submit).not.toHaveBeenCalled();
  queries.clear();
});
