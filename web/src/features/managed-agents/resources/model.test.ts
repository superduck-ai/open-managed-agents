import { describe, expect, test } from 'bun:test';

import {
  type CredentialFormValues,
  type DeploymentApiResponse,
  type EnvironmentApiResponse,
  type EnvironmentEditValues,
  type I18nMsg,
} from '../types';
import {
  credentialAuthBody,
  credentialFormReady,
  credentialFormValues,
  emptyCredentialFormValues,
  environmentConfigBody,
  environmentEditValues,
  patchCredentialFormValues,
  statusPillTone,
  vaultOAuthErrorMessage,
} from './model';

const msgFallback: I18nMsg = ((_key, fallback) => fallback) as I18nMsg;

function editValues(overrides: Partial<EnvironmentEditValues>): EnvironmentEditValues {
  return {
    name: 'env',
    description: '',
    networkType: 'unrestricted',
    allowMcpServers: false,
    allowPackageManagers: false,
    allowedHostsText: '',
    packages: [{ manager: 'pip', value: '' }],
    metadataRows: [{ key: '', value: '' }],
    ...overrides,
  };
}

function apiEnvironment(config: unknown): EnvironmentApiResponse {
  return {
    id: 'env_test',
    archived_at: null,
    config,
    created_at: '2026-07-19T00:00:00Z',
    description: '',
    name: 'env',
    scope: 'workspace',
    state: 'active',
    type: 'environment',
    updated_at: '2026-07-19T00:00:00Z',
  };
}

function apiDeployment(): DeploymentApiResponse {
  return {
    id: 'deployment_test',
    agent: 'agent_test',
    archived_at: null,
    created_at: '2026-07-19T00:00:00Z',
    environment_id: 'env_test',
    name: 'deployment',
    status: 'active',
    type: 'deployment',
    updated_at: '2026-07-19T00:00:00Z',
  };
}

describe('statusPillTone', () => {
  test('keeps deployment active and archived resource statuses neutral', () => {
    expect(statusPillTone('deployments', apiDeployment())).toBe('neutral');
    expect(statusPillTone('environments', { ...apiEnvironment({ type: 'cloud' }), archived_at: '2026-07-20' })).toBe(
      'neutral',
    );
  });

  test('uses success for active resources', () => {
    expect(statusPillTone('environments', apiEnvironment({ type: 'cloud' }))).toBe('success');
  });
});

describe('environmentConfigBody limited networking', () => {
  test('normalizes messy allowed hosts text into a deduped ordered array', () => {
    const body = environmentConfigBody(
      editValues({
        networkType: 'limited',
        allowMcpServers: true,
        allowPackageManagers: false,
        allowedHostsText: 'api.example.com, *.example.com\n api.example.com \n\nfiles.example.org',
      }),
    );
    expect(body.networking).toEqual({
      type: 'limited',
      allowed_hosts: ['api.example.com', '*.example.com', 'files.example.org'],
      allow_mcp_servers: true,
      allow_package_managers: false,
    });
  });

  test('unrestricted submit drops dormant limited fields', () => {
    const body = environmentConfigBody(
      editValues({
        networkType: 'unrestricted',
        allowMcpServers: true,
        allowPackageManagers: true,
        allowedHostsText: 'api.example.com',
      }),
    );
    expect(body.networking).toEqual({ type: 'unrestricted' });
  });
});

describe('environmentEditValues networking round-trip', () => {
  test('editing a limited environment preserves hosts and switches', () => {
    const entity = apiEnvironment({
      type: 'cloud',
      networking: {
        type: 'limited',
        allowed_hosts: ['api.example.com', '*.example.com'],
        allow_mcp_servers: true,
        allow_package_managers: true,
      },
    });
    const values = environmentEditValues(entity);
    expect(values.networkType).toBe('limited');
    expect(values.allowMcpServers).toBe(true);
    expect(values.allowPackageManagers).toBe(true);
    expect(values.allowedHostsText).toBe('api.example.com\n*.example.com');
    // 回归：编辑后再提交不得清空已有 allowlist。
    const body = environmentConfigBody(values);
    expect(body.networking).toEqual({
      type: 'limited',
      allowed_hosts: ['api.example.com', '*.example.com'],
      allow_mcp_servers: true,
      allow_package_managers: true,
    });
  });

  test('environment without networking defaults to unrestricted with empty limited fields', () => {
    const values = environmentEditValues(apiEnvironment({ type: 'cloud' }));
    expect(values.networkType).toBe('unrestricted');
    expect(values.allowMcpServers).toBe(false);
    expect(values.allowPackageManagers).toBe(false);
    expect(values.allowedHostsText).toBe('');
  });
});

