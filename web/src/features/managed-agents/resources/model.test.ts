import { describe, expect, test } from 'bun:test';

import {
  type CredentialFormValues,
  type DeploymentApiResponse,
  type EnvironmentApiResponse,
  type EnvironmentEditValues,
} from '../types';
import {
  credentialAuthBody,
  credentialFormReady,
  credentialFormValues,
  emptyCredentialFormValues,
  environmentConfigBody,
  environmentEditValues,
  statusPillTone,
} from './model';

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
    expect(credentialAuthBody(oauthValues(), true)).toEqual({
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
        true,
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
    expect(credentialAuthBody(oauthValues({ token: '' }), false)).toEqual({ type: 'mcp_oauth' });
    expect(credentialAuthBody(oauthValues({ token: 'rotated' }), false)).toEqual({
      type: 'mcp_oauth',
      access_token: 'rotated',
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
});
