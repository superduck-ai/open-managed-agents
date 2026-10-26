import { I18nProvider } from '../../shared/i18n';
import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';
import { useState } from 'react';
import { useScopeUnsavedChanges } from '../../shared/organizations/unsaved';
import { useScopeConfirmation } from './useScopeConfirmation';
const { cleanup, fireEvent, render, screen, waitFor, within } = await import('@testing-library/react');
afterEach(cleanup);
function Sample({ dirty = true }: { dirty?: boolean }) {
  useScopeUnsavedChanges(dirty);
  const confirmation = useScopeConfirmation();
  const [changed, setChanged] = useState('original');
  return (
    <>
      <button onClick={() => confirmation.request(() => setChanged('organization'))}>Switch organization</button>
      <button onClick={() => confirmation.request(() => setChanged('workspace'))}>Switch workspace</button>
      <p role="status">{changed}</p>
      {confirmation.dialog}
    </>
  );
}
test('cancellation preserves the current scope and the next confirmation uses the new request', async () => {
  render(
    <I18nProvider initialLocale="zh-CN">
      <Sample />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Switch organization' }));
  const dialog = await screen.findByRole('alertdialog');
  fireEvent.click(within(dialog).getByRole('button', { name: '继续编辑' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(screen.getByRole('status').textContent).toBe('original');
  fireEvent.click(screen.getByRole('button', { name: 'Switch workspace' }));
  fireEvent.click(within(await screen.findByRole('alertdialog')).getByRole('button', { name: '放弃更改并切换' }));
  expect(screen.getByRole('status').textContent).toBe('workspace');
});
test('Escape preserves changes and a clean page switches immediately', async () => {
  const view = render(
    <I18nProvider initialLocale="zh-CN">
      <Sample />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Switch organization' }));
  fireEvent.keyDown(await screen.findByRole('alertdialog'), { key: 'Escape', code: 'Escape' });
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(screen.getByRole('status').textContent).toBe('original');
  view.rerender(
    <I18nProvider initialLocale="zh-CN">
      <Sample dirty={false} />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Switch organization' }));
  expect(screen.getByRole('status').textContent).toBe('organization');
  expect(screen.queryByRole('alertdialog')).toBeNull();
});

test('shows English scope confirmation and keeps the current scope when cancelled', async () => {
  render(
    <I18nProvider initialLocale="en">
      <Sample />
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Switch organization' }));
  const dialog = await screen.findByRole('alertdialog', { name: 'Discard unsaved changes?' });
  expect(
    within(dialog).getByText('Switching organizations or workspaces closes this page and discards unsaved changes.'),
  ).toBeTruthy();
  fireEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }));
  await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
  expect(screen.getByRole('status').textContent).toBe('original');
});
