import { describe, expect, test } from 'bun:test';
import '../../../test/setup';
import { createManagedEntityBody, updateManagedEntityBody } from '../api';
import type { DeploymentApiResponse } from '../types';
import { initialFormValues } from './model';
import { emptyGitResource, gitResourceBody, gitResourceValid } from './git-resource';

const repositoryURL = 'https://git.internal/group/subgroup/repo.git';

test.each(['', 'abcdef0', 'a'.repeat(39), 'a'.repeat(41), 'a'.repeat(63), 'a'.repeat(65), 'g'.repeat(40)])(
  'rejects incomplete or non-hexadecimal commit SHA %j',
  (sha) => {
    const resource = {
      ...emptyGitResource(),
      url: repositoryURL,
      checkoutType: 'commit' as const,
      checkoutValue: sha,
    };
    expect(gitResourceValid(resource)).toBe(false);
    expect(() => gitResourceBody(resource)).toThrow();
  },
);

test.each([40, 64])('accepts a full %i-character commit SHA for Session and Deployment resources', (length) => {
  for (const section of ['sessions', 'deployments'] as const) {
    const sha = 'Ab'.repeat(length / 2);
    const values = {
      ...initialFormValues(section),
      gitResources: [
        { ...emptyGitResource(), url: repositoryURL, checkoutType: 'commit' as const, checkoutValue: sha },
      ],
    };
    expect(createManagedEntityBody(section, values).resources).toEqual([
      { type: 'github_repository', url: repositoryURL, checkout: { type: 'commit', sha } },
    ]);
  }
});

test('creates Session and Deployment Git resources with optional credentials', () => {
  for (const section of ['sessions', 'deployments'] as const) {
    for (const authorizationToken of ['', 'secret']) {
      const values = {
        ...initialFormValues(section),
        gitResources: [{ ...emptyGitResource(), url: repositoryURL, authorizationToken }],
      };
      expect(createManagedEntityBody(section, values).resources).toEqual([
        {
          type: 'github_repository',
          url: repositoryURL,
          ...(authorizationToken ? { authorization_token: authorizationToken } : {}),
        },
      ]);
    }
  }
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
        url: repositoryURL,
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

  test.each(['', 'replacement'])('replaces resources with token %j while preserving other resources', (token) => {
    const values = initialFormValues('deployments', deployment);
    const body = updateManagedEntityBody('deployments', {
      ...values,
      resourcesChanged: true,
      gitResources: values.gitResources.map((resource) => ({ ...resource, authorizationToken: token })),
    });
    expect(body.resources).toEqual([
      { type: 'file', file_id: 'file_test', mount_path: '/reports/input.csv' },
      {
        type: 'github_repository',
        url: repositoryURL,
        mount_path: '/workspace/repo',
        ...(token ? { authorization_token: token } : {}),
        checkout: { type: 'branch', name: 'main' },
      },
      { type: 'memory_store', memory_store_id: 'memory_test', access: 'read_only', instructions: 'Keep context' },
    ]);
  });
});