describe('credentialAuthBody environment_variable', () => {
  function envValues(overrides: Partial<CredentialFormValues> = {}): CredentialFormValues {
    return {
      ...emptyCredentialFormValues(),
      displayName: 'Env secret',
      authType: 'environment_variable',
      secretName: 'API_KEY',
      secretValue: 'value',
      networkType: 'limited',
      allowedHostsText: 'api.notion.com, *.example.com\napi.notion.com',
      injectHeader: true,
      injectBody: false,
      ...overrides,
    };
  }

  test('create sends networking, injection_location, and preserves secret_value whitespace', () => {
    expect(
      credentialAuthBody(
        envValues({
          secretName: ' API_KEY ',
          secretValue: '  line-one\nline-two\n',
        }),
        'create',
      ),
    ).toEqual({
      type: 'environment_variable',
      secret_name: 'API_KEY',
      secret_value: '  line-one\nline-two\n',
      networking: {
        type: 'limited',
        allowed_hosts: ['api.notion.com', '*.example.com'],
      },
      injection_location: { header: true, body: false },
    });
  });

  test('unrestricted networking omits allowed_hosts; body injection can be enabled', () => {
    expect(
      credentialAuthBody(
        envValues({
          networkType: 'unrestricted',
          allowedHostsText: 'ignored.example.com',
          injectHeader: true,
          injectBody: true,
        }),
        'create',
      ),
    ).toEqual({
      type: 'environment_variable',
      secret_name: 'API_KEY',
      secret_value: 'value',
      networking: { type: 'unrestricted' },
      injection_location: { header: true, body: true },
    });
  });

  test('update omits blank secret_value but keeps intentional whitespace when provided', () => {
    expect(credentialAuthBody(envValues({ secretValue: '' }), 'update')).toEqual({
      type: 'environment_variable',
      networking: {
        type: 'limited',
        allowed_hosts: ['api.notion.com', '*.example.com'],
      },
      injection_location: { header: true, body: false },
    });
    expect(credentialAuthBody(envValues({ secretValue: '   ' }), 'update')).toEqual({
      type: 'environment_variable',
      networking: {
        type: 'limited',
        allowed_hosts: ['api.notion.com', '*.example.com'],
      },
      injection_location: { header: true, body: false },
    });
    expect(credentialAuthBody(envValues({ secretValue: '  rotated\n' }), 'update')).toEqual({
      type: 'environment_variable',
      networking: {
        type: 'limited',
        allowed_hosts: ['api.notion.com', '*.example.com'],
      },
      injection_location: { header: true, body: false },
      secret_value: '  rotated\n',
    });
  });

  test('credentialFormValues round-trips networking and injection_location', () => {
    expect(
      credentialFormValues({
        id: 'cred_env',
        type: 'vault_credential',
        vault_id: 'vlt_test',
        display_name: 'Notion',
        archived_at: null,
        auth: {
          type: 'environment_variable',
          secret_name: 'NOTION_API_KEY',
          networking: { type: 'limited', allowed_hosts: ['api.notion.com'] },
          injection_location: { header: true, body: true },
        },
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      }),
    ).toMatchObject({
      authType: 'environment_variable',
      secretName: 'NOTION_API_KEY',
      networkType: 'limited',
      allowedHostsText: 'api.notion.com',
      injectHeader: true,
      injectBody: true,
    });
  });

  test('credentialFormReady requires hosts for limited and at least one injection location', () => {
    expect(credentialFormReady(envValues({ displayName: '' }), 'create', true)).toBe(false);
    expect(credentialFormReady(envValues({ allowedHostsText: '' }), 'create', true)).toBe(false);
    expect(credentialFormReady(envValues({ networkType: 'unrestricted', allowedHostsText: '' }), 'create', true)).toBe(
      true,
    );
    expect(credentialFormReady(envValues({ injectHeader: false, injectBody: false }), 'create', true)).toBe(false);
    expect(credentialFormReady(envValues({ injectHeader: false, injectBody: true }), 'create', true)).toBe(true);
    expect(credentialFormReady(envValues({ secretValue: '' }), 'edit', true)).toBe(true);
    expect(credentialFormReady(envValues({ secretName: '', secretValue: 'x' }), 'create', true)).toBe(false);
  });
});

