import { afterEach, expect, test } from 'bun:test';
import '../../../test/setup';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { I18nProvider } from '../../../shared/i18n';
import { AddWorkspaceMemberDialog } from './AddWorkspaceMemberDialog';
const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
afterEach(cleanup);
test('respects controlled open, blocks pending dismissal and can reopen after cancellation', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData(['console', 'workspace-member-candidates', 'org_sample', 'wrkspc_sample'], []);
  let closed = 0;
  const view = (open: boolean, pending: boolean) => (
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <AddWorkspaceMemberDialog
          orgUuid="org_sample"
          workspaceId="wrkspc_sample"
          workspaceName="Research"
          open={open}
          pending={pending}
          onClose={() => {
            closed++;
          }}
          onAdd={() => {}}
        />
      </I18nProvider>
    </QueryClientProvider>
  );
  const rendered = render(view(false, false));
  expect(screen.queryByRole('dialog')).toBeNull();
  rendered.rerender(view(true, true));
  const dialog = await screen.findByRole('dialog');
  expect(screen.getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(closed).toBe(0);
  rendered.rerender(view(true, false));
  fireEvent.click(screen.getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(closed).toBe(1));
  rendered.rerender(view(false, false));
  rendered.rerender(view(true, false));
  expect(await screen.findByRole('dialog')).toBeTruthy();
});

test('shows a placeholder, searches candidates and returns the selected user ID and role', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData(
    ['console', 'workspace-member-candidates', 'org_sample', 'wrkspc_sample'],
    [
      { user_id: 'candidate_one', name: 'Same name', email: 'first@example.test' },
      { user_id: 'candidate_two', name: 'Same name', email: 'second@example.test' },
    ],
  );
  const selected: Array<[string, string]> = [];
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <AddWorkspaceMemberDialog
          orgUuid="org_sample"
          workspaceId="wrkspc_sample"
          workspaceName={'LongWorkspaceName'.repeat(12)}
          open
          pending={false}
          onClose={() => {}}
          onAdd={(userId, role) => selected.push([userId, role])}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  expect(screen.getByRole('combobox', { name: 'Member', exact: true }).textContent).toContain('Select');
  fireEvent.click(screen.getByRole('combobox', { name: 'Member', exact: true }));
  fireEvent.change(screen.getByRole('combobox', { name: 'Search options...' }), {
    target: { value: 'second@example.test' },
  });
  fireEvent.click(await screen.findByRole('option', { name: 'Same name (second@example.test)' }));
  fireEvent.click(screen.getByRole('combobox', { name: 'Role', exact: true }));
  fireEvent.click(await screen.findByRole('option', { name: 'Workspace Admin' }));
  fireEvent.click(screen.getByRole('button', { name: 'Add to Workspace' }));
  expect(selected).toEqual([['candidate_two', 'workspace_admin']]);
});

test('keeps unavailable candidate controls disabled and leaves cancellation available', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  client.setQueryData(['console', 'workspace-member-candidates', 'org_sample', 'wrkspc_sample'], []);
  let closed = 0;
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <AddWorkspaceMemberDialog
          orgUuid="org_sample"
          workspaceId="wrkspc_sample"
          workspaceName="Research"
          open
          pending={false}
          onClose={() => {
            closed++;
          }}
          onAdd={() => {
            throw new Error('Unavailable candidate must not be submitted');
          }}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  expect(screen.getByRole('combobox', { name: 'Member', exact: true }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('combobox', { name: 'Role', exact: true }).hasAttribute('disabled')).toBe(true);
  expect(screen.getByRole('button', { name: 'Add to Workspace' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(closed).toBe(1));
});
