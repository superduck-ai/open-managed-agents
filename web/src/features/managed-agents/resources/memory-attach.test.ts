import { describe, expect, test } from 'bun:test';

import { createManagedEntityBody, updateManagedEntityBody } from '../api';
import type { DeploymentApiResponse, ManagedEntityFormValues, MemoryAttachFormValue } from '../types';
import {
  areMemoryAttachesValid,
  emptyMemoryAttach,
  entityMemoryAttaches,
  MAX_MEMORY_ATTACH_INSTRUCTIONS,
  memoryAttachHasForbiddenClientFields,
  memoryAttachResources,
  memoryInstructionsCodePointCount,
  syncMemoryAttaches,
} from './memory-attach';

function formValues(overrides: Partial<ManagedEntityFormValues> = {}): ManagedEntityFormValues {
  return {
    name: 'Nightly inbox triage',
    description: '',
    agentId: 'agent_option123456',
    environmentId: 'env_option123456',
    initialMessage: 'Summarize support tickets.',
    triggerType: 'manual',
    cronExpression: '0 9 * * 1',
    timezone: 'UTC',
    vaultIds: ['vlt_one123456'],
    memoryAttaches: [],
    fileResources: [],
    ...overrides,
  };
}

function attach(overrides: Partial<MemoryAttachFormValue> = {}): MemoryAttachFormValue {
  return {
    memoryStoreId: 'memstore_one123456',
    access: 'read_write',
    instructions: '有新偏好就更新',
    ...overrides,
  };
}

describe('memory attach packing', () => {
  test('rejects instructions above 500 Unicode code points', () => {
    expect(memoryInstructionsCodePointCount('😀'.repeat(500))).toBe(500);
    expect(memoryInstructionsCodePointCount('😀'.repeat(501))).toBe(501);
    expect(areMemoryAttachesValid([attach({ instructions: 'a'.repeat(501) })])).toBe(false);
    expect(areMemoryAttachesValid([attach({ instructions: '😀'.repeat(501) })])).toBe(false);
  });

  test('accepts empty instructions and exactly 500 code points', () => {
    expect(areMemoryAttachesValid([])).toBe(true);
    expect(areMemoryAttachesValid([attach({ instructions: '' })])).toBe(true);
    expect(areMemoryAttachesValid([attach({ instructions: 'a'.repeat(MAX_MEMORY_ATTACH_INSTRUCTIONS) })])).toBe(true);
    expect(areMemoryAttachesValid([attach({ instructions: '😀'.repeat(MAX_MEMORY_ATTACH_INSTRUCTIONS) })])).toBe(true);
  });

  test('rejects a draft memory card before a store is selected', () => {
    expect(areMemoryAttachesValid([emptyMemoryAttach()])).toBe(false);
  });

  test('omits empty instructions and never copies mount_path, name, or description', () => {
    const packed = memoryAttachResources([
      {
        memoryStoreId: 'memstore_one123456',
        access: 'read_only',
        instructions: '',
        mountPath: '/mnt/memory/secret',
      },
    ]);
    expect(packed).toEqual([
      {
        type: 'memory_store',
        memory_store_id: 'memstore_one123456',
        access: 'read_only',
      },
    ]);
    expect(memoryAttachHasForbiddenClientFields(packed[0])).toBe(false);
    expect(JSON.stringify(packed)).not.toContain('mount_path');
    expect(JSON.stringify(packed)).not.toContain('/mnt/memory/secret');
  });

  test('keeps selected instructions and access on session and deployment create bodies', () => {
    const values = formValues({
      memoryAttaches: [attach()],
      fileResources: [{ fileId: 'file_input123456', mountPath: '' }],
    });
    const sessionBody = createManagedEntityBody('sessions', values);
    expect(sessionBody.resources).toEqual([
      { type: 'file', file_id: 'file_input123456' },
      {
        type: 'memory_store',
        memory_store_id: 'memstore_one123456',
        access: 'read_write',
        instructions: '有新偏好就更新',
      },
    ]);
    expect(memoryAttachHasForbiddenClientFields(sessionBody.resources[1] as object)).toBe(false);

    const deploymentBody = createManagedEntityBody('deployments', values);
    expect(deploymentBody.resources).toEqual([
      {
        type: 'memory_store',
        memory_store_id: 'memstore_one123456',
        access: 'read_write',
        instructions: '有新偏好就更新',
      },
    ]);
  });

  test('does not write back server mount_path, name, or description when editing a deployment', () => {
    const entity = {
      id: 'dep_one123456',
      agent: 'agent_option123456',
      archived_at: null,
      created_at: '2026-08-30T00:00:00Z',
      environment_id: 'env_option123456',
      name: 'Deployment one',
      status: 'active',
      type: 'deployment',
      updated_at: '2026-08-30T00:00:00Z',
      resources: [
        {
          type: 'memory_store',
          memory_store_id: 'memstore_one123456',
          access: 'read_only',
          instructions: 'keep',
          mount_path: '/mnt/memory/user-preferences',
          name: 'snapshot name',
          description: 'snapshot description',
        },
      ],
    } as DeploymentApiResponse;
    const attaches = entityMemoryAttaches(entity);
    expect(attaches).toEqual([
      {
        memoryStoreId: 'memstore_one123456',
        access: 'read_only',
        instructions: 'keep',
        mountPath: '/mnt/memory/user-preferences',
      },
    ]);
    const body = updateManagedEntityBody(
      'deployments',
      formValues({ memoryAttaches: attaches, name: 'Deployment one' }),
    );
    expect(body.resources).toEqual([
      {
        type: 'memory_store',
        memory_store_id: 'memstore_one123456',
        access: 'read_only',
        instructions: 'keep',
      },
    ]);
    expect(JSON.stringify(body.resources)).not.toContain('mount_path');
    expect(JSON.stringify(body.resources)).not.toContain('snapshot name');
    expect(JSON.stringify(body.resources)).not.toContain('snapshot description');
  });

  test('omits memory resources when no store is selected', () => {
    expect(createManagedEntityBody('deployments', formValues()).resources).toEqual([]);
    expect(createManagedEntityBody('sessions', formValues()).resources).toEqual([]);
  });

  test('preserves existing attaches when syncing selected store ids', () => {
    const current = [attach({ access: 'read_only', instructions: 'keep' })];
    expect(syncMemoryAttaches(current, ['memstore_one123456', 'memstore_two'])).toEqual([
      current[0],
      { memoryStoreId: 'memstore_two', access: 'read_write', instructions: '' },
    ]);
  });
});