describe('credentialAuthBody mcp_oauth', () => {
  function oauthValues(overrides: Partial<CredentialFormValues> = {}): CredentialFormValues {
    return {
      ...emptyCredentialFormValues(),
      displayName: 'Slack MCP',
      authType: 'mcp_oauth',
      mcpServerUrl: 'https://mcp.example.com/mcp',
      token: 'access-secret',
      ...overrides,
    };
  }

  test('create body includes access_token and optional refresh', () => {
    expect(credentialAuthBody(oauthValues(), 'create')).toEqual({
      type: 'mcp_oauth',
      mcp_server_url: 'https://mcp.example.com/mcp',
      access_token: 'access-secret',
    });
    expect(
      credentialAuthBody(
        oauthValues({
          refreshToken: 'refresh-secret',
          refreshTokenEndpoint: 'https://auth.example.com/token',
          refreshClientId: 'client-123',
          refreshAuthType: 'client_secret_post',
          refreshClientSecret: 'client-secret',
        }),
        'create',
      ),
    ).toEqual({
      type: 'mcp_oauth',
      mcp_server_url: 'https://mcp.example.com/mcp',
      access_token: 'access-secret',
      refresh: {
        token_endpoint: 'https://auth.example.com/token',
        client_id: 'client-123',
        refresh_token: 'refresh-secret',
        token_endpoint_auth: { type: 'client_secret_post', client_secret: 'client-secret' },
      },
    });
  });

  test('update body omits immutable url and empty access token', () => {
    expect(credentialAuthBody(oauthValues({ token: '' }), 'update')).toEqual({ type: 'mcp_oauth' });
    expect(credentialAuthBody(oauthValues({ token: 'rotated' }), 'update')).toEqual({
      type: 'mcp_oauth',
      access_token: 'rotated',
    });
  });

  test('create body trims pasted oauth secrets', () => {
    expect(
      credentialAuthBody(
        oauthValues({
          token: ' access-secret\n',
          refreshToken: ' refresh-secret ',
          refreshTokenEndpoint: ' https://auth.example.com/token ',
          refreshClientId: ' client-123 ',
          refreshAuthType: 'client_secret_basic',
          refreshClientSecret: ' client-secret\n',
        }),
        'create',
      ),
    ).toEqual({
      type: 'mcp_oauth',
      mcp_server_url: 'https://mcp.example.com/mcp',
      access_token: 'access-secret',
      refresh: {
        token_endpoint: 'https://auth.example.com/token',
        client_id: 'client-123',
        refresh_token: 'refresh-secret',
        token_endpoint_auth: { type: 'client_secret_basic', client_secret: 'client-secret' },
      },
    });
  });

  test('credentialFormValues detects mcp_oauth', () => {
    const values = credentialFormValues({
      id: 'vcrd_1',
      vault_id: 'vault_1',
      type: 'vault_credential',
      display_name: 'OAuth',
      created_at: '2026-08-01T00:00:00Z',
      updated_at: '2026-08-01T00:00:00Z',
      archived_at: null,
      metadata: {},
      auth: { type: 'mcp_oauth', mcp_server_url: 'https://mcp.example.com/mcp' },
    });
    expect(values.authType).toBe('mcp_oauth');
    expect(values.mcpServerUrl).toBe('https://mcp.example.com/mcp');
  });

  test('credentialFormReady gates ack, connect, and paste paths', () => {
    expect(credentialFormReady(oauthValues({ token: '' }), 'create', false)).toBe(false);
    expect(credentialFormReady(oauthValues({ token: '' }), 'create', true)).toBe(true);
    expect(credentialFormReady(oauthValues({ token: '', oauthClientSecret: 'secret-only' }), 'create', true)).toBe(
      false,
    );
    expect(credentialFormReady(oauthValues(), 'create', true)).toBe(true);
  });

  test('clearing access token drops refresh fields so connect stays recoverable', () => {
    const withRefresh = oauthValues({
      refreshToken: 'refresh-secret',
      refreshTokenEndpoint: 'https://auth.example.com/token',
      refreshClientId: 'client-123',
      refreshAuthType: 'client_secret_post',
      refreshClientSecret: 'client-secret',
    });
    expect(credentialFormReady(withRefresh, 'create', true)).toBe(true);

    const stuckIfMerged = { ...withRefresh, token: '' };
    expect(credentialFormReady(stuckIfMerged, 'create', true)).toBe(false);

    const cleared = patchCredentialFormValues(withRefresh, { token: '' });
    expect(cleared).toMatchObject({
      token: '',
      refreshToken: '',
      refreshTokenEndpoint: '',
      refreshClientId: '',
      refreshClientSecret: '',
      refreshAuthType: 'none',
    });
    expect(credentialFormReady(cleared, 'create', true)).toBe(true);
  });
});

describe('vaultOAuthErrorMessage', () => {
  test('maps known wire codes to user copy and hides unknown codes', () => {
    expect(vaultOAuthErrorMessage('token_exchange_failed', msgFallback)).toBe(
      'OAuth token exchange failed. Try connecting again.',
    );
    expect(vaultOAuthErrorMessage('already_exists', msgFallback)).toContain('already exists');
    expect(vaultOAuthErrorMessage('oauth_discovery_failed', msgFallback)).toContain('discover');
    expect(vaultOAuthErrorMessage('verification_request_failed', msgFallback)).toContain('verification failed');
    expect(vaultOAuthErrorMessage('mystery_code', msgFallback)).toBe('Could not complete OAuth. Try again.');
    expect(vaultOAuthErrorMessage(' mystery_code ', msgFallback)).toBe('Could not complete OAuth. Try again.');
  });
});
