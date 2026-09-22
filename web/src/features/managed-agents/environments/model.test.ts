import { describe, expect, test } from 'bun:test';
import { addPackage, environmentDraftKey, environmentFormBody, environmentFormValues } from './model';
import type { EnvironmentApiResponse } from '../types';

const environment: EnvironmentApiResponse = {
  id: 'env_test',
  type: 'environment',
  name: 'Data',
  description: '',
  scope: 'organization',
  state: 'active',
  created_at: '2026-09-20T00:00:00Z',
  updated_at: '2026-09-20T00:00:00Z',
  archived_at: null,
  config: {
    type: 'cloud',
    init_script: 'echo ready',
    environment: { MODE: 'test' },
    packages: { pip: ['pandas==2.0.2'] },
    networking: { type: 'limited', allow_package_managers: true, allowed_hosts: ['example.com'] },
  },
  metadata: { team: 'data', remove: 'old' },
};

describe('environment form payload', () => {
  test('patches metadata from the draft baseline without overwriting concurrent changes', () => {
    const initial = environmentFormValues(environment);
    const values = { ...initial, description: 'Updated description' };
    const refreshed = { ...environment, metadata: { team: 'other', remove: 'new value', added: 'new key' } };
    expect(environmentFormBody(values, refreshed, initial).metadata).toEqual({});
    values.metadataRows = [{ key: 'team', value: 'tools' }];
    expect(environmentFormBody(values, refreshed, initial).metadata).toEqual({ team: 'tools', remove: null });
  });
  test('preserves unedited cloud settings and sends metadata deletions', () => {
    const values = environmentFormValues(environment);
    values.name = ' Data tools ';
    values.metadataRows = [{ key: 'team', value: 'tools' }];
    const body = environmentFormBody(values, environment, environmentFormValues(environment));
    expect(body.name).toBe('Data tools');
    expect(body.config).toMatchObject({
      init_script: 'echo ready',
      environment: { MODE: 'test' },
      networking: { type: 'limited', allow_package_managers: true, allowed_hosts: ['example.com'] },
    });
    expect(body.metadata).toEqual({ team: 'tools', remove: null });
  });
  test('creation stays cloud and updates preserve the existing hosting type', () => {
    const values = { ...environmentFormValues(), hosting: 'self_hosted' as const };
    expect(environmentFormBody(values).config.type).toBe('cloud');
    const existing = { ...environment, config: { type: 'self_hosted' } };
    expect(environmentFormBody(environmentFormValues(existing), existing).config).toEqual({ type: 'self_hosted' });
  });
  test('adds one package at a time and ignores duplicates within a manager', () => {
    const initial = environmentFormValues(environment);
    expect(addPackage(initial, 'pip', 'pandas==2.0.2')).toBe(initial);
    const values = addPackage(initial, 'pip', 'requests==2.32.0');
    expect(values.packages).toEqual([
      { manager: 'pip', value: 'pandas==2.0.2' },
      { manager: 'pip', value: 'requests==2.32.0' },
    ]);
    expect(addPackage(values, 'npm', 'requests==2.32.0').packages).toHaveLength(3);
  });
  test('defaults to limited networking and treats blank UI rows as unchanged', () => {
    const initial = environmentFormValues();
    expect(initial.networkType).toBe('limited');
    expect(initial.allowPackageManagers).toBe(false);
    expect(environmentDraftKey({ ...initial, metadataRows: [{ key: '', value: '' }] })).toBe(
      environmentDraftKey(initial),
    );
  });
});
