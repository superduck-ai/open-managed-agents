import { afterEach, describe, expect, test } from 'bun:test';
import {
  mockAgentsApi,
  renderManagedAgentsPage,
  resetManagedAgentsTestState,
  resetTestDom,
} from './ManagedAgentsPage.test-utils';

afterEach(resetManagedAgentsTestState);

describe('ManagedAgentsPage test utilities', () => {
  test('seeds create-agent models under the selected workspace', () => {
    resetTestDom('https://oma.duck.ai/workspaces/wrkspc_test/agents');
    mockAgentsApi([]);
    const models = [{ id: 'claude-test-model', displayName: 'Claude Test Model' }];

    const { queryClient } = renderManagedAgentsPage('agents', 'en', {
      workspaceId: 'wrkspc_test',
      models,
    });

    expect(queryClient.getQueryData(['create-agent', 'models', 'wrkspc_test'])).toEqual(models);
    expect(queryClient.getQueryData(['create-agent', 'models', 'default'])).toBeUndefined();
  });
});
