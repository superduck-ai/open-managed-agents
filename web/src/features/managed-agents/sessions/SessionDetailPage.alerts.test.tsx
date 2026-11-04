import { afterEach, expect, test } from 'bun:test';
import {
  cleanup,
  mockManagedResourceApi,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  within,
} from '../ManagedAgentsPage.test-utils';

const originalFetch = globalThis.fetch;
const htmlError =
  '404 <!DOCTYPE html><html lang="en"><head><title>404 - Not Found</title></head><body>404 Not Found</body></html>';

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('shows session sync errors above the composer instead of under the page header', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  mockManagedResourceApi();
  const mockedFetch = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof Request ? input.url : String(input);
    if (new URL(url, 'https://oma.duck.ai').pathname === '/v1/sessions/sesn_one123456/threads') {
      return new Response(htmlError, { status: 404, headers: { 'Content-Type': 'text/html' } });
    }
    return mockedFetch(input, init);
  };

  renderManagedAgentsPage('sessions');

  const alerts = await screen.findByTestId('session-alerts');
  expect(within(alerts).getByText('Request failed: the server returned an HTTP 404 page.')).toBeTruthy();
  expect(screen.getByTestId('session-detail-header').contains(alerts)).toBe(false);
  expect(screen.queryByText(/<!DOCTYPE html>/)).toBeNull();
  expect(within(alerts).getByRole('button', { name: 'Show raw error' })).toBeTruthy();
});
