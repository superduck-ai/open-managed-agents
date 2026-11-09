import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { I18nProvider } from '../../shared/i18n';
import type { LLMProvider } from './api';
import { ProviderDeleteDialog } from './ProviderDeleteDialog';
const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);
const provider: LLMProvider = {
  id: 'llmp_sample',
  type: 'llm_provider',
  scope: 'organization',
  name: 'Research',
  slug: 'research',
  api_backend: 'messages',
  base_url: 'https://example.test',
  has_api_key: true,
  api_key_last4: '',
  models: [{ id: 'research' }],
  created_at: '2026-10-25T00:00:00Z',
  updated_at: '2026-10-25T00:00:00Z',
};
function Sample() {
  const [selected, setSelected] = useState<LLMProvider | null>(provider);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  return (
    <I18nProvider initialLocale="en">
      <button
        onClick={() => {
          setPending(false);
          setError('Delete failed');
        }}
      >
        Fail request
      </button>
      <button
        onClick={() => {
          setError('');
          setSelected({ ...provider, scope: undefined });
        }}
      >
        Open workspace
      </button>
      <ProviderDeleteDialog
        provider={selected}
        error={error}
        isPending={pending}
        onClose={() => {
          setSelected(null);
          setError('');
        }}
        onConfirm={() => setPending(true)}
      />
    </I18nProvider>
  );
}
test('blocks dismiss while deleting and preserves the error until cancellation finishes', async () => {
  render(<Sample />);
  const dialog = await screen.findByRole('alertdialog');
  fireEvent.click(within(dialog).getByRole('button', { name: 'Delete', exact: true }));
  expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
  fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
  expect(screen.getByRole('alertdialog')).toBe(dialog);
  fireEvent.click(screen.getByRole('button', { name: 'Fail request', hidden: true }));
  expect(within(dialog).getByText('Delete failed')).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Open workspace' }));
  const reopened = await screen.findByRole('alertdialog');
  expect(within(reopened).getByText('Its models will stop working immediately in this workspace.')).toBeTruthy();
  expect(within(reopened).queryByText('Delete failed')).toBeNull();
});

test('keeps organization warning and error when the owner clears selection during exit', async () => {
  const view = (value: LLMProvider | null, error: string) => (
    <I18nProvider initialLocale="en">
      <ProviderDeleteDialog provider={value} error={error} isPending={false} onClose={() => {}} onConfirm={() => {}} />
    </I18nProvider>
  );
  const rendered = render(view(provider, 'Delete failed'));
  await screen.findByRole('alertdialog');
  rendered.rerender(view(null, ''));
  expect(document.querySelector('[data-slot="alert-dialog-description"]')?.textContent).toContain(
    'this provider for every workspace',
  );
  expect(document.querySelector('[data-slot="field-error"]')?.textContent).toContain('Delete failed');
});
