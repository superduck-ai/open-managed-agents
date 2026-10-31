import { afterEach, expect, test } from 'bun:test';
import { render, screen, resetTestDom, resetManagedAgentsTestState } from '../../ManagedAgentsPage.test-utils';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import { newBuilderState } from './model';
import { BuilderConfigPreview } from './BuilderConfigPreview';

afterEach(resetManagedAgentsTestState);

test.each([undefined, { type: 'packages', pip: [], npm: [] }])(
  'environment confirmation explains empty restrictions and packages %j',
  (packages) => {
    resetTestDom();
    const state = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'));
    render(
      <BuilderConfigPreview
        state={state}
        tool={{
          id: 'env',
          name: 'create_environment',
          input: {
            name: 'Restricted',
            config: { type: 'cloud', networking: { type: 'limited', allowed_hosts: [] }, packages },
          },
        }}
      />,
    );
    expect(screen.getByText('Restricted network · Empty allowlist')).toBeTruthy();
    expect(screen.getByText('No additional packages')).toBeTruthy();
  },
);

test('environment confirmation preserves configured hosts and packages', () => {
  resetTestDom();
  const state = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'));
  render(
    <BuilderConfigPreview
      state={state}
      tool={{
        id: 'env',
        name: 'create_environment',
        input: {
          name: 'Restricted',
          config: {
            type: 'cloud',
            networking: { type: 'limited', allowed_hosts: ['example.com'] },
            packages: { type: 'packages', pip: ['polars'] },
          },
        },
      }}
    />,
  );
  expect(screen.getByText('Restricted network · example.com')).toBeTruthy();
  expect(screen.getByText('pip: polars')).toBeTruthy();
  expect(screen.queryByText('No additional packages')).toBeNull();
});
