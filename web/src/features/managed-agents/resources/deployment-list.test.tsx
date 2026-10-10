import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { I18nProvider } from '../../../shared/i18n';
import { type DeploymentApiResponse } from '../types';

const { cleanup, render, screen } = await import('@testing-library/react');
const { DeploymentTriggerCell } = await import('./deployment-list');

afterEach(cleanup);

function deployment(schedule: unknown): DeploymentApiResponse {
  return {
    id: 'dep_trigger123456',
    agent: { type: 'agent', id: 'agent_option123456' },
    archived_at: null,
    created_at: '2026-09-06T00:00:00.000Z',
    environment_id: 'env_option123456',
    name: 'Nightly',
    schedule,
    status: 'active',
    type: 'deployment',
    updated_at: '2026-09-06T00:00:00.000Z',
  };
}

test('blank schedule expressions stay on the manual trigger path', () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/deployments');
  const { rerender } = render(
    <I18nProvider>
      <DeploymentTriggerCell deployment={deployment({})} />
    </I18nProvider>,
  );
  expect(screen.getByText('Manual')).toBeTruthy();

  rerender(
    <I18nProvider>
      <DeploymentTriggerCell deployment={deployment({ type: 'cron', expression: '  ' })} />
    </I18nProvider>,
  );
  expect(screen.getByText('Manual')).toBeTruthy();

  rerender(
    <I18nProvider>
      <DeploymentTriggerCell deployment={deployment({ type: 'cron', expression: '0 9 * * *', timezone: 'UTC' })} />
    </I18nProvider>,
  );
  expect(screen.getByText(/Daily at/)).toBeTruthy();
  expect(screen.queryByText('Manual')).toBeNull();
});
