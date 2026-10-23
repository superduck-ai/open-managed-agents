import { afterEach, expect, test } from 'bun:test';
import {
  cleanup,
  fireEvent,
  mockManagedResourceApi,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  waitFor,
} from '../ManagedAgentsPage.test-utils';
import { budgetWireBody } from '../resources/budget';

const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

for (const remove of [false, true]) {
  test(`${remove ? 'removing' : 'raising'} the budget enables sending despite the old idle event`, async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
    const api = mockManagedResourceApi();
    Object.assign(api.resources.sessions[0], {
      status: 'idle',
      budget: budgetWireBody(1),
      usage: { list_cost: { amount: '7', currency: 'USD' } },
    });
    api.resources.sessionEvents.length = 0;
    Object.values(api.resources.sessionThreadEvents).forEach((events) => (events.length = 0));
    api.resources.sessionEvents.push({
      id: 'budget_idle',
      type: 'session.status_idle',
      stop_reason: { type: 'budget_reached' },
      created_at: '2026-10-20T01:00:00Z',
      processed_at: '2026-10-20T01:00:00Z',
    });
    const fetchMock = globalThis.fetch;
    globalThis.fetch = (async (input, init) => {
      if (String(input).includes('/v1/sessions/sesn_one123456?') && init?.method === 'POST') {
        const body = JSON.parse(String(init.body));
        if ('budget' in body) Object.assign(api.resources.sessions[0], { budget: body.budget });
      }
      return fetchMock(input, init);
    }) as typeof fetch;
    renderManagedAgentsPage('sessions');
    await screen.findByRole('button', { name: 'Remove budget' });
    const message = screen.getByRole('textbox', { name: 'Message' });
    expect(message.hasAttribute('disabled')).toBe(true);
    if (remove) {
      fireEvent.click(screen.getByRole('button', { name: 'Remove budget' }));
    } else {
      fireEvent.click(screen.getByRole('button', { name: 'Change budget' }));
      fireEvent.change(screen.getByLabelText('Budget (USD)'), { target: { value: '1' } });
      fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    }
    await waitFor(() => expect(message.hasAttribute('disabled')).toBe(false));
    fireEvent.change(message, { target: { value: 'continue' } });
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Send message' }).hasAttribute('disabled')).toBe(false),
    );
  });
}
