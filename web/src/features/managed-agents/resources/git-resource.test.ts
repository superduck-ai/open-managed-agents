import { describe, expect, test } from 'bun:test';
import '../../../test/setup';
import { createManagedEntityBody, updateManagedEntityBody } from '../api';
import type { DeploymentApiResponse } from '../types';
import { initialFormValues } from './model';
import { emptyGitResource } from './git-resource';

const repositoryURL = 'https://git.internal/group/subgroup/repo.git';

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
