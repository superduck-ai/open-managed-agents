import '../../test/setup';
import { afterEach, expect, mock, test } from 'bun:test';
import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../i18n';
import { CreateWorkspaceDialog } from './CreateWorkspaceDialog';
import { EditWorkspaceDialog } from './EditWorkspaceDialog';

afterEach(cleanup);

function DialogHarness({ kind, save }: { kind: 'create' | 'edit'; save: () => Promise<void> }) {
  const [open, setOpen] = useState(true);
  return (
    <I18nProvider initialLocale="en">
      <button onClick={() => setOpen(true)}>Open fixture</button>
      {kind === 'create' ? (
        <CreateWorkspaceDialog open={open} onOpenChange={setOpen} onCreate={save} />
      ) : (
        <EditWorkspaceDialog
          open={open}
          workspace={open ? { id: 'wrkspc_fixture', name: 'Research' } : null}
          onClose={() => setOpen(false)}
          onSave={save}
        />
      )}
    </I18nProvider>
  );
}

for (const kind of ['create', 'edit'] as const) {
  test(`${kind} workspace protects the pending draft and preserves the failed form through cancellation`, async () => {
    resetTestDom('https://oma.duck.ai/settings/workspaces');
    let rejectSave: (reason: Error) => void = () => {};
    const save = mock(
      () =>
        new Promise<void>((_resolve, reject) => {
          rejectSave = reject;
        }),
    );
    render(<DialogHarness kind={kind} save={save} />);
    const dialog = screen.getByRole('dialog');
    const nameInput = within(dialog).getByRole('textbox', { name: 'Name', exact: true });
    fireEvent.change(nameInput, { target: { value: 'Research draft' } });
    fireEvent.click(within(dialog).getByRole('button', { name: kind === 'create' ? 'Create' : 'Save', exact: true }));
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Close' })).toHaveProperty('disabled', true));
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveProperty('disabled', true);
    expect(dialog.querySelector('fieldset')).toHaveProperty('disabled', true);
    fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    fireEvent.submit(dialog.querySelector('form')!);
    expect(dialog.hasAttribute('data-closed')).toBe(false);
    expect(save).toHaveBeenCalledTimes(1);
    await act(async () => rejectSave(new Error('Workspace save failed')));
    await within(dialog).findByText('Workspace save failed');
    expect(document.activeElement).toBe(within(dialog).getByRole('alert'));
    expect(nameInput).toHaveProperty('value', 'Research draft');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(dialog.hasAttribute('data-closed')).toBe(true);
    expect(nameInput).toHaveProperty('value', 'Research draft');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
}

test('create workspace resets the completed draft only after exit and starts a fresh reopened form', async () => {
  resetTestDom('https://oma.duck.ai/settings/workspaces');
  const save = mock(async () => {});
  render(<DialogHarness kind="create" save={save} />);
  const dialog = screen.getByRole('dialog');
  const nameInput = within(dialog).getByRole('textbox', { name: 'Name', exact: true });
  fireEvent.change(nameInput, { target: { value: 'New research' } });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Create', exact: true }));
  await waitFor(() => expect(dialog.hasAttribute('data-closed')).toBe(true));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'Open fixture' }));
  expect(await screen.findByRole('textbox', { name: 'Name', exact: true })).toHaveProperty('value', '');
  expect(save).toHaveBeenCalledTimes(1);
});
