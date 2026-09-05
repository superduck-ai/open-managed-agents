import { describe, expect, test } from 'bun:test';
import { createManagedEntityBody, updateManagedEntityBody } from '../api';
import type { DeploymentApiResponse } from '../types';
import { initialFormValues } from './model';
import {
  emptyGitResource,
  gitResourceBody,
  gitResourceMountPathValid,
  gitResourceURLValid,
  gitResourceValid,
} from './git-resource';

const git = { ...emptyGitResource(), url: 'https://github.com/owner/repo', authorizationToken: 'secret' };

describe('Git resource request contract', () => {
  test('rejects malformed URLs, unsafe paths and incomplete fields', () => {
    for (const url of [
      'https://github.com/owner/repo.git',
      'https://github.com/owner/repo/',
      'http://github.com/owner/repo',
      'https://token@github.com/owner/repo',
      'https://github.com/owner/repo?token=secret',
    ]) {
      expect(gitResourceURLValid(url)).toBe(false);
    }
    for (const path of ['/workspace', '/etc/repo', '/workspace/../repo', '/workspace/.git', '/workspace/repo//src']) {
      expect(gitResourceMountPathValid(path)).toBe(false);
    }
    expect(gitResourceValid({ ...git, authorizationToken: '' })).toBe(true);
    expect(gitResourceBody({ ...git, authorizationToken: '' })).not.toHaveProperty('authorization_token');
    expect(gitResourceValid({ ...git, checkoutType: 'branch' })).toBe(false);
    expect(gitResourceValid({ ...git, checkoutType: 'commit', checkoutValue: 'xyz1234' })).toBe(false);
  });

  test('creates the same Git config for Session and Deployment with optional fields omitted', () => {
    for (const section of ['sessions', 'deployments'] as const) {
      const values = { ...initialFormValues(section), gitResources: [git] };
      expect(createManagedEntityBody(section, values)).toMatchObject({
        resources: [
          {
            type: 'github_repository',
            url: git.url,
            authorization_token: 'secret',
          },
        ],
      });
    }
    expect(
      gitResourceBody({ ...git, checkoutType: 'branch', checkoutValue: ' main ', mountPath: ' /workspace/repo ' }),
    ).toMatchObject({
      checkout: { type: 'branch', name: 'main' },
      mount_path: '/workspace/repo',
    });
    const sha = 'a'.repeat(40);
    expect(gitResourceBody({ ...git, checkoutType: 'commit', checkoutValue: sha })).toMatchObject({
      checkout: { type: 'commit', sha },
    });
  });
});

describe('Deployment resource replacement', () => {
  const deployment: DeploymentApiResponse = {
    id: 'depl_test',
    agent: 'agent_test',
    archived_at: null,
    created_at: '',
    environment_id: 'env_test',
    name: 'existing',
    status: 'active',
    type: 'deployment',
    updated_at: '',
    resources: [
      {
        type: 'github_repository',
        url: git.url,
        mount_path: '/workspace/repo',
        checkout: { type: 'branch', name: 'main' },
      },
      { type: 'file', file_id: 'file_test', mount_path: '/uploads/reports/input.csv' },
      { type: 'memory_store', memory_store_id: 'memory_test', access: 'read_only', instructions: 'Keep context' },
    ],
  };

  test('omits resources when only other settings change, without reading or inventing a token', () => {
    const values = initialFormValues('deployments', deployment);
    expect(values.gitResources[0]?.authorizationToken).toBe('');
    expect(values.gitResources[0]?.checkoutValue).toBe('main');
    const body = updateManagedEntityBody('deployments', { ...values, name: 'renamed' });
    expect(body).not.toHaveProperty('resources');
  });

  test('uses anonymous access when explicitly replacing resources without tokens', () => {
    const values = { ...initialFormValues('deployments', deployment), resourcesChanged: true };
    expect(updateManagedEntityBody('deployments', values)).toMatchObject({
      resources: expect.arrayContaining([expect.objectContaining({ type: 'github_repository', url: git.url })]),
    });
  });

  test('replaces the full list while preserving file paths and memory settings', () => {
    const values = initialFormValues('deployments', deployment);
    const body = updateManagedEntityBody('deployments', {
      ...values,
      resourcesChanged: true,
      gitResources: values.gitResources.map((resource) => ({ ...resource, authorizationToken: 'replacement' })),
    });
    expect(body).toMatchObject({
      resources: [
        { type: 'file', file_id: 'file_test', mount_path: '/reports/input.csv' },
        {
          type: 'github_repository',
          url: git.url,
          mount_path: '/workspace/repo',
          authorization_token: 'replacement',
          checkout: { type: 'branch', name: 'main' },
        },
        { type: 'memory_store', memory_store_id: 'memory_test', access: 'read_only', instructions: 'Keep context' },
      ],
    });
  });
});
