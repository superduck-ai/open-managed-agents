import { describe, expect, test } from 'bun:test';

import { type EnvironmentApiResponse, type EnvironmentEditValues } from '../types';
import { environmentConfigBody, environmentEditValues } from './model';

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
