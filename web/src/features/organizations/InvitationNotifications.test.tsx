import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../test/setup';
import { AuthContext } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { InvitationNotifications } from './InvitationNotifications';
import { setConsoleRequestContext } from '../../shared/api/client';

const { render, cleanup, screen, fireEvent, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.forEach((client) => client.clear());
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});
function mount(fail = false) {
  resetTestDom('https://oma.duck.ai/');
  setConsoleRequestContext({ csrfToken: 'csrf-test' });
  let invitations = ['Research', 'Design'].map((name, index) => ({
    id: `${index}`,
    organization_uuid: name,
    organization_name: name,
    role: 'user',
    invited_at: '2026-11-01T00:00:00Z',
    expires_at: '2026-12-01T00:00:00Z',
  }));
  const writes: string[] = [];
  globalThis.fetch = mock(async (input, init) => {
    const path = String(input);
    if (fail) return Response.json({ error: 'Unavailable' }, { status: 500 });
    if (init?.method === 'POST') {
      const id = path.split('/').at(-2)!;
      writes.push(path);
      invitations = invitations.filter((item) => item.id !== id);
      expect(new Headers(init.headers).get('X-CSRF-Token')).toBe('csrf-test');
      return Response.json({ id, status: 'declined', organization_uuid: 'Research' });
    }
    return Response.json({ data: invitations });
  }) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  render(
    <I18nProvider initialLocale="en">
      <QueryClientProvider client={client}>
        <AuthContext.Provider
          value={{
            account: { uuid: 'account', email_address: 'a@example.com' },
            status: 'authenticated',
            refresh: async () => undefined,
            logout: async () => {},
          }}
        >
          <InvitationNotifications />
        </AuthContext.Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  return writes;
}

test('invitation load failure offers retry without claiming zero pending invitations', async () => {
  mount(true);
  fireEvent.click(screen.getByRole('button', { name: 'Organization invitations' }));
  expect(await screen.findByRole('alert')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
  expect(screen.queryByText('No pending invitations.')).toBeNull();
});

test('viewing keeps the count; processing updates the badge through the real API contract', async () => {
  const writes = mount();
  fireEvent.click(await screen.findByRole('button', { name: 'Invitations, 2 pending' }));
  fireEvent.click(screen.getByRole('button', { name: /Research/ }));
  expect(await screen.findByRole('dialog')).toBeTruthy();
  expect(writes).toHaveLength(0);
  expect(document.querySelector('.console-invitation-count')?.textContent).toBe('2');
  fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(writes).toEqual(['/api/invitations/0/decline']);
  expect(await screen.findByRole('button', { name: 'Invitations, 1 pending' })).toBeTruthy();
});
