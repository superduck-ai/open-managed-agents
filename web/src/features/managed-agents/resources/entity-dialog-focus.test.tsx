import { afterEach, expect, test } from 'bun:test';
import {
  fireEvent,
  mockManagedResourceApi,
  renderManagedAgentsPage,
  resetManagedAgentsTestState,
  resetTestDom,
  screen,
  waitFor,
  within,
} from '../ManagedAgentsPage.test-utils';

afterEach(resetManagedAgentsTestState);

for (const scenario of [
  { section: 'memory-stores', label: 'Create memory store', close: 'Escape' },
  { section: 'credential-vaults', label: 'Create vault', close: 'Cancel' },
  { section: 'deployments', label: 'Create deployment', close: 'Close' },
  { section: 'memory-stores', label: 'Add memory', path: '/memstore_one123456', close: 'Cancel' },
] as const) {
  test(`${scenario.section} restores focus to its opener after ${scenario.close} without saving`, async () => {
    resetTestDom(
      `https://oma.duck.ai/workspaces/default/${scenario.section}${'path' in scenario ? scenario.path : ''}`,
    );
    const api = mockManagedResourceApi();
    renderManagedAgentsPage(scenario.section);
    const opener = await screen.findByRole('button', { name: scenario.label, exact: true });
    fireEvent.click(opener);
    const dialog = await screen.findByRole('dialog');
    if (scenario.close === 'Escape') {
      fireEvent.keyDown(within(dialog).getByLabelText('Name'), { key: 'Escape', code: 'Escape' });
    } else {
      fireEvent.click(within(dialog).getByRole('button', { name: scenario.close, exact: true }));
    }
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(opener));
    expect(api.requests.some((request) => request.method !== 'GET')).toBe(false);
  });
}
