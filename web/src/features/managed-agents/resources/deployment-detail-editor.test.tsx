import { afterEach, expect, test } from 'bun:test';
import {
  fireEvent,
  mockManagedResourceApi,
  render,
  renderManagedAgentsPage,
  resetManagedAgentsTestState,
  resetTestDom,
  screen,
  waitFor,
  within,
} from '../ManagedAgentsPage.test-utils';
import { I18nProvider } from '../../../shared/i18n';
import { EnvironmentWorkPanel } from './environment-details';

afterEach(resetManagedAgentsTestState);

function mountDeploymentDetail() {
  resetTestDom('https://oma.duck.ai/workspaces/default/deployments/dep_one123456');
  const api = mockManagedResourceApi();
  Object.assign(api.resources.deployments[0], {
    initial_events: [{ type: 'user.message', content: [{ type: 'text', text: 'Summarize updates.' }] }],
    schedule: { type: 'cron', expression: '0 9 * * 1-5', timezone: 'Asia/Shanghai' },
  });
  renderManagedAgentsPage('deployments');
  return api;
}

test('opens the shared deployment editor from details and cancels without a mutation', async () => {
  const api = mountDeploymentDetail();
  await screen.findByRole('heading', { name: 'Deployment one' });
  fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
  const dialog = await screen.findByRole('dialog', { name: 'Edit deployment' });
  expect(within(dialog).getByRole('combobox', { name: 'Agent' })).toBeTruthy();
  expect(within(dialog).getByRole('combobox', { name: 'Environment' })).toBeTruthy();
  expect(within(dialog).getByLabelText('Initial message')).toHaveProperty('value', 'Summarize updates.');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(api.requests.some((request) => request.method !== 'GET')).toBe(false);
  expect(screen.getByRole('heading', { name: 'Deployment one' })).toBeTruthy();
  expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Edit', exact: true }));
});

test('keeps detail edits after a save failure and closes the shared editor after retry succeeds', async () => {
  const api = mountDeploymentDetail();
  const fetchMock = globalThis.fetch;
  let attempts = 0;
  globalThis.fetch = async (input, init) => {
    const url = String(input instanceof Request ? input.url : input);
    const method = init?.method ?? (input instanceof Request ? input.method : 'GET');
    if (url.includes('/v1/deployments/dep_one123456') && method === 'POST') {
      attempts++;
      if (attempts === 1) return Response.json({ error: { message: 'Sample update failed' } }, { status: 500 });
    }
    return fetchMock(input, init);
  };
  await screen.findByRole('heading', { name: 'Deployment one' });
  fireEvent.click(screen.getByRole('button', { name: 'Edit', exact: true }));
  const dialog = await screen.findByRole('dialog', { name: 'Edit deployment' });
  fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Updated digest' } });
  const save = within(dialog).getByRole('button', { name: 'Save changes' });
  await waitFor(() => expect(save.hasAttribute('disabled')).toBe(false));
  fireEvent.click(save);
  await within(dialog).findByRole('alert');
  expect(within(dialog).getByLabelText('Name')).toHaveProperty('value', 'Updated digest');
  await waitFor(() => expect(save.hasAttribute('disabled')).toBe(false));
  fireEvent.click(save);
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(attempts).toBe(2);
  expect(screen.getByRole('heading', { name: 'Updated digest' })).toBeTruthy();
  const update = api.requests.find((request) => request.method === 'POST' && request.url.includes('/v1/deployments/'));
  expect(update?.body?.initial_events).toEqual([
    { type: 'user.message', content: [{ type: 'text', text: 'Summarize updates.' }] },
  ]);
  expect(update?.body?.schedule).toEqual({ type: 'cron', expression: '0 9 * * 1-5', timezone: 'Asia/Shanghai' });
  expect(update?.body).not.toHaveProperty('resources');
});

test('uses the shared loading state for environment work history', () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/environments/env_sample');
  globalThis.fetch = async () => new Promise<Response>(() => {});
  render(
    <I18nProvider initialLocale="en">
      <EnvironmentWorkPanel
        environment={{
          id: 'env_sample',
          type: 'environment',
          name: 'Environment',
          description: '',
          archived_at: null,
          config: {},
          scope: 'workspace',
          state: 'active',
          created_at: '2026-10-25T00:00:00Z',
          updated_at: '2026-10-25T00:00:00Z',
        }}
        workspaceId="default"
        refreshKey={0}
      />
    </I18nProvider>,
  );
  expect(screen.getByRole('heading', { name: 'Work queue' })).toBeTruthy();
  expect(screen.getByText('Loading...')).toBeTruthy();
});
