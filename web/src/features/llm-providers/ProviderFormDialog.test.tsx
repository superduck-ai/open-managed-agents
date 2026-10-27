import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import type { LLMProvider } from './api';
import { ProviderFormDialog } from './ProviderFormDialog';
const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);
const provider: LLMProvider = {
  id: 'llmp_sample',
  type: 'llm_provider',
  name: 'Research provider',
  slug: 'research',
  api_backend: 'messages',
  base_url: 'https://provider.example.test',
  has_api_key: true,
  api_key_last4: '',
  models: [{ id: 'sample-model' }],
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
};
function Sample({ onSave }: { onSave: () => Promise<void> }) {
  const [open, setOpen] = useState(false),
    [closed, setClosed] = useState(0),
    [pending, setPending] = useState(false),
    [error, setError] = useState('');
  return (
    <I18nProvider initialLocale="en">
      <button onClick={() => setOpen(true)}>Edit provider</button>
      <p role="status">Closed {closed}</p>
      {open ? (
        <ProviderFormDialog
          provider={provider}
          error={error}
          isPending={pending}
          onClose={() => {
            setOpen(false);
            setClosed((count) => count + 1);
          }}
          onDiscoverModels={async () => []}
          onSubmit={async () => {
            setPending(true);
            try {
              await onSave();
              return true;
            } catch (error) {
              setError(error instanceof Error ? error.message : 'Save failed');
              return false;
            } finally {
              setPending(false);
            }
          }}
        />
      ) : null}
    </I18nProvider>
  );
}
test('keeps provider edits after failure and disables dismiss controls while saving', async () => {
  let rejectSave: (error: Error) => void = () => {};
  let saveCalls = 0;
  render(
    <Sample
      onSave={() =>
        new Promise<void>((_, reject) => {
          saveCalls++;
          rejectSave = reject;
        })
      }
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Edit provider' }));
  const dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'Updated research' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save', exact: true }));
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true);
  expect(dialog.querySelector('fieldset')).toHaveProperty('disabled', true);
  fireEvent.submit(dialog.querySelector('form')!);
  expect(saveCalls).toBe(1);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(screen.getByText('Closed 0')).toBeTruthy();
  rejectSave(new Error('Sample save failed'));
  expect(await within(dialog).findByText('Sample save failed')).toBeTruthy();
  expect(document.activeElement).toBe(within(dialog).getByRole('alert'));
  expect(within(dialog).getByLabelText('Display name')).toHaveProperty('value', 'Updated research');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(screen.getByText('Closed 1')).toBeTruthy();
});
test('cancels provider edits without saving and restores the original fields on reopen', async () => {
  let saved = 0;
  render(
    <Sample
      onSave={async () => {
        saved++;
      }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Edit provider' }));
  let dialog = await screen.findByRole('dialog');
  fireEvent.change(within(dialog).getByLabelText('Display name'), { target: { value: 'Discarded' } });
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Edit provider' }));
  dialog = await screen.findByRole('dialog');
  expect(within(dialog).getByLabelText('Display name')).toHaveProperty('value', 'Research provider');
  expect(saved).toBe(0);
});

test('keeps a successfully saved provider draft mounted until its exit completes', async () => {
  render(<Sample onSave={async () => {}} />);
  fireEvent.click(screen.getByRole('button', { name: 'Edit provider' }));
  const dialog = await screen.findByRole('dialog');
  const name = within(dialog).getByLabelText('Display name');
  fireEvent.change(name, { target: { value: 'Saved research' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(dialog.hasAttribute('data-closed')).toBe(true));
  expect(name).toHaveProperty('value', 'Saved research');
  await waitFor(() => expect(screen.getByText('Closed 1')).toBeTruthy());
});

test('editing preserves an existing display name and clearing it saves the Provider ID', async () => {
  let savedName = '';
  render(
    <I18nProvider initialLocale="en">
      <ProviderFormDialog
        provider={provider}
        error=""
        isPending={false}
        onClose={() => {}}
        onDiscoverModels={async () => []}
        onSubmit={async (input) => {
          savedName = input.name;
          return true;
        }}
      />
    </I18nProvider>,
  );
  expect(screen.getByLabelText('Display name')).toHaveProperty('value', provider.name);
  fireEvent.change(screen.getByLabelText('Display name'), { target: { value: '' } });
  expect(screen.getByLabelText('Display name')).toHaveProperty('value', provider.slug);
  fireEvent.click(screen.getByRole('button', { name: 'Save', exact: true }));
  await waitFor(() => expect(savedName).toBe(provider.slug));
});
